package iprule

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
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/jmoiron/sqlx"
)

// ipRule is the in-memory representation of one api_ip_rule row.
// The compiled regexp is stored pre-compiled to avoid re-compilation on every request.
type ipRule struct {
	ID                int64
	Name              string
	EndpointRegex     string
	compiledRegex     *regexp.Regexp
	HTTPMethods       []string // nil or empty = all methods match
	AllowedCIDRs      []*net.IPNet
	DeniedCIDRs       []*net.IPNet
	AppliesToAPIToken bool
	AppliesToSession  bool
	Priority          int
	Active            bool
}

// RuleCache holds active IP rules in memory.
// Rules are eagerly loaded at construction time and refreshed from the DB every TTL seconds.
//
// Concurrency model:
//   - A sync.RWMutex protects the rule slice: many readers, one writer.
//   - A non-blocking refreshInProgress flag prevents concurrent refreshes.
//     The first goroutine that detects a stale cache runs the refresh;
//     all others continue with the (temporarily stale) cached rules.
//     The next request after the refresh completes will see the fresh rules.
type RuleCache struct {
	mu                sync.RWMutex
	rules             []ipRule // sorted by priority ASC (lower = higher priority)
	lastLoaded        time.Time
	ttl               time.Duration
	db                *sql.DB

	// refreshMu + refreshInProgress implement a non-blocking "call once" guard.
	// Replaces golang.org/x/sync/singleflight (not vendored) with stdlib-only code.
	refreshMu         sync.Mutex
	refreshInProgress bool
}

// NewRuleCache creates and initialises a RuleCache.
// Rules are loaded immediately from the database — there is no lazy-load window,
// so the service starts with correct rules from the very first request.
func NewRuleCache(db *sql.DB, ttl time.Duration) *RuleCache {
	rc := &RuleCache{
		db:  db,
		ttl: ttl,
	}
	rules, err := fetchRulesFromDB(db)
	if err != nil {
		log.Errorf("iprule: initial load from DB failed: %v — service starting with empty rule set", err)
	}
	rc.rules = rules
	rc.lastLoaded = time.Now()
	return rc
}

// Check evaluates the active rules against the request attributes.
// Returns (allow bool, matchedRuleName string).
//
// Evaluation order per rule (priority ASC, lower number = first evaluated):
//  1. Skip if auth-type doesn't match (api token vs session).
//  2. Skip if endpoint_regex doesn't match the path.
//  3. Skip if http_methods is set and method isn't in the list.
//  4. Evaluate denied_cidrs first — if matched: DENY.
//  5. If allowed_cidrs is empty: ALLOW (no restriction defined).
//  6. If allowed_cidrs is non-empty: ALLOW only if IP is in list, else DENY.
//
// If no rule matches: returns (true, "") — fail-open for unconfigured endpoints.
func (rc *RuleCache) Check(path, method, clientIP string, isAPITokenAuth bool) (bool, string) {
	rules := rc.getOrRefreshRules()

	parsedIP := net.ParseIP(clientIP)
	if parsedIP == nil {
		// Unparseable IP → fail-closed: deny with a synthetic rule name for traceability.
		log.Warnf("iprule: could not parse client IP %q — denying (fail-closed)", clientIP)
		return false, "unparseable-client-ip"
	}

	for _, rule := range rules {
		if !rule.Active {
			continue
		}
		// Step 1: Auth-type filter.
		if isAPITokenAuth && !rule.AppliesToAPIToken {
			continue
		}
		if !isAPITokenAuth && !rule.AppliesToSession {
			continue
		}
		// Step 2: Endpoint regex filter.
		if rule.compiledRegex != nil && !rule.compiledRegex.MatchString(path) {
			continue
		}
		// Step 3: HTTP method filter.
		if len(rule.HTTPMethods) > 0 && !containsMethod(rule.HTTPMethods, method) {
			continue
		}

		// Rule matched. Evaluate CIDRs.

		// Step 4: denied_cidrs first — explicit deny wins over allow.
		for _, denied := range rule.DeniedCIDRs {
			if denied.Contains(parsedIP) {
				return false, rule.Name
			}
		}
		// Step 5: If no allowed_cidrs defined → unrestricted allow.
		if len(rule.AllowedCIDRs) == 0 {
			return true, rule.Name
		}
		// Step 6: Check allowed_cidrs.
		for _, allowed := range rule.AllowedCIDRs {
			if allowed.Contains(parsedIP) {
				return true, rule.Name
			}
		}
		// IP is not in any allowed CIDR → deny.
		return false, rule.Name
	}

	// No rule matched → fail-open.
	return true, ""
}

// getOrRefreshRules returns the current cached rules and triggers a background
// refresh if the TTL has expired.
//
// Non-blocking design: the first goroutine to see a stale cache starts the refresh;
// all others immediately get the (temporarily stale) current rules.
// This avoids request latency spikes caused by a blocking DB query on every goroutine
// when the cache expires at a busy moment.
func (rc *RuleCache) getOrRefreshRules() []ipRule {
	rc.mu.RLock()
	rules := rc.rules
	needRefresh := time.Since(rc.lastLoaded) > rc.ttl
	rc.mu.RUnlock()

	if !needRefresh {
		return rules
	}

	// Non-blocking guard: only one goroutine refreshes at a time.
	rc.refreshMu.Lock()
	if rc.refreshInProgress {
		// Another goroutine is already refreshing — return current (stale) rules.
		rc.refreshMu.Unlock()
		return rules
	}
	rc.refreshInProgress = true
	rc.refreshMu.Unlock()

	// Run the refresh in a goroutine so callers are never blocked.
	go func() {
		defer func() {
			rc.refreshMu.Lock()
			rc.refreshInProgress = false
			rc.refreshMu.Unlock()
		}()

		newRules, err := fetchRulesFromDB(rc.db)
		if err != nil {
			// Keep stale rules on DB failure — do not crash, do not fail-open.
			log.Errorf("iprule: DB refresh failed: %v — keeping stale rules", err)
			return
		}
		rc.mu.Lock()
		rc.rules = newRules
		rc.lastLoaded = time.Now()
		rc.mu.Unlock()
		log.Debugf("iprule: rule cache refreshed (%d rules loaded)", len(newRules))
	}()

	// Return the (still valid) stale rules to the current request — no blocking.
	return rules
}

// fetchRulesFromDB queries all active IP rules from the database,
// sorted by priority ASC (lower number = evaluated first = higher priority).
// Invalid regex patterns are logged and skipped — they do not cause a panic.
func fetchRulesFromDB(db *sql.DB) ([]ipRule, error) {
	const qry = `
SELECT
    id, name, endpoint_regex,
    COALESCE(http_methods, ARRAY[]::TEXT[]),
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]),
    COALESCE(denied_cidrs, ARRAY[]::TEXT[]),
    applies_to_api_token, applies_to_session,
    priority, active
FROM api_ip_rule
WHERE active = TRUE
ORDER BY priority ASC
`
	rows, err := db.Query(qry)
	if err != nil {
		return nil, fmt.Errorf("querying api_ip_rule: %w", err)
	}
	defer rows.Close()

	var rules []ipRule
	for rows.Next() {
		var r ipRule
		var rawHTTPMethods pgTextArray
		var rawAllowedCIDRs pgTextArray
		var rawDeniedCIDRs pgTextArray

		if err := rows.Scan(
			&r.ID, &r.Name, &r.EndpointRegex,
			&rawHTTPMethods, &rawAllowedCIDRs, &rawDeniedCIDRs,
			&r.AppliesToAPIToken, &r.AppliesToSession,
			&r.Priority, &r.Active,
		); err != nil {
			log.Errorf("iprule: scanning row: %v — skipping", err)
			continue
		}

		// Compile regex — skip rule if invalid (log, no crash).
		compiled, err := regexp.Compile(r.EndpointRegex)
		if err != nil {
			log.Errorf("iprule: invalid endpoint_regex %q for rule %q: %v — skipping rule", r.EndpointRegex, r.Name, err)
			continue
		}
		r.compiledRegex = compiled
		r.HTTPMethods = []string(rawHTTPMethods)
		r.AllowedCIDRs = parseCIDRList(r.Name, "allowed_cidrs", []string(rawAllowedCIDRs))
		r.DeniedCIDRs = parseCIDRList(r.Name, "denied_cidrs", []string(rawDeniedCIDRs))

		rules = append(rules, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating api_ip_rule rows: %w", err)
	}
	return rules, nil
}

// parseCIDRList parses a slice of CIDR strings into net.IPNet values.
// Invalid entries are logged and skipped — they do not cause a panic or stop parsing.
func parseCIDRList(ruleName, field string, cidrs []string) []*net.IPNet {
	parsed := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if cidr == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			log.Errorf("iprule: rule %q field %s: invalid CIDR %q: %v — skipping", ruleName, field, cidr, err)
			continue
		}
		parsed = append(parsed, ipNet)
	}
	return parsed
}

// containsMethod returns true if method is in the list (case-insensitive).
func containsMethod(methods []string, method string) bool {
	upper := strings.ToUpper(method)
	for _, m := range methods {
		if strings.ToUpper(m) == upper {
			return true
		}
	}
	return false
}

// GetPathWithoutVersion strips the /api/X.Y/ prefix from a URL path.
// Normalises the path for regex matching against endpoint_regex values stored in DB.
// Examples:
//
//	/api/5.0/user/api_tokens  → user/api_tokens
//	/api/3.1/deliveryservices → deliveryservices
//	/health                   → /health (passthrough, no version prefix)
func GetPathWithoutVersion(urlPath string) string {
	const apiPrefix = "/api/"
	if !strings.HasPrefix(urlPath, apiPrefix) {
		return urlPath
	}
	rest := urlPath[len(apiPrefix):]
	idx := strings.Index(rest, "/")
	if idx < 0 {
		return urlPath
	}
	return rest[idx+1:]
}

// ExtractClientIP returns the client's real IP address.
// If the request's RemoteAddr comes from a trusted proxy, the first address
// in X-Forwarded-For is used instead to identify the real client.
// Prevents IP spoofing: X-Forwarded-For is only trusted from known proxies.
func ExtractClientIP(r *http.Request, trustedProxyCIDRs []*net.IPNet) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	if isIPInCIDRList(remoteHost, trustedProxyCIDRs) {
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

// IsIPInCIDRList returns true if ipStr is contained within any of the CIDR strings.
// Exported for use in token handler (per-token allowed_cidrs validation).
func IsIPInCIDRList(ipStr string, cidrs []string) bool {
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

// isIPInCIDRList is the internal variant using pre-parsed *net.IPNet values.
func isIPInCIDRList(ipStr string, nets []*net.IPNet) bool {
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

// NewIPRuleMiddlewareWithKey returns an HTTP middleware that enforces IP rules.
//
// The apiTokenAuthKey parameter is the context key used by the api package to
// signal API token authentication. It is injected here to avoid importing the
// api package (which would create a circular dependency: api→iprule→api).
//
// Decision flow per request:
//  1. If cache is nil → pass through (feature disabled or test environment).
//  2. Extract real client IP (X-Forwarded-For respected for trusted proxies).
//  3. Strip /api/X.Y/ version prefix from URL path.
//  4. Detect auth type via context key presence.
//  5. Evaluate RuleCache.Check() — first-match-wins, priority ASC.
//  6. If denied → 403 Forbidden with structured JSON body including rule name.
//  7. If allowed → call next handler.
func NewIPRuleMiddlewareWithKey(
	cache *RuleCache,
	trustedProxyCIDRs []*net.IPNet,
	apiTokenAuthKey interface{},
) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if cache == nil {
				next(w, r)
				return
			}

			path := GetPathWithoutVersion(r.URL.Path)
			method := r.Method
			clientIP := ExtractClientIP(r, trustedProxyCIDRs)
			isAPITokenAuth := r.Context().Value(apiTokenAuthKey) != nil

			allowed, ruleName := cache.Check(path, method, clientIP, isAPITokenAuth)
			if !allowed {
				log.Infof("IP_RULE_BLOCK ip=%s path=%s method=%s rule=%s", clientIP, path, method, ruleName)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusForbidden)
				fmt.Fprintf(w, `{"alerts":[{"level":"error","text":"access denied from this IP address (rule: %s)"}]}`, ruleName)
				return
			}

			next(w, r)
		}
	}
}

// DBFromSqlx extracts the underlying *sql.DB from a *sqlx.DB.
// Used in NewRuleCache calls from main() which has a *sqlx.DB.
func DBFromSqlx(db *sqlx.DB) *sql.DB {
	return db.DB
}

// ─────────────────────────────────────────────────────────────────────────────
// PostgreSQL TEXT[] scanner (stdlib only — avoids importing github.com/lib/pq
// in this package, which is not needed beyond scanning)
// ─────────────────────────────────────────────────────────────────────────────

// pgTextArray implements sql.Scanner for PostgreSQL TEXT[] wire format.
// It parses the literal {a,b,"c d"} syntax returned by the PostgreSQL driver.
type pgTextArray []string

func (a *pgTextArray) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	var raw string
	switch v := src.(type) {
	case []byte:
		raw = string(v)
	case string:
		raw = v
	default:
		return fmt.Errorf("iprule: pgTextArray: unsupported type %T", src)
	}
	return parsePGArray(raw, a)
}

// parsePGArray decodes a PostgreSQL array literal like {a,"b c",d} into a []string.
func parsePGArray(s string, out *pgTextArray) error {
	s = strings.TrimSpace(s)
	if s == "{}" || s == "" {
		return nil
	}
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return fmt.Errorf("iprule: invalid pg array: %q", s)
	}
	inner := s[1 : len(s)-1]
	if inner == "" {
		return nil
	}
	// Simple split on comma — handles unquoted entries (sufficient for CIDRs, methods, regex).
	// Does not handle escaped commas inside quoted strings; not needed for our data.
	parts := strings.Split(inner, ",")
	for _, p := range parts {
		*out = append(*out, strings.Trim(strings.TrimSpace(p), `"`))
	}
	return nil
}
