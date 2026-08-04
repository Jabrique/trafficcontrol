package api

/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/auth"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

// isAPITokenAuthCtxKey is the unexported type for the API token auth context key.
// Using a dedicated unexported struct type prevents collisions with any other context values.
type isAPITokenAuthCtxKey struct{}

// IsAPITokenAuthKey is the context key set by GetUserFromReq after successful API token
// authentication. Its presence (value != nil) signals to IPRuleMiddleware and other
// downstream middleware that the request is authenticated via an API token (scoped or unscoped).
//
// Consumer pattern:
//
//	isAPITokenAuth := r.Context().Value(api.IsAPITokenAuthKey) != nil
var IsAPITokenAuthKey = isAPITokenAuthCtxKey{}

// lastUsedDebounceInterval controls how often last_used_at is written per token.
// Updates are skipped if the field was updated within this interval.
// Reduces write amplification for high-frequency tokens.
const lastUsedDebounceInterval = 5 * time.Minute

// lastUsedSemaphore limits concurrent goroutines updating last_used_at.
// Initialised by InitAPITokenAuth. If the semaphore is full, the update is
// silently dropped — no data corruption occurs.
var lastUsedSemaphore chan struct{}

// ─────────────────────────────────────────────────────────────────────────────
// Token bucket rate limiter (stdlib only — no external dependencies)
// ─────────────────────────────────────────────────────────────────────────────

// tokenBucket is a single per-key rate limiter using the token bucket algorithm.
// It is safe for concurrent use.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	maxBurst float64
	rate     float64 // tokens refilled per second
	lastTime time.Time
}

func newTokenBucket(ratePerSec, maxBurst float64) *tokenBucket {
	return &tokenBucket{
		tokens:   maxBurst,
		maxBurst: maxBurst,
		rate:     ratePerSec,
		lastTime: time.Now(),
	}
}

// Allow returns true if a token is available (request is within rate limit).
func (tb *tokenBucket) Allow() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	now := time.Now()
	elapsed := now.Sub(tb.lastTime).Seconds()
	tb.tokens = math.Min(tb.maxBurst, tb.tokens+elapsed*tb.rate)
	tb.lastTime = now
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// rateLimiterMap manages per-key token buckets.
// New buckets are created lazily on first access. Safe for concurrent use.
type rateLimiterMap struct {
	limiters   sync.Map
	ratePerSec float64
	maxBurst   float64
}

func newRateLimiterMap(perMinute int) *rateLimiterMap {
	if perMinute <= 0 {
		perMinute = 100
	}
	rps := float64(perMinute) / 60.0
	burst := math.Max(1, float64(perMinute)/10)
	return &rateLimiterMap{
		ratePerSec: rps,
		maxBurst:   burst,
	}
}

// Allow returns true if the key is within its rate limit, false if exceeded.
func (m *rateLimiterMap) Allow(key string) bool {
	v, loaded := m.limiters.Load(key)
	if !loaded {
		tb := newTokenBucket(m.ratePerSec, m.maxBurst)
		// LoadOrStore is atomic — if two goroutines race, only one bucket wins.
		actual, _ := m.limiters.LoadOrStore(key, tb)
		v = actual
	}
	return v.(*tokenBucket).Allow()
}

// Package-level rate limiters — initialised by InitAPITokenAuth at startup.
var (
	globalIPRateLimiter    *rateLimiterMap
	globalTokenRateLimiter *rateLimiterMap
)

// InitAPITokenAuth initialises the package-level semaphore and rate limiters.
// Must be called in main() before RegisterRoutes.
func InitAPITokenAuth(maxAsyncUpdates, ipRateLimitPerMin, tokenRateLimitPerMin int) {
	if maxAsyncUpdates <= 0 {
		maxAsyncUpdates = 50
	}
	lastUsedSemaphore = make(chan struct{}, maxAsyncUpdates)
	globalIPRateLimiter = newRateLimiterMap(ipRateLimitPerMin)
	globalTokenRateLimiter = newRateLimiterMap(tokenRateLimitPerMin)
}

// ─────────────────────────────────────────────────────────────────────────────
// IP utility helpers (private to api package — avoids circular import with iprule)
// ─────────────────────────────────────────────────────────────────────────────

// extractClientIPForToken returns the real client IP address for token authentication.
// Respects X-Forwarded-For only if the direct connection comes from a trusted proxy.
// This mirrors iprule.ExtractClientIP but is duplicated here to avoid the import cycle:
//
//	api → iprule → api (iprule/handlers.go imports api)
func extractClientIPForToken(r *http.Request, trustedProxyCIDRs []*net.IPNet) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	if isIPInTrustedCIDRs(remoteHost, trustedProxyCIDRs) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			candidate := strings.TrimSpace(parts[0])
			if net.ParseIP(candidate) != nil {
				return candidate
			}
		}
	}
	return remoteHost
}

// isIPInTrustedCIDRs returns true if ipStr is contained in any of the pre-parsed CIDRs.
func isIPInTrustedCIDRs(ipStr string, nets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// isIPInCIDRStringList returns true if ipStr falls within any of the CIDR strings.
// Used for per-token allowed_cidrs check.
func isIPInCIDRStringList(ipStr string, cidrs []string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, cidrStr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidrStr)
		if err != nil {
			continue
		}
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Core authentication function
// ─────────────────────────────────────────────────────────────────────────────

// authenticateAPIToken validates a raw API token (format: to_at_<publicID>_<secret>)
// and returns the authenticated CurrentUser on success.
//
// Authentication steps (in order):
//  1. IP-level rate limit check (BEFORE any DB query — throttles brute force)
//  2. Token structure validation (length guard + format check)
//  3. SHA-256(secret) computation
//  4. DB lookup: token_hash + token_prefix + not expired (single JOIN query)
//  5. Per-token IP allowlist check (if token.allowed_cidrs is non-empty)
//  6. Per-token rate limit check
//  7. Load user fresh from DB — bypass cache for role-change correctness
//  8. Check disallowed role
//  9. Set EffectivePrivLevel explicitly (prevents zero-value invariant violations)
//  10. Apply scoped permissions if token has scoped_permissions
//  11. Async update last_used_at (debounced, non-blocking via semaphore)
//  12. Structured success log
func authenticateAPIToken(w http.ResponseWriter, r *http.Request, rawToken string) (auth.CurrentUser, error, error, int) {
	db, err := GetDB(r.Context())
	if err != nil {
		return auth.CurrentUser{}, nil, fmt.Errorf("getting db from context: %w", err), http.StatusInternalServerError
	}
	cfg, cfgErr := GetConfig(r.Context())
	if cfgErr != nil {
		return auth.CurrentUser{}, nil, fmt.Errorf("getting config from context: %w", cfgErr), http.StatusInternalServerError
	}
	timeout := time.Duration(cfg.DBQueryTimeoutSeconds) * time.Second

	// Step 1: IP-level rate limit BEFORE any DB query.
	// Throttles brute-force including invalid/non-existent tokens.
	clientIP := extractClientIPForToken(r, cfg.ParsedTrustedProxyCIDRs)
	if globalIPRateLimiter != nil && !globalIPRateLimiter.Allow(clientIP) {
		w.Header().Set("Retry-After", "60")
		return auth.CurrentUser{}, errors.New("rate limit exceeded"), nil, http.StatusTooManyRequests
	}

	// Step 2: Token structure validation.
	// Length guard: reject before hashing to prevent hash-DoS via very long strings.
	if len(rawToken) > 128 {
		log.Warnf("API_TOKEN_AUTH_FAILED reason=invalid_format ip=%s (token too long)", clientIP)
		return auth.CurrentUser{}, errors.New("invalid token format"), nil, http.StatusBadRequest
	}
	withoutPrefix := strings.TrimPrefix(rawToken, tc.APITokenPrefix)
	parts := strings.SplitN(withoutPrefix, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		log.Warnf("API_TOKEN_AUTH_FAILED reason=invalid_format ip=%s", clientIP)
		return auth.CurrentUser{}, errors.New("unauthorized"), nil, http.StatusUnauthorized
	}
	publicID, secretPart := parts[0], parts[1]
	tokenPrefix := tc.APITokenPrefix + publicID

	// Step 3: Hash only the secret part — never the full token or public ID.
	hashBytes := sha256.Sum256([]byte(secretPart))
	tokenHash := hex.EncodeToString(hashBytes[:])

	// Step 4: DB lookup — must match hash AND prefix AND not be expired.
	var username string
	var scopedPerms pq.StringArray
	var allowedCIDRs pq.StringArray
	dbCtx, dbCancel := context.WithTimeout(context.Background(), timeout)
	defer dbCancel()

	err = db.QueryRowContext(dbCtx, `
		SELECT u.username, at.scoped_permissions, at.allowed_cidrs
		FROM api_token at
		JOIN tm_user u ON at.user_id = u.id
		WHERE at.token_hash = $1
		  AND at.token_prefix = $2
		  AND at.expires_at > NOW()
	`, tokenHash, tokenPrefix).Scan(&username, &scopedPerms, &allowedCIDRs)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Log every 401 for anomaly detection.
			// Do NOT distinguish expired vs not-found — prevents timing oracle.
			log.Warnf("API_TOKEN_AUTH_FAILED reason=not_found ip=%s path=%s", clientIP, r.URL.Path)
			return auth.CurrentUser{}, errors.New("unauthorized"), nil, http.StatusUnauthorized
		}
		return auth.CurrentUser{}, nil, fmt.Errorf("querying api_token: %w", err), http.StatusInternalServerError
	}

	// Step 5: Per-token IP allowlist check (after token confirmed valid).
	// Use 403 not 401 — avoids confirming to an attacker that the token is valid.
	if len(allowedCIDRs) > 0 {
		if !isIPInCIDRStringList(clientIP, []string(allowedCIDRs)) {
			log.Warnf("API_TOKEN_AUTH_FAILED reason=ip_not_allowed ip=%s prefix=%s", clientIP, tokenPrefix)
			return auth.CurrentUser{}, errors.New("access denied"), nil, http.StatusForbidden
		}
	}

	// Step 6: Per-token rate limit (keyed on first 16 hex chars of tokenHash).
	// Key is a safe portion of the hash — secret never appears here.
	if globalTokenRateLimiter != nil && !globalTokenRateLimiter.Allow(tokenHash[:16]) {
		w.Header().Set("Retry-After", "60")
		return auth.CurrentUser{}, errors.New("rate limit exceeded"), nil, http.StatusTooManyRequests
	}

	// Step 7: Load user fresh from DB — bypass in-memory cache.
	// Ensures role downgrades (e.g. disabled account, role change) take effect immediately.
	user, userErr, sysErr, code := auth.GetCurrentUserFromDBDirect(db, username, timeout)
	if userErr != nil || sysErr != nil {
		return auth.CurrentUser{}, userErr, sysErr, code
	}

	// Step 8: Check disallowed role.
	if user.RoleName == auth.DisallowedRoleName {
		log.Warnf("API_TOKEN_AUTH_FAILED reason=disallowed user=%s", username)
		return auth.CurrentUser{}, errors.New("account disabled"), nil, http.StatusUnauthorized
	}

	// Step 9: Set EffectivePrivLevel explicitly for ALL tokens (including unscoped).
	// Without this, unscoped tokens have EffectivePrivLevel=0 (zero value of int).
	// While harmless today (IsAPITokenScoped=false guards the path), being explicit
	// prevents a future refactoring from creating a silent invariant violation.
	user.EffectivePrivLevel = user.PrivLevel

	// Step 10: Apply scoped permissions if this token has them.
	if len(scopedPerms) > 0 {
		// Scoped permissions are meaningless without role-based permission enforcement.
		if !cfg.RoleBasedPermissions {
			return auth.CurrentUser{}, errors.New("scoped tokens require RoleBasedPermissions=true in cdn.conf"), nil, http.StatusForbidden
		}
		// ApplyTokenPermissionScope computes: Capabilities = scopedPerms ∩ user.Capabilities
		// and sets IsAPITokenScoped=true + EffectivePrivLevel=PrivLevelReadOnly.
		user = auth.ApplyTokenPermissionScope(user, []string(scopedPerms))
	}

	// Step 11: Async update last_used_at with debounce.
	// Non-blocking select: if semaphore is full, update is silently skipped.
	select {
	case lastUsedSemaphore <- struct{}{}:
		go func() {
			defer func() { <-lastUsedSemaphore }()
			updateTokenLastUsedDebounced(db, tokenHash)
		}()
	default:
		// Semaphore full — drop this update. No data corruption (SQL UPDATE is idempotent).
	}

	// Step 12: Structured success log (prefix is safe; secret never logged).
	log.Infof("API_TOKEN_AUTH user=%s prefix=%s ip=%s method=%s path=%s",
		user.UserName, tokenPrefix, clientIP, r.Method, r.URL.Path)

	return user, nil, nil, http.StatusOK
}

// updateTokenLastUsedDebounced updates api_token.last_used_at only if the last
// update was more than 5 minutes ago. Reduces write amplification for high-frequency tokens.
// Called from a goroutine; errors are silently ignored (update is best-effort).
func updateTokenLastUsedDebounced(db *sqlx.DB, tokenHash string) {
	_, _ = db.Exec(`
		UPDATE api_token SET last_used_at = NOW()
		WHERE token_hash = $1
		  AND (last_used_at IS NULL OR last_used_at < NOW() - INTERVAL '5 minutes')
	`, tokenHash)
}
