-- 0003: outcomes of thinking — artifacts (versioned Markdown), context packs,
-- external-AI deltas and analyses.

CREATE TABLE artifacts (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id          uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id        uuid REFERENCES branches(id) ON DELETE SET NULL,
  checkpoint_id    uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  type             text NOT NULL CHECK (type IN (
                     'ACTION_PLAN','IMPLEMENTATION_PROMPT','PRODUCT_BRIEF','RESEARCH_REPORT','STRATEGY',
                     'TECHNICAL_SPEC','ARCHITECTURE','DECISION_MEMO','PROPOSAL','CHECKLIST','MEETING_BRIEF',
                     'EXECUTIVE_SUMMARY','EXPERIMENT_PLAN','REQUIREMENTS','REFERENCE','CUSTOM')),
  title            text NOT NULL CHECK (char_length(title) BETWEEN 1 AND 300),
  content_markdown text NOT NULL,
  status           text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','FINAL','ARCHIVED')),
  current_version  integer NOT NULL DEFAULT 1 CHECK (current_version > 0),
  generator        text NOT NULL DEFAULT 'user',   -- user | llm:<provider>/<model> | template
  instructions     text NOT NULL DEFAULT '',
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  search_tsv       tsvector GENERATED ALWAYS AS (
                     setweight(to_tsvector('english', coalesce(title, '')), 'A') ||
                     setweight(to_tsvector('english', left(coalesce(content_markdown, ''), 200000)), 'B')
                   ) STORED
);
CREATE INDEX artifacts_idea_idx ON artifacts (idea_id, updated_at DESC);
CREATE INDEX artifacts_user_idx ON artifacts (user_id, updated_at DESC);
CREATE INDEX artifacts_search_idx ON artifacts USING gin (search_tsv);
CREATE TRIGGER artifacts_touch BEFORE UPDATE ON artifacts FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

CREATE TABLE artifact_versions (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  artifact_id          uuid NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  version              integer NOT NULL CHECK (version > 0),
  title                text NOT NULL,
  content_markdown     text NOT NULL,
  change_note          text NOT NULL DEFAULT '',
  created_by           text NOT NULL DEFAULT 'user' CHECK (created_by IN ('user','agent','system')),
  generator            text NOT NULL DEFAULT 'user',
  source_checkpoint_id uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  agent_run_id         uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT artifact_versions_unique UNIQUE (artifact_id, version)
);
CREATE TRIGGER artifact_versions_immutable BEFORE UPDATE OR DELETE ON artifact_versions
  FOR EACH ROW EXECUTE FUNCTION iv_forbid_history_mutation();

-- Which thinking produced which artifact version (answers "why does this say X?").
CREATE TABLE artifact_provenance (
  artifact_version_id uuid NOT NULL REFERENCES artifact_versions(id) ON DELETE CASCADE,
  entity_type         text NOT NULL,
  entity_id           uuid NOT NULL,
  label               text NOT NULL DEFAULT '',   -- e.g. D3, CP7
  role                text NOT NULL DEFAULT 'input' CHECK (role IN ('input','cited','base_checkpoint')),
  PRIMARY KEY (artifact_version_id, entity_type, entity_id)
);
CREATE INDEX artifact_provenance_entity_idx ON artifact_provenance (entity_type, entity_id);
CREATE TRIGGER artifact_provenance_immutable BEFORE UPDATE OR DELETE ON artifact_provenance
  FOR EACH ROW EXECUTE FUNCTION iv_forbid_history_mutation();

CREATE TABLE context_packs (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id          uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id        uuid REFERENCES branches(id) ON DELETE SET NULL,
  checkpoint_id    uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  title            text NOT NULL,
  objective        text NOT NULL DEFAULT '',
  content_markdown text NOT NULL,
  content_json     jsonb NOT NULL,
  token_estimate   integer NOT NULL DEFAULT 0,
  created_at       timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX context_packs_idea_idx ON context_packs (idea_id, created_at DESC);

-- External AI conversation brought back into IdeaVault, analysed against a branch.
CREATE TABLE deltas (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  idea_id         uuid NOT NULL REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id       uuid NOT NULL REFERENCES branches(id) ON DELETE CASCADE,
  conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
  source_id       uuid REFERENCES sources(id) ON DELETE SET NULL,
  context_pack_id uuid REFERENCES context_packs(id) ON DELETE SET NULL,
  status          text NOT NULL DEFAULT 'PENDING_REVIEW' CHECK (status IN ('PENDING_REVIEW','MERGED','PARTIALLY_MERGED','DISCARDED')),
  summary         text NOT NULL DEFAULT '',
  analyzer        text NOT NULL DEFAULT '',
  merge_checkpoint_id uuid REFERENCES checkpoints(id) ON DELETE SET NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  resolved_at     timestamptz
);
CREATE INDEX deltas_idea_idx ON deltas (idea_id, created_at DESC);

CREATE TABLE delta_items (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  delta_id       uuid NOT NULL REFERENCES deltas(id) ON DELETE CASCADE,
  classification text NOT NULL CHECK (classification IN ('NEW','CHANGED','REJECTED','UNCHANGED')),
  kind           text NOT NULL CHECK (kind IN ('decision','assumption','evidence','insight','question','action')),
  statement      text NOT NULL,
  details        text NOT NULL DEFAULT '',
  rationale      text NOT NULL DEFAULT '',
  source_excerpt text NOT NULL DEFAULT '',
  target_item_id uuid REFERENCES knowledge_items(id) ON DELETE SET NULL,
  confidence     real,
  selected       boolean NOT NULL DEFAULT true,
  merged_item_id uuid REFERENCES knowledge_items(id) ON DELETE SET NULL,
  position       integer NOT NULL DEFAULT 0
);
CREATE INDEX delta_items_delta_idx ON delta_items (delta_id, position);

-- Prompt analyses, thinking-pattern analyses, evolution analyses, contradiction scans...
CREATE TABLE analyses (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('prompt','thinking','evolution','contradictions','branch_comparison','checkpoint_comparison','conversation')),
  idea_id      uuid REFERENCES ideas(id) ON DELETE CASCADE,
  branch_id    uuid REFERENCES branches(id) ON DELETE SET NULL,
  subject_type text NOT NULL DEFAULT '',
  subject_id   uuid,
  input        jsonb NOT NULL DEFAULT '{}'::jsonb,
  result       jsonb NOT NULL DEFAULT '{}'::jsonb,
  analyzer     text NOT NULL DEFAULT '',
  agent_run_id uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX analyses_user_kind_idx ON analyses (user_id, kind, created_at DESC);
CREATE INDEX analyses_idea_idx ON analyses (idea_id, kind, created_at DESC);
