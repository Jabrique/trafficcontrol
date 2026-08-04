-- IP Rule System: configurable per-endpoint IP allowlist
-- Rules are managed via Traffic Portal UI, cached in-memory (TTL=30s).
-- Migration: 2026080302000000_create_api_ip_rule.up.sql

CREATE TABLE IF NOT EXISTS api_ip_rule (
    id              BIGSERIAL PRIMARY KEY,
    name            VARCHAR(256) NOT NULL UNIQUE,
    description     TEXT,

    -- Go regex pattern matched against URL path AFTER stripping /api/X.Y/ prefix.
    -- Examples: '^user/api_tokens', '^deliveryservice_stats$', '.*'
    endpoint_regex  TEXT NOT NULL,

    -- NULL = all HTTP methods; or specific array e.g. ARRAY['GET','POST','DELETE']
    http_methods    TEXT[] DEFAULT NULL,

    -- IPs that are ALLOWED; NULL/empty array = allow all IPs (no restriction)
    allowed_cidrs   TEXT[] DEFAULT NULL,

    -- IPs that are ALWAYS DENIED — evaluated BEFORE allowed_cidrs
    denied_cidrs    TEXT[] DEFAULT NULL,

    -- Whether this rule applies to requests authenticated via API token
    applies_to_api_token  BOOLEAN NOT NULL DEFAULT TRUE,
    -- Whether this rule applies to requests authenticated via cookie/session
    applies_to_session    BOOLEAN NOT NULL DEFAULT FALSE,

    -- Lower priority number = higher precedence. First matching rule wins.
    priority        INT NOT NULL DEFAULT 100,

    active          BOOLEAN NOT NULL DEFAULT TRUE,
    last_updated    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_by      BIGINT REFERENCES tm_user(id) ON DELETE SET NULL
);

-- Primary cache query: load active rules sorted by priority
CREATE INDEX idx_api_ip_rule_active   ON api_ip_rule(active, priority);
-- Secondary: regex text for potential future partial lookups
CREATE INDEX idx_api_ip_rule_endpoint ON api_ip_rule(endpoint_regex);

-- =============================================================================
-- DEFAULT SEED RULE: fail-closed protection for token/rule management endpoints
-- =============================================================================
-- This rule is REQUIRED for security. Do NOT remove it from this migration.
-- Without it, there is a fail-open window at first startup.
--
-- DESIGN NOTE: endpoint_regex has no trailing $ - INTENTIONAL.
--   Without $, nested paths like users/123/api_tokens/456 also match.
--   This is fail-safe (over-restrictive), not over-permissive.
--   Admin can create exception rules with lower priority if needed.
--
-- POST-DEPLOY ACTION REQUIRED: Update this rule via Traffic Portal to add
--   your backend server IP(s) to allowed_cidrs before onboarding customers.
--   Example: ARRAY['127.0.0.1/32', '::1/128', '10.20.0.5/32']
--
-- applies_to_session=FALSE: admin browser sessions bypass this rule entirely
--   so admins are NEVER locked out when managing rules from any IP.
-- priority=1: no other rule can shadow this one.
-- =============================================================================
INSERT INTO api_ip_rule (
    name,
    description,
    endpoint_regex,
    http_methods,
    allowed_cidrs,
    applies_to_api_token,
    applies_to_session,
    priority,
    active
) VALUES (
    'token-management-localhost-only',
    'DEFAULT RULE: Token management and IP rule management accessible from localhost only via API token. '
    'UPDATE allowed_cidrs to add backend server IP(s) before onboarding customers. '
    'Cookie/session auth (admin browser) is NOT subject to this rule.',
    '^(user/api_tokens|users/[0-9]+/api_tokens|api_ip_rules)',
    NULL,
    ARRAY['127.0.0.1/32', '::1/128'],
    TRUE,
    FALSE,
    1,
    TRUE
);

-- Register capabilities for the API IP Rule feature.
-- Admin-only: IP rules are security-critical configuration.
INSERT INTO public.capability (name, description) VALUES
    ('API-IP-RULE:READ',   'List and view API IP rules'),
    ('API-IP-RULE:CREATE', 'Create a new API IP rule'),
    ('API-IP-RULE:UPDATE', 'Update an existing API IP rule'),
    ('API-IP-RULE:DELETE', 'Delete an API IP rule')
ON CONFLICT (name) DO NOTHING;

-- Grant API-IP-RULE capabilities to admin only.
-- These rules control security configuration and must not be accessible to other roles.
INSERT INTO public.role_capability (role_id, cap_name)
SELECT r.id, c.name
FROM public.role r
CROSS JOIN (VALUES
    ('API-IP-RULE:READ'),
    ('API-IP-RULE:CREATE'),
    ('API-IP-RULE:UPDATE'),
    ('API-IP-RULE:DELETE')
) AS c(name)
WHERE r.name = 'admin'
ON CONFLICT DO NOTHING;
