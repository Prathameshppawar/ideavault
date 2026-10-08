-- 0002: the persistent cognitive graph — ideas, branches, conversations, messages,
-- checkpoints, knowledge (class-table inheritance), relationships and history.

-- Where content came from. Every conversation/message/knowledge item can be traced to a source.
CREATE TABLE sources (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('ideavault_chat', 'import', 'url', 'file', 'paste', 'manual', 'external_ai', 'connector')),
  provider     text NOT NULL DEFAULT '',          -- chatgpt | claude | gemini | markdown | json | text | web | github ...
  adapter      text NOT NULL DEFAULT '',          -- e.g. chatgpt/v1
  title        text NOT NULL DEFAULT '',
  uri          text NOT NULL DEFAULT '',
  external_id  text NOT NULL DEFAULT '',
  content_hash text NOT NULL DEFAULT '',
  trusted      boolean NOT NULL DEFAULT false,    -- false for anything not typed by the user inside IdeaVault
  metadata     jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sources_user_idx ON sources (user_id, created_at DESC);
CREATE INDEX sources_hash_idx ON sources (user_id, content_hash) WHERE content_hash <> '';

CREATE TABLE ideas (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title                  text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  slug                   text NOT NULL,
  -- SOURCE: what the user actually said when the idea was born (verbatim).
  origin_text            text NOT NULL DEFAULT '',
  -- INTERPRETATION: IdeaVault's current understanding of the idea.
  summary                text NOT NULL DEFAULT '',
  status                 text NOT NULL DEFAULT 'EXPLORING'
                         CHECK (status IN ('EXPLORING','ACTIVE','DECIDED','READY_TO_IMPLEMENT','CONCLUDED','PARKED','ABANDONED','MERGED')),
  outcome                text CHECK (outcome IN ('IMPLEMENT','ACTION_PLAN','RESEARCH_COMPLETE','DECISION','PROPOSAL','REFERENCE','PARK','ABANDON','MERGE','OTHER')),
  outcome_note           text NOT NULL DEFAULT '',
  tags                   text[] NOT NULL DEFAULT '{}',
  default_branch_id      uuid,
  origin_source_id       uuid REFERENCES sources(id) ON DELETE SET NULL,
  origin_conversation_id uuid,
  merged_into_idea_id    uuid REFERENCES ideas(id) ON DELETE SET NULL,
  version                integer NOT NULL DEFAULT 1,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  last_activity_at       timestamptz NOT NULL DEFAULT now(),
  concluded_at           timestamptz,
  search_tsv             tsvector GENERATED ALWAYS AS (
                           setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
                           setweight(to_tsvector('english', coalesce(summary, '')), 'B') ||
                           setweight(to_tsvector('english', coalesce(origin_text, '')), 'C')
                         ) STORED,
  CONSTRAINT ideas_slug_unique UNIQUE (user_id, slug),
  CONSTRAINT ideas_merge_target CHECK (status <> 'MERGED' OR merged_into_idea_id IS NOT NULL),
  CONSTRAINT ideas_not_self_merge CHECK (merged_into_idea_id IS NULL OR merged_into_idea_id <> id)
);
CREATE INDEX ideas_user_status_idx ON ideas (user_id, status, last_activity_at DESC);
CREATE INDEX ideas_search_idx ON ideas USING gin (search_tsv);
CREATE INDEX ideas_title_trgm_idx ON ideas USING gin (title gin_trgm_ops);
CREATE TRIGGER ideas_touch BEFORE UPDATE ON ideas FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

CREATE TABLE branches (
  id                        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                   uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id                   uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  name                      text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 200),
  slug                      text NOT NULL,
  description               text NOT NULL DEFAULT '',
  status                    text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','CONCLUDED','ARCHIVED','MERGED')),
  is_default                boolean NOT NULL DEFAULT false,
  parent_branch_id          uuid REFERENCES branches(id) ON DELETE SET NULL,
  forked_from_checkpoint_id uuid,
  fork_mode                 text CHECK (fork_mode IN ('checkpoint','selective','empty')),
  head_checkpoint_id        uuid,
  created_at                timestamptz NOT NULL DEFAULT now(),
  updated_at                timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT branches_slug_unique UNIQUE (idea_id, slug)
);
CREATE UNIQUE INDEX branches_one_default_idx ON branches (idea_id) WHERE is_default;
CREATE INDEX branches_idea_idx ON branches (idea_id, created_at);
CREATE TRIGGER branches_touch BEFORE UPDATE ON branches FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

ALTER TABLE ideas ADD CONSTRAINT ideas_default_branch_fk
  FOREIGN KEY (default_branch_id) REFERENCES branches(id) ON DELETE SET NULL DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE conversations (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id       uuid REFERENCES ideas(id) ON DELETE SET NULL,
  branch_id     uuid REFERENCES branches(id) ON DELETE SET NULL,
  source_id     uuid REFERENCES sources(id) ON DELETE SET NULL,
  title         text NOT NULL DEFAULT '',
  origin        text NOT NULL DEFAULT 'native' CHECK (origin IN ('native','imported','external')),
  provider      text NOT NULL DEFAULT 'ideavault',
  external_id   text NOT NULL DEFAULT '',
  summary       text NOT NULL DEFAULT '',
  message_count integer NOT NULL DEFAULT 0,
  started_at    timestamptz NOT NULL DEFAULT now(),
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  search_tsv    tsvector GENERATED ALWAYS AS (
                  setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
                  setweight(to_tsvector('english', coalesce(summary, '')), 'B')
                ) STORED
);
CREATE INDEX conversations_user_idx ON conversations (user_id, updated_at DESC);
CREATE INDEX conversations_idea_idx ON conversations (idea_id, branch_id);
CREATE UNIQUE INDEX conversations_external_uniq ON conversations (user_id, provider, external_id) WHERE external_id <> '';
CREATE INDEX conversations_search_idx ON conversations USING gin (search_tsv);
CREATE TRIGGER conversations_touch BEFORE UPDATE ON conversations FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

ALTER TABLE ideas ADD CONSTRAINT ideas_origin_conversation_fk
  FOREIGN KEY (origin_conversation_id) REFERENCES conversations(id) ON DELETE SET NULL;

-- Agent runs are declared here because messages and knowledge reference them.
CREATE TABLE agent_runs (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id      uuid REFERENCES conversations(id) ON DELETE CASCADE,
  idea_id              uuid REFERENCES ideas(id) ON DELETE SET NULL,
  branch_id            uuid REFERENCES branches(id) ON DELETE SET NULL,
  user_message_id      uuid,
  assistant_message_id uuid,
  status               text NOT NULL DEFAULT 'RUNNING'
                       CHECK (status IN ('RUNNING','COMPLETED','FAILED','CANCELLED','AWAITING_CONFIRMATION')),
  task                 text NOT NULL DEFAULT 'chat',
  provider             text NOT NULL DEFAULT '',
  model                text NOT NULL DEFAULT '',
  -- Safe, user-visible execution events only. Never chain-of-thought.
  trace                jsonb NOT NULL DEFAULT '[]'::jsonb,
  -- Resumable loop state (model-facing messages) for confirmation pauses.
  state                jsonb NOT NULL DEFAULT '{}'::jsonb,
  input_tokens         integer NOT NULL DEFAULT 0,
  output_tokens        integer NOT NULL DEFAULT 0,
  error                text NOT NULL DEFAULT '',
  started_at           timestamptz NOT NULL DEFAULT now(),
  completed_at         timestamptz
);
CREATE INDEX agent_runs_user_idx ON agent_runs (user_id, started_at DESC);
CREATE INDEX agent_runs_conversation_idx ON agent_runs (conversation_id, started_at);

CREATE TABLE messages (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  position        integer NOT NULL,
  role            text NOT NULL CHECK (role IN ('user','assistant','system','tool')),
  content         text NOT NULL,
  -- Imported/external content is DATA, never instructions.
  untrusted       boolean NOT NULL DEFAULT false,
  external_id     text NOT NULL DEFAULT '',
  model           text NOT NULL DEFAULT '',
  agent_run_id    uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  metadata        jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at      timestamptz NOT NULL DEFAULT now(),
  search_tsv      tsvector GENERATED ALWAYS AS (to_tsvector('english', left(content, 200000))) STORED,
  CONSTRAINT messages_position_unique UNIQUE (conversation_id, position)
);
CREATE INDEX messages_conversation_idx ON messages (conversation_id, position);
CREATE INDEX messages_user_time_idx ON messages (user_id, created_at);
CREATE INDEX messages_search_idx ON messages USING gin (search_tsv);

ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_user_message_fk
  FOREIGN KEY (user_message_id) REFERENCES messages(id) ON DELETE SET NULL;
ALTER TABLE agent_runs ADD CONSTRAINT agent_runs_assistant_message_fk
  FOREIGN KEY (assistant_message_id) REFERENCES messages(id) ON DELETE SET NULL;

-- Immutable snapshots of a branch's knowledge state.
CREATE TABLE checkpoints (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id              uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id              uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id            uuid NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  number               integer NOT NULL CHECK (number > 0),   -- CP<number>, unique per idea
  title                text NOT NULL DEFAULT '',
  summary              text NOT NULL DEFAULT '',
  kind                 text NOT NULL DEFAULT 'manual' CHECK (kind IN ('manual','auto','conclusion','fork_base','merge')),
  parent_checkpoint_id uuid REFERENCES checkpoints(id) ON DELETE CASCADE,
  snapshot             jsonb NOT NULL,
  content_hash         text NOT NULL,
  created_by           text NOT NULL DEFAULT 'user' CHECK (created_by IN ('user','agent','system')),
  agent_run_id         uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT checkpoints_number_unique UNIQUE (idea_id, number)
);
CREATE INDEX checkpoints_branch_idx ON checkpoints (branch_id, created_at);
CREATE TRIGGER checkpoints_immutable BEFORE UPDATE OR DELETE ON checkpoints
  FOR EACH ROW EXECUTE FUNCTION iv_forbid_history_mutation();

ALTER TABLE branches ADD CONSTRAINT branches_forked_from_fk
  FOREIGN KEY (forked_from_checkpoint_id) REFERENCES checkpoints(id) ON DELETE SET NULL;
ALTER TABLE branches ADD CONSTRAINT branches_head_fk
  FOREIGN KEY (head_checkpoint_id) REFERENCES checkpoints(id) ON DELETE SET NULL;

-- Shared knowledge table (class-table inheritance). Type-specific columns live in
-- decisions / assumptions / evidence / insights / open_questions / action_items.
CREATE TABLE knowledge_items (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id                uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id                uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id              uuid NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  kind                   text NOT NULL CHECK (kind IN ('decision','assumption','evidence','insight','question','action')),
  -- Human reference (D3, A1, ...). Copies made by forks keep the ref of their lineage.
  ref_number             integer NOT NULL CHECK (ref_number > 0),
  lineage_id             uuid NOT NULL,
  statement              text NOT NULL CHECK (char_length(statement) BETWEEN 1 AND 4000),
  details                text NOT NULL DEFAULT '',
  status                 text NOT NULL,
  -- SOURCE = stated in the source material; INTERPRETATION = inferred by IdeaVault.
  origin                 text NOT NULL DEFAULT 'SOURCE' CHECK (origin IN ('SOURCE','INTERPRETATION')),
  -- PROPOSED items are suggestions awaiting review and are excluded from durable state.
  review_state           text NOT NULL DEFAULT 'ACCEPTED' CHECK (review_state IN ('PROPOSED','ACCEPTED','REJECTED')),
  confidence             real CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
  source_excerpt         text NOT NULL DEFAULT '',
  source_message_id      uuid REFERENCES messages(id) ON DELETE SET NULL,
  source_conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
  source_id              uuid REFERENCES sources(id) ON DELETE SET NULL,
  created_by             text NOT NULL DEFAULT 'user' CHECK (created_by IN ('user','agent','import','system')),
  agent_run_id           uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  inherited_from_id      uuid REFERENCES knowledge_items(id) ON DELETE SET NULL,
  superseded_by_id       uuid REFERENCES knowledge_items(id) ON DELETE SET NULL,
  created_at             timestamptz NOT NULL DEFAULT now(),
  updated_at             timestamptz NOT NULL DEFAULT now(),
  search_tsv             tsvector GENERATED ALWAYS AS (
                           setweight(to_tsvector('english', coalesce(statement, '')), 'A') ||
                           setweight(to_tsvector('english', coalesce(details, '')), 'B') ||
                           setweight(to_tsvector('english', coalesce(source_excerpt, '')), 'C')
                         ) STORED,
  CONSTRAINT knowledge_status_valid CHECK (
    (kind = 'decision'   AND status IN ('PROPOSED','ACTIVE','SUPERSEDED','REVERSED','REJECTED')) OR
    (kind = 'assumption' AND status IN ('UNVALIDATED','VALIDATING','VALIDATED','INVALIDATED','SUPERSEDED')) OR
    (kind = 'evidence'   AND status IN ('ACTIVE','DISPUTED','RETRACTED','SUPERSEDED')) OR
    (kind = 'insight'    AND status IN ('ACTIVE','SUPERSEDED','RETRACTED')) OR
    (kind = 'question'   AND status IN ('OPEN','ANSWERED','DROPPED','SUPERSEDED')) OR
    (kind = 'action'     AND status IN ('TODO','IN_PROGRESS','DONE','DROPPED','SUPERSEDED'))
  ),
  CONSTRAINT knowledge_not_self_superseded CHECK (superseded_by_id IS NULL OR superseded_by_id <> id),
  CONSTRAINT knowledge_ref_unique UNIQUE (branch_id, kind, ref_number)
);
CREATE INDEX knowledge_branch_idx ON knowledge_items (branch_id, kind, review_state, status);
CREATE INDEX knowledge_idea_idx ON knowledge_items (idea_id, kind, created_at);
CREATE INDEX knowledge_lineage_idx ON knowledge_items (lineage_id);
CREATE INDEX knowledge_user_time_idx ON knowledge_items (user_id, created_at DESC);
CREATE INDEX knowledge_source_msg_idx ON knowledge_items (source_message_id) WHERE source_message_id IS NOT NULL;
CREATE INDEX knowledge_search_idx ON knowledge_items USING gin (search_tsv);
CREATE TRIGGER knowledge_touch BEFORE UPDATE ON knowledge_items FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

-- Knowledge content is append-only: statement/kind/ownership never change after creation.
-- Changing a belief means creating a new item that supersedes the old one.
CREATE OR REPLACE FUNCTION iv_guard_knowledge_content() RETURNS trigger AS $$
BEGIN
  IF coalesce(current_setting('ideavault.allow_history_delete', true), '') = 'on' THEN
    RETURN NEW;
  END IF;
  IF NEW.statement IS DISTINCT FROM OLD.statement
     OR NEW.details IS DISTINCT FROM OLD.details
     OR NEW.kind IS DISTINCT FROM OLD.kind
     OR NEW.idea_id IS DISTINCT FROM OLD.idea_id
     OR NEW.branch_id IS DISTINCT FROM OLD.branch_id
     OR NEW.origin IS DISTINCT FROM OLD.origin
     OR NEW.source_excerpt IS DISTINCT FROM OLD.source_excerpt
     OR NEW.lineage_id IS DISTINCT FROM OLD.lineage_id
     OR NEW.ref_number IS DISTINCT FROM OLD.ref_number THEN
    RAISE EXCEPTION 'knowledge content is immutable; create a superseding item instead'
      USING ERRCODE = 'check_violation';
  END IF;
  RETURN NEW;
END
$$ LANGUAGE plpgsql;
CREATE TRIGGER knowledge_content_guard BEFORE UPDATE ON knowledge_items
  FOR EACH ROW EXECUTE FUNCTION iv_guard_knowledge_content();

CREATE TABLE decisions (
  item_id      uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  rationale    text NOT NULL DEFAULT '',
  -- [{"option": "...", "reason_rejected": "..."}]
  alternatives jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(alternatives) = 'array'),
  decided_at   timestamptz NOT NULL DEFAULT now(),
  reverses_id  uuid REFERENCES knowledge_items(id) ON DELETE SET NULL
);

CREATE TABLE assumptions (
  item_id           uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  risk              text NOT NULL DEFAULT 'MEDIUM' CHECK (risk IN ('LOW','MEDIUM','HIGH')),
  validation_method text NOT NULL DEFAULT '',
  validated_at      timestamptz
);

CREATE TABLE evidence (
  item_id        uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  stance         text NOT NULL DEFAULT 'SUPPORTS' CHECK (stance IN ('SUPPORTS','CHALLENGES','NEUTRAL')),
  url            text NOT NULL DEFAULT '',
  strength       text NOT NULL DEFAULT 'MODERATE' CHECK (strength IN ('WEAK','MODERATE','STRONG')),
  target_item_id uuid REFERENCES knowledge_items(id) ON DELETE SET NULL
);

CREATE TABLE insights (
  item_id    uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  importance text NOT NULL DEFAULT 'MEDIUM' CHECK (importance IN ('LOW','MEDIUM','HIGH'))
);

CREATE TABLE open_questions (
  item_id             uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  answer              text NOT NULL DEFAULT '',
  answered_at         timestamptz,
  answered_by_item_id uuid REFERENCES knowledge_items(id) ON DELETE SET NULL
);

CREATE TABLE action_items (
  item_id      uuid PRIMARY KEY REFERENCES knowledge_items(id) ON DELETE CASCADE,
  priority     text NOT NULL DEFAULT 'MEDIUM' CHECK (priority IN ('LOW','MEDIUM','HIGH')),
  due_at       timestamptz,
  completed_at timestamptz
);

-- Which items (and their exact state) each checkpoint captured. Mirrors the snapshot JSON for querying.
CREATE TABLE checkpoint_items (
  checkpoint_id uuid NOT NULL REFERENCES checkpoints(id) ON DELETE CASCADE,
  item_id       uuid NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
  kind          text NOT NULL,
  lineage_id    uuid NOT NULL,
  ref_number    integer NOT NULL,
  status        text NOT NULL,
  PRIMARY KEY (checkpoint_id, item_id)
);
CREATE INDEX checkpoint_items_item_idx ON checkpoint_items (item_id);
CREATE TRIGGER checkpoint_items_immutable BEFORE UPDATE OR DELETE ON checkpoint_items
  FOR EACH ROW EXECUTE FUNCTION iv_forbid_history_mutation();

-- Typed, provenance-carrying edges between any two graph entities.
CREATE TABLE relationships (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id           uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  from_type         text NOT NULL,
  from_id           uuid NOT NULL,
  to_type           text NOT NULL,
  to_id             uuid NOT NULL,
  rel_type          text NOT NULL CHECK (rel_type IN (
                      'similar_to','evolved_from','inspired_by','contradicts','depends_on','related_to',
                      'solves','reuses_lesson_from','supersedes','derived_from','inherited_from',
                      'supports','challenges','answers','generated_from','merged_into')),
  idea_id           uuid REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id         uuid REFERENCES branches(id) ON DELETE CASCADE,
  origin            text NOT NULL DEFAULT 'INTERPRETATION' CHECK (origin IN ('SOURCE','INTERPRETATION')),
  confidence        real CHECK (confidence IS NULL OR (confidence >= 0 AND confidence <= 1)),
  rationale         text NOT NULL DEFAULT '',
  source_message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
  created_by        text NOT NULL DEFAULT 'user' CHECK (created_by IN ('user','agent','import','system')),
  agent_run_id      uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT relationships_entity_types CHECK (
    from_type IN ('idea','branch','checkpoint','conversation','message','source','decision','assumption','evidence','insight','question','action','artifact','context_pack') AND
    to_type   IN ('idea','branch','checkpoint','conversation','message','source','decision','assumption','evidence','insight','question','action','artifact','context_pack')),
  CONSTRAINT relationships_no_self CHECK (NOT (from_type = to_type AND from_id = to_id)),
  CONSTRAINT relationships_unique UNIQUE (from_type, from_id, to_type, to_id, rel_type)
);
CREATE INDEX relationships_from_idx ON relationships (from_type, from_id);
CREATE INDEX relationships_to_idx ON relationships (to_type, to_id);
CREATE INDEX relationships_idea_idx ON relationships (idea_id, rel_type);
CREATE INDEX relationships_user_idx ON relationships (user_id, rel_type);

-- Exactly what a fork / merge inherited, and from where.
CREATE TABLE branch_inheritance (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  branch_id            uuid NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  entity_type          text NOT NULL,
  source_entity_id     uuid NOT NULL,
  new_entity_id        uuid,               -- NULL when linked rather than copied
  via                  text NOT NULL CHECK (via IN ('checkpoint','selection','merge')),
  source_checkpoint_id uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  source_branch_id     uuid REFERENCES branches(id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX branch_inheritance_branch_idx ON branch_inheritance (branch_id);
CREATE INDEX branch_inheritance_source_idx ON branch_inheritance (source_entity_id);

-- Non-copied associations of shared entities (conversations, sources, artifacts) with branches.
CREATE TABLE branch_links (
  branch_id            uuid NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  entity_type          text NOT NULL CHECK (entity_type IN ('conversation','source','artifact')),
  entity_id            uuid NOT NULL,
  link_type            text NOT NULL DEFAULT 'inherited' CHECK (link_type IN ('inherited','referenced')),
  source_checkpoint_id uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (branch_id, entity_type, entity_id)
);

-- Append-only history of an idea's identity fields.
CREATE TABLE idea_versions (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  idea_id        uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  version        integer NOT NULL,
  title          text NOT NULL,
  summary        text NOT NULL,
  status         text NOT NULL,
  outcome        text,
  tags           text[] NOT NULL DEFAULT '{}',
  changed_fields text[] NOT NULL DEFAULT '{}',
  change_reason  text NOT NULL DEFAULT '',
  created_by     text NOT NULL DEFAULT 'user',
  agent_run_id   uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT idea_versions_unique UNIQUE (idea_id, version)
);
CREATE TRIGGER idea_versions_immutable BEFORE UPDATE OR DELETE ON idea_versions
  FOR EACH ROW EXECUTE FUNCTION iv_forbid_history_mutation();

-- Activity log powering timeline, momentum and journey views.
CREATE TABLE activity_events (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id     uuid REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id   uuid REFERENCES branches(id) ON DELETE CASCADE,
  event_type  text NOT NULL,
  entity_type text NOT NULL DEFAULT '',
  entity_id   uuid,
  summary     text NOT NULL DEFAULT '',
  actor       text NOT NULL DEFAULT 'user' CHECK (actor IN ('user','agent','import','system')),
  metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX activity_user_time_idx ON activity_events (user_id, created_at DESC);
CREATE INDEX activity_idea_time_idx ON activity_events (idea_id, created_at);

CREATE TABLE idea_conclusions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id         uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id       uuid REFERENCES branches(id) ON DELETE SET NULL,
  checkpoint_id   uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  outcome         text NOT NULL CHECK (outcome IN ('IMPLEMENT','ACTION_PLAN','RESEARCH_COMPLETE','DECISION','PROPOSAL','REFERENCE','PARK','ABANDON','MERGE','OTHER')),
  note            text NOT NULL DEFAULT '',
  learned         text NOT NULL DEFAULT '',
  decisions       text NOT NULL DEFAULT '',
  unresolved      text NOT NULL DEFAULT '',
  previous_status text NOT NULL,
  new_status      text NOT NULL,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idea_conclusions_idea_idx ON idea_conclusions (idea_id, created_at);
