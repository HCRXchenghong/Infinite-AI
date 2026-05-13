CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone_verified_at TIMESTAMPTZ;
CREATE UNIQUE INDEX IF NOT EXISTS users_phone_unique_idx ON users (phone) WHERE phone IS NOT NULL AND phone <> '';

CREATE TABLE IF NOT EXISTS oauth_provider_configs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  provider_kind TEXT NOT NULL DEFAULT 'oauth2',
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  auth_url TEXT NOT NULL DEFAULT '',
  token_url TEXT NOT NULL DEFAULT '',
  userinfo_url TEXT NOT NULL DEFAULT '',
  scopes TEXT NOT NULL DEFAULT '',
  client_id_enc TEXT NOT NULL DEFAULT '',
  client_secret_enc TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE oauth_provider_configs ADD COLUMN IF NOT EXISTS logo_url TEXT NOT NULL DEFAULT '';
ALTER TABLE oauth_provider_configs ADD COLUMN IF NOT EXISTS user_id_field TEXT NOT NULL DEFAULT 'id';
ALTER TABLE oauth_provider_configs ADD COLUMN IF NOT EXISTS user_email_field TEXT NOT NULL DEFAULT 'email';
ALTER TABLE oauth_provider_configs ADD COLUMN IF NOT EXISTS user_name_field TEXT NOT NULL DEFAULT 'name';
ALTER TABLE oauth_provider_configs ADD COLUMN IF NOT EXISTS auth_params JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE oauth_provider_configs ADD COLUMN IF NOT EXISTS token_params JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS oauth_identities (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  provider_slug TEXT NOT NULL,
  provider_user_id TEXT NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  profile JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider_slug, provider_user_id)
);

CREATE TABLE IF NOT EXISTS admin_roles (
  slug TEXT PRIMARY KEY,
  description TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS admin_users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  display_name TEXT NOT NULL,
  role_slug TEXT NOT NULL REFERENCES admin_roles(slug),
  status TEXT NOT NULL DEFAULT 'active',
  totp_secret_enc TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS admin_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  admin_user_id UUID NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
  csrf_token TEXT NOT NULL,
  user_agent TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS conversations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title TEXT NOT NULL DEFAULT '新聊天',
  model_slug TEXT NOT NULL DEFAULT '',
  deep_search BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS attachments (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID REFERENCES conversations(id) ON DELETE SET NULL,
  object_key TEXT NOT NULL,
  bucket TEXT NOT NULL,
  file_name TEXT NOT NULL,
  mime_type TEXT NOT NULL,
  size_bytes BIGINT NOT NULL DEFAULT 0,
  extracted_text TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS messages (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  content TEXT NOT NULL DEFAULT '',
  reasoning_content TEXT NOT NULL DEFAULT '',
  attachments JSONB NOT NULL DEFAULT '[]'::jsonb,
  model_slug TEXT NOT NULL DEFAULT '',
  token_usage JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE messages ADD COLUMN IF NOT EXISTS reasoning_content TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS sources JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS artifacts JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE TABLE IF NOT EXISTS chat_runs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  status TEXT NOT NULL DEFAULT 'queued',
  model_slug TEXT NOT NULL DEFAULT '',
  deep_search BOOLEAN NOT NULL DEFAULT FALSE,
  user_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
  assistant_message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
  error_message TEXT NOT NULL DEFAULT '',
  cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS chat_run_events (
  seq BIGSERIAL PRIMARY KEY,
  run_id UUID NOT NULL REFERENCES chat_runs(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS chat_artifacts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE,
  message_id UUID REFERENCES messages(id) ON DELETE SET NULL,
  title TEXT NOT NULL DEFAULT '代码预览',
  kind TEXT NOT NULL DEFAULT 'html',
  entry_file TEXT NOT NULL DEFAULT 'index.html',
  files JSONB NOT NULL DEFAULT '[]'::jsonb,
  version INTEGER NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS chat_artifact_versions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  artifact_id UUID NOT NULL REFERENCES chat_artifacts(id) ON DELETE CASCADE,
  version INTEGER NOT NULL,
  files JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (artifact_id, version)
);

CREATE TABLE IF NOT EXISTS conversation_shares (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id UUID NOT NULL UNIQUE REFERENCES conversations(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  require_access_code BOOLEAN NOT NULL DEFAULT FALSE,
  access_code_enc TEXT NOT NULL DEFAULT '',
  collaboration_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS conversation_share_collaborators (
  share_id UUID NOT NULL REFERENCES conversation_shares(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (share_id, user_id)
);

CREATE TABLE IF NOT EXISTS plans (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  tier TEXT NOT NULL,
  price_cents INTEGER NOT NULL DEFAULT 0,
  interval TEXT NOT NULL DEFAULT 'month',
  description TEXT NOT NULL DEFAULT '',
  features JSONB NOT NULL DEFAULT '[]'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE
);

CREATE TABLE IF NOT EXISTS subscriptions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  plan_code TEXT NOT NULL REFERENCES plans(code),
  status TEXT NOT NULL DEFAULT 'active',
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ends_at TIMESTAMPTZ,
  auto_renew BOOLEAN NOT NULL DEFAULT TRUE,
  source TEXT NOT NULL DEFAULT '',
  source_reference TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS quota_ledgers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  amount NUMERIC(20,4) NOT NULL DEFAULT 0,
  balance_after NUMERIC(20,4) NOT NULL DEFAULT 0,
  source TEXT NOT NULL DEFAULT '',
  reference_id TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS api_keys (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  prefix TEXT NOT NULL,
  hash TEXT NOT NULL,
  scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
  status TEXT NOT NULL DEFAULT 'active',
  rate_limit_per_minute INTEGER NOT NULL DEFAULT 60,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS payment_orders (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  order_type TEXT NOT NULL,
  plan_code TEXT NOT NULL DEFAULT '',
  recharge_amount NUMERIC(20,4) NOT NULL DEFAULT 0,
  amount_cents INTEGER NOT NULL DEFAULT 0,
  currency TEXT NOT NULL DEFAULT 'CNY',
  ifpay_payment_id TEXT NOT NULL DEFAULT '',
  ifpay_order_id TEXT NOT NULL DEFAULT '',
  sub_method TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  description TEXT NOT NULL DEFAULT '',
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS payment_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  order_id UUID REFERENCES payment_orders(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  signature_ok BOOLEAN NOT NULL DEFAULT FALSE,
  processed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS system_logs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  service TEXT NOT NULL DEFAULT '',
  level TEXT NOT NULL DEFAULT 'info',
  category TEXT NOT NULL DEFAULT 'request',
  event_type TEXT NOT NULL DEFAULT '',
  method TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL DEFAULT '',
  status_code INTEGER NOT NULL DEFAULT 0,
  user_id TEXT NOT NULL DEFAULT '',
  admin_id TEXT NOT NULL DEFAULT '',
  account TEXT NOT NULL DEFAULT '',
  ip TEXT NOT NULL DEFAULT '',
  fingerprint TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL DEFAULT '',
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS model_routes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  protocol TEXT NOT NULL,
  strategy TEXT NOT NULL,
  model_type TEXT NOT NULL,
  upstream_model TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  sort_order INTEGER NOT NULL DEFAULT 0,
  prompt_enabled BOOLEAN NOT NULL DEFAULT FALSE,
  prompt_text TEXT NOT NULL DEFAULT '',
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE model_routes ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0;
ALTER TABLE model_routes ADD COLUMN IF NOT EXISTS prompt_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE model_routes ADD COLUMN IF NOT EXISTS prompt_text TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS model_endpoints (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  route_id UUID NOT NULL REFERENCES model_routes(id) ON DELETE CASCADE,
  sort_order INTEGER NOT NULL DEFAULT 0,
  base_url TEXT NOT NULL,
  secret_enc TEXT NOT NULL DEFAULT '',
  extra_headers JSONB NOT NULL DEFAULT '{}'::jsonb,
  active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS endpoint_adaptations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  operation TEXT NOT NULL,
  protocol TEXT NOT NULL DEFAULT 'openai',
  route_slug TEXT NOT NULL DEFAULT '',
  upstream_model TEXT NOT NULL DEFAULT '',
  base_url TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  url TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (operation, protocol, route_slug, upstream_model, base_url)
);

CREATE TABLE IF NOT EXISTS redeem_campaigns (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL,
  plan_code TEXT NOT NULL REFERENCES plans(code),
  duration_months INTEGER NOT NULL DEFAULT 1,
  lifetime BOOLEAN NOT NULL DEFAULT FALSE,
  account_type TEXT NOT NULL DEFAULT 'has_account',
  max_uses INTEGER NOT NULL DEFAULT 1,
  expires_at TIMESTAMPTZ,
  created_by_admin_id UUID REFERENCES admin_users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS redeem_codes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  campaign_id UUID NOT NULL REFERENCES redeem_campaigns(id) ON DELETE CASCADE,
  code TEXT NOT NULL UNIQUE,
  gift_link TEXT NOT NULL DEFAULT '',
  max_uses INTEGER NOT NULL DEFAULT 1,
  remaining_uses INTEGER NOT NULL DEFAULT 1,
  expires_at TIMESTAMPTZ,
  status TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS redeem_claims (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  redeem_code_id UUID NOT NULL REFERENCES redeem_codes(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  claim_status TEXT NOT NULL DEFAULT 'claimed',
  claimed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (redeem_code_id, user_id)
);

CREATE TABLE IF NOT EXISTS affiliate_invites (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  code TEXT NOT NULL UNIQUE,
  created_by_admin_id UUID REFERENCES admin_users(id) ON DELETE SET NULL,
  source_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  consumed_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  consumed_at TIMESTAMPTZ,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS download_releases (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  platform TEXT NOT NULL,
  channel TEXT NOT NULL DEFAULT 'stable',
  version TEXT NOT NULL,
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  download_url TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'unavailable',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS user_settings (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
  theme TEXT NOT NULL DEFAULT 'dark',
  language TEXT NOT NULL DEFAULT 'auto',
  deep_search_default BOOLEAN NOT NULL DEFAULT FALSE,
  selected_model_slug TEXT NOT NULL DEFAULT '',
  chat_history_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  memory_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS selected_model_slug TEXT NOT NULL DEFAULT '';
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS chat_history_enabled BOOLEAN NOT NULL DEFAULT TRUE;
ALTER TABLE user_settings ADD COLUMN IF NOT EXISTS memory_enabled BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE IF NOT EXISTS system_settings (
  key TEXT PRIMARY KEY,
  value JSONB NOT NULL DEFAULT '{}'::jsonb,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_conversations_user_updated ON conversations (user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_conversation_created ON messages (conversation_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_chat_runs_user_conversation ON chat_runs (user_id, conversation_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_runs_active ON chat_runs (conversation_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_chat_run_events_run_seq ON chat_run_events (run_id, seq ASC);
CREATE INDEX IF NOT EXISTS idx_chat_artifacts_user_conversation ON chat_artifacts (user_id, conversation_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_shares_user_active ON conversation_shares (user_id, is_active, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversation_share_collaborators_share ON conversation_share_collaborators (share_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_user_sessions_user_expires ON user_sessions (user_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_admin_sessions_user_expires ON admin_sessions (admin_user_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_status ON api_keys (user_id, status);
CREATE INDEX IF NOT EXISTS idx_payment_orders_user_status ON payment_orders (user_id, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_redeem_codes_status_expires ON redeem_codes (status, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_system_logs_created_at ON system_logs (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_system_logs_service_category ON system_logs (service, category, created_at DESC);
