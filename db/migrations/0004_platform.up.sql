-- 0004: agent tool calls, model registry, usage, connectors, credentials,
-- imports, background jobs, embeddings and the model lab.

CREATE TABLE tool_calls (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  agent_run_id  uuid NOT NULL REFERENCES agent_runs(id) ON DELETE CASCADE,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider_call_id text NOT NULL DEFAULT '',
  tool_name     text NOT NULL,
  category      text NOT NULL CHECK (category IN ('READ','ANALYZE','WRITE','DESTRUCTIVE','EXTERNAL')),
  arguments     jsonb NOT NULL DEFAULT '{}'::jsonb,
  result        jsonb,
  summary       text NOT NULL DEFAULT '',
  status        text NOT NULL DEFAULT 'PENDING'
                CHECK (status IN ('PENDING','RUNNING','SUCCEEDED','FAILED','DENIED','AWAITING_CONFIRMATION','REJECTED_BY_POLICY')),
  error         text NOT NULL DEFAULT '',
  latency_ms    integer NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  completed_at  timestamptz
);
CREATE INDEX tool_calls_run_idx ON tool_calls (agent_run_id, created_at);
CREATE INDEX tool_calls_pending_idx ON tool_calls (user_id) WHERE status = 'AWAITING_CONFIRMATION';

CREATE TABLE model_configs (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  provider             text NOT NULL,
  model                text NOT NULL,
  display_name         text NOT NULL DEFAULT '',
  capabilities         text[] NOT NULL DEFAULT '{}',
  context_length       integer NOT NULL DEFAULT 8192,
  tool_calling         boolean NOT NULL DEFAULT false,
  structured_output    boolean NOT NULL DEFAULT false,
  vision               boolean NOT NULL DEFAULT false,
  reasoning            boolean NOT NULL DEFAULT false,
  relative_cost        integer NOT NULL DEFAULT 3 CHECK (relative_cost BETWEEN 0 AND 5),
  speed                integer NOT NULL DEFAULT 3 CHECK (speed BETWEEN 1 AND 5),
  quality              integer NOT NULL DEFAULT 3 CHECK (quality BETWEEN 1 AND 5),
  input_cost_per_mtok  numeric(12,4) NOT NULL DEFAULT 0,
  output_cost_per_mtok numeric(12,4) NOT NULL DEFAULT 0,
  enabled              boolean NOT NULL DEFAULT true,
  is_builtin           boolean NOT NULL DEFAULT true,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT model_configs_unique UNIQUE (provider, model)
);
CREATE TRIGGER model_configs_touch BEFORE UPDATE ON model_configs FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

CREATE TABLE usage_events (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id            uuid REFERENCES users(id) ON DELETE CASCADE,
  model_call_id      uuid NOT NULL DEFAULT gen_random_uuid(),
  provider           text NOT NULL,
  model              text NOT NULL,
  operation          text NOT NULL,         -- chat | chat_stream | embed | structured
  task               text NOT NULL,         -- supervisor | extraction | synthesis | ...
  input_tokens       integer NOT NULL DEFAULT 0,
  output_tokens      integer NOT NULL DEFAULT 0,
  total_tokens       integer NOT NULL DEFAULT 0,
  tokens_estimated   boolean NOT NULL DEFAULT false,
  estimated_cost_usd numeric(14,8) NOT NULL DEFAULT 0,
  latency_ms         integer NOT NULL DEFAULT 0,
  tool_calls         integer NOT NULL DEFAULT 0,
  success            boolean NOT NULL DEFAULT true,
  error              text NOT NULL DEFAULT '',
  conversation_id    uuid REFERENCES conversations(id) ON DELETE SET NULL,
  idea_id            uuid REFERENCES ideas(id) ON DELETE SET NULL,
  agent_run_id       uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX usage_events_user_time_idx ON usage_events (user_id, created_at DESC);
CREATE INDEX usage_events_model_idx ON usage_events (provider, model, created_at DESC);

CREATE TABLE connectors (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  connector_key   text NOT NULL,
  status          text NOT NULL DEFAULT 'NOT_CONNECTED' CHECK (status IN ('CONNECTED','NOT_CONNECTED','ERROR')),
  config          jsonb NOT NULL DEFAULT '{}'::jsonb,
  granted_permissions text[] NOT NULL DEFAULT '{}',
  last_error      text NOT NULL DEFAULT '',
  last_checked_at timestamptz,
  connected_at    timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT connectors_unique UNIQUE (user_id, connector_key)
);
CREATE TRIGGER connectors_touch BEFORE UPDATE ON connectors FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

-- Secrets are AES-256-GCM encrypted with a key that never touches the database.
CREATE TABLE credentials (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  owner_type   text NOT NULL CHECK (owner_type IN ('connector','model_provider')),
  owner_key    text NOT NULL,
  name         text NOT NULL DEFAULT 'api_key',
  ciphertext   bytea NOT NULL,
  nonce        bytea NOT NULL,
  key_version  integer NOT NULL DEFAULT 1,
  hint         text NOT NULL DEFAULT '',   -- masked, e.g. "sk-…a1b2"
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  CONSTRAINT credentials_unique UNIQUE (user_id, owner_type, owner_key, name)
);
CREATE TRIGGER credentials_touch BEFORE UPDATE ON credentials FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

CREATE TABLE connector_events (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  connector_key text NOT NULL,
  tool          text NOT NULL DEFAULT '',
  operation     text NOT NULL,
  success       boolean NOT NULL,
  latency_ms    integer NOT NULL DEFAULT 0,
  error         text NOT NULL DEFAULT '',
  agent_run_id  uuid REFERENCES agent_runs(id) ON DELETE SET NULL,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX connector_events_user_time_idx ON connector_events (user_id, created_at DESC);

CREATE TABLE imports (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  source_kind     text NOT NULL CHECK (source_kind IN ('file','paste','url')),
  provider        text NOT NULL DEFAULT '',
  adapter         text NOT NULL DEFAULT '',
  status          text NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED','PROCESSING','PREVIEW','COMPLETED','FAILED','PARTIAL','CANCELLED')),
  stage           text NOT NULL DEFAULT 'parse' CHECK (stage IN ('parse','commit')),
  filename        text NOT NULL DEFAULT '',
  uri             text NOT NULL DEFAULT '',
  byte_size       bigint NOT NULL DEFAULT 0,
  content_hash    text NOT NULL DEFAULT '',
  -- Raw payload is kept only until parsing finishes, then cleared.
  raw_payload     bytea,
  total_items     integer NOT NULL DEFAULT 0,
  processed_items integer NOT NULL DEFAULT 0,
  failed_items    integer NOT NULL DEFAULT 0,
  options         jsonb NOT NULL DEFAULT '{}'::jsonb,
  warnings        jsonb NOT NULL DEFAULT '[]'::jsonb,
  error           text NOT NULL DEFAULT '',
  target_idea_id  uuid REFERENCES ideas(id) ON DELETE SET NULL,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now(),
  completed_at    timestamptz
);
CREATE INDEX imports_user_idx ON imports (user_id, created_at DESC);
CREATE TRIGGER imports_touch BEFORE UPDATE ON imports FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

CREATE TABLE import_items (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  import_id           uuid NOT NULL REFERENCES imports(id) ON DELETE CASCADE,
  user_id             uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  position            integer NOT NULL,
  external_id         text NOT NULL DEFAULT '',
  title               text NOT NULL DEFAULT '',
  status              text NOT NULL DEFAULT 'PENDING'
                      CHECK (status IN ('PENDING','SELECTED','SKIPPED','IMPORTED','DUPLICATE','FAILED')),
  message_count       integer NOT NULL DEFAULT 0,
  started_at          timestamptz,
  content_hash        text NOT NULL DEFAULT '',
  payload             jsonb NOT NULL,           -- normalized conversation (untrusted data)
  injection_flags     text[] NOT NULL DEFAULT '{}',
  duplicate_of        uuid REFERENCES conversations(id) ON DELETE SET NULL,
  conversation_id     uuid REFERENCES conversations(id) ON DELETE SET NULL,
  idea_id             uuid REFERENCES ideas(id) ON DELETE SET NULL,
  extracted_count     integer NOT NULL DEFAULT 0,
  error               text NOT NULL DEFAULT '',
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT import_items_position_unique UNIQUE (import_id, position)
);
CREATE INDEX import_items_import_idx ON import_items (import_id, position);
CREATE TRIGGER import_items_touch BEFORE UPDATE ON import_items FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

-- PostgreSQL-backed job queue (SELECT ... FOR UPDATE SKIP LOCKED). No external queue required.
CREATE TABLE jobs (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid REFERENCES users(id) ON DELETE CASCADE,
  kind         text NOT NULL,
  payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
  status       text NOT NULL DEFAULT 'QUEUED' CHECK (status IN ('QUEUED','RUNNING','COMPLETED','FAILED')),
  attempts     integer NOT NULL DEFAULT 0,
  max_attempts integer NOT NULL DEFAULT 3,
  run_after    timestamptz NOT NULL DEFAULT now(),
  locked_at    timestamptz,
  locked_by    text NOT NULL DEFAULT '',
  last_error   text NOT NULL DEFAULT '',
  dedupe_key   text,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
CREATE INDEX jobs_ready_idx ON jobs (run_after) WHERE status = 'QUEUED';
CREATE UNIQUE INDEX jobs_dedupe_idx ON jobs (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('QUEUED','RUNNING');
CREATE TRIGGER jobs_touch BEFORE UPDATE ON jobs FOR EACH ROW EXECUTE FUNCTION iv_touch_updated_at();

-- Embeddings for semantic retrieval. Dimension is fixed at 768; providers that
-- support it are asked for 768-d output so vectors stay comparable per model.
CREATE TABLE embeddings (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  entity_type  text NOT NULL,
  entity_id    uuid NOT NULL,
  idea_id      uuid REFERENCES ideas(id) ON DELETE CASCADE,
  chunk_index  integer NOT NULL DEFAULT 0,
  content      text NOT NULL,
  content_hash text NOT NULL,
  provider     text NOT NULL,
  model        text NOT NULL,
  embedding    vector(768) NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  CONSTRAINT embeddings_unique UNIQUE (entity_type, entity_id, chunk_index, model)
);
CREATE INDEX embeddings_entity_idx ON embeddings (entity_type, entity_id);
CREATE INDEX embeddings_user_model_idx ON embeddings (user_id, model);
CREATE INDEX embeddings_hnsw_idx ON embeddings USING hnsw (embedding vector_cosine_ops);

CREATE TABLE model_lab_runs (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  task         text NOT NULL,
  title        text NOT NULL DEFAULT '',
  system       text NOT NULL DEFAULT '',
  prompt       text NOT NULL,
  expect_json  boolean NOT NULL DEFAULT false,
  json_schema  jsonb,
  reference    text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT 'RUNNING' CHECK (status IN ('RUNNING','COMPLETED','FAILED')),
  created_at   timestamptz NOT NULL DEFAULT now(),
  completed_at timestamptz
);
CREATE INDEX model_lab_runs_user_idx ON model_lab_runs (user_id, created_at DESC);

CREATE TABLE model_lab_results (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id             uuid NOT NULL REFERENCES model_lab_runs(id) ON DELETE CASCADE,
  provider           text NOT NULL,
  model              text NOT NULL,
  output             text NOT NULL DEFAULT '',
  valid_json         boolean,
  schema_errors      jsonb NOT NULL DEFAULT '[]'::jsonb,
  correctness        real,
  error              text NOT NULL DEFAULT '',
  latency_ms         integer NOT NULL DEFAULT 0,
  input_tokens       integer NOT NULL DEFAULT 0,
  output_tokens      integer NOT NULL DEFAULT 0,
  total_tokens       integer NOT NULL DEFAULT 0,
  estimated_cost_usd numeric(14,8) NOT NULL DEFAULT 0,
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX model_lab_results_run_idx ON model_lab_results (run_id);
