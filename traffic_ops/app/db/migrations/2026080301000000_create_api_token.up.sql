-- API Token System: to_at_<publicID>_<secret>
-- publicID (12 base62 chars) → stored as token_prefix → safe to log/display
-- secret (52 base62 chars)   → SHA-256(secret) = token_hash → NEVER logged
--
-- Migration: 2026080301000000_create_api_token.up.sql

CREATE TABLE IF NOT EXISTS api_token (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL REFERENCES tm_user(id) ON DELETE CASCADE,
    name                TEXT NOT NULL,

    -- SHA-256(secret_part) — not hash of the full token
    -- NEVER stored or logged as plaintext
    token_hash          TEXT NOT NULL UNIQUE,

    -- "to_at_" + publicID (12 chars) = 18 chars total
    -- Safe to log, display in UI, use as rate limiter display key
    token_prefix        TEXT NOT NULL,

    expires_at          TIMESTAMPTZ NOT NULL,

    -- NULL = inherit all user permissions
    -- If set: MUST be ⊆ user's permissions (validated at create-time AND auth-time)
    scoped_permissions  TEXT[] DEFAULT NULL,

    -- NULL = no IP restriction on this token
    -- If set: request must originate from one of these CIDRs
    allowed_cidrs       TEXT[] DEFAULT NULL,

    last_used_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One token name is unique per user (different users can have same name)
    CONSTRAINT api_token_user_name_unique UNIQUE (user_id, name),
    -- Ensure prefix always has the correct format
    CONSTRAINT api_token_check_prefix CHECK (token_prefix LIKE 'to_at_%')
);

-- Primary lookup: validation during auth (most frequent query)
CREATE INDEX idx_api_token_hash      ON api_token(token_hash);
-- Admin: list tokens for a user
CREATE INDEX idx_api_token_user_id   ON api_token(user_id);
-- Background cleanup: find and purge expired tokens
CREATE INDEX idx_api_token_expires   ON api_token(expires_at);
-- UI display: lookup by prefix for human-readable identification
CREATE INDEX idx_api_token_prefix    ON api_token(token_prefix);

-- Register capabilities for the API Token feature.
-- Required so that GET /api/3.0/capabilities returns them and they can
-- be assigned to roles and selected in the portal capability picker.
INSERT INTO public.capability (name, description) VALUES
    ('API-TOKEN:READ',   'List and view API tokens owned by the current user'),
    ('API-TOKEN:CREATE', 'Create a new API token for the current user'),
    ('API-TOKEN:DELETE', 'Revoke or delete an API token owned by the current user')
ON CONFLICT (name) DO NOTHING;

-- Grant API-TOKEN capabilities to all non-disallowed roles.
-- admin: already bypasses capability checks via the ALL capability.
-- operations, read-only, portal, steering: regular users who need token management.
INSERT INTO public.role_capability (role_id, cap_name)
SELECT r.id, c.name
FROM public.role r
CROSS JOIN (VALUES
    ('API-TOKEN:READ'),
    ('API-TOKEN:CREATE'),
    ('API-TOKEN:DELETE')
) AS c(name)
WHERE r.name IN ('admin', 'operations', 'read-only', 'portal', 'steering', 'federation')
ON CONFLICT DO NOTHING;
