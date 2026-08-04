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
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// pgTextArray scanner tests
// ─────────────────────────────────────────────────────────────────────────────

func TestPgTextArray_Empty(t *testing.T) {
	var a pgTextArray
	if err := a.Scan("{}"); err != nil {
		t.Fatalf("expected no error on empty array, got: %v", err)
	}
	if len(a) != 0 {
		t.Errorf("expected empty slice, got: %v", a)
	}
}

func TestPgTextArray_SingleElement(t *testing.T) {
	var a pgTextArray
	if err := a.Scan(`{GET}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a) != 1 || a[0] != "GET" {
		t.Errorf("expected [GET], got: %v", a)
	}
}

func TestPgTextArray_MultipleElements(t *testing.T) {
	var a pgTextArray
	if err := a.Scan(`{192.168.1.0/24,10.0.0.0/8}`); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(a))
	}
	if a[0] != "192.168.1.0/24" {
		t.Errorf("element 0: expected 192.168.1.0/24, got %s", a[0])
	}
	if a[1] != "10.0.0.0/8" {
		t.Errorf("element 1: expected 10.0.0.0/8, got %s", a[1])
	}
}

func TestPgTextArray_NilInput(t *testing.T) {
	var a pgTextArray
	if err := a.Scan(nil); err != nil {
		t.Fatalf("expected no error on nil input, got: %v", err)
	}
	if len(a) != 0 {
		t.Errorf("expected empty slice on nil, got: %v", a)
	}
}

func TestPgTextArray_ByteSlice(t *testing.T) {
	var a pgTextArray
	if err := a.Scan([]byte(`{GET,POST}`)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(a) != 2 || a[0] != "GET" || a[1] != "POST" {
		t.Errorf("expected [GET POST], got: %v", a)
	}
}

func TestPgTextArray_InvalidFormat(t *testing.T) {
	var a pgTextArray
	if err := a.Scan("not_an_array"); err == nil {
		t.Error("expected error for invalid format, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// parseCIDRList tests
// ─────────────────────────────────────────────────────────────────────────────

func TestParseCIDRList_ValidEntries(t *testing.T) {
	nets := parseCIDRList("test-rule", "allowed_cidrs", []string{"10.0.0.0/8", "192.168.0.0/16"})
	if len(nets) != 2 {
		t.Errorf("expected 2 parsed nets, got %d", len(nets))
	}
}

func TestParseCIDRList_InvalidEntrySkipped(t *testing.T) {
	nets := parseCIDRList("test-rule", "allowed_cidrs", []string{"10.0.0.0/8", "not-a-cidr"})
	if len(nets) != 1 {
		t.Errorf("expected 1 valid net (invalid skipped), got %d", len(nets))
	}
}

func TestParseCIDRList_EmptyStringsSkipped(t *testing.T) {
	nets := parseCIDRList("test-rule", "allowed_cidrs", []string{"", "", "10.0.0.0/8"})
	if len(nets) != 1 {
		t.Errorf("expected 1 net (empty strings skipped), got %d", len(nets))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// containsMethod tests
// ─────────────────────────────────────────────────────────────────────────────

func TestContainsMethod_MatchExact(t *testing.T) {
	if !containsMethod([]string{"GET", "POST"}, "GET") {
		t.Error("expected GET to match")
	}
}

func TestContainsMethod_CaseInsensitive(t *testing.T) {
	if !containsMethod([]string{"get", "post"}, "GET") {
		t.Error("expected case-insensitive match")
	}
}

func TestContainsMethod_NoMatch(t *testing.T) {
	if containsMethod([]string{"POST", "PUT"}, "DELETE") {
		t.Error("expected no match for DELETE")
	}
}

func TestContainsMethod_EmptyList(t *testing.T) {
	if containsMethod([]string{}, "GET") {
		t.Error("expected false for empty list")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ExtractClientIP tests
// ─────────────────────────────────────────────────────────────────────────────

func mustParseCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("invalid CIDR %q: %v", cidr, err)
	}
	return n
}

func TestExtractClientIP_DirectConnection(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	r.Header.Set("X-Forwarded-For", "9.9.9.9")

	// No trusted proxies → XFF ignored.
	ip := ExtractClientIP(r, nil)
	if ip != "1.2.3.4" {
		t.Errorf("expected direct IP 1.2.3.4, got %s", ip)
	}
}

func TestExtractClientIP_TrustedProxy(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "203.0.113.5, 10.0.0.1")

	proxyCIDR := mustParseCIDR(t, "10.0.0.0/8")
	ip := ExtractClientIP(r, []*net.IPNet{proxyCIDR})
	if ip != "203.0.113.5" {
		t.Errorf("expected XFF IP 203.0.113.5, got %s", ip)
	}
}

func TestExtractClientIP_UntrustedProxyXFFIgnored(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "5.5.5.5:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.5")

	proxyCIDR := mustParseCIDR(t, "10.0.0.0/8")
	// 5.5.5.5 is NOT in 10.0.0.0/8 → XFF ignored.
	ip := ExtractClientIP(r, []*net.IPNet{proxyCIDR})
	if ip != "5.5.5.5" {
		t.Errorf("expected direct IP 5.5.5.5, got %s", ip)
	}
}

func TestExtractClientIP_InvalidXFFIgnored(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.1:9999"
	r.Header.Set("X-Forwarded-For", "not-an-ip")

	proxyCIDR := mustParseCIDR(t, "10.0.0.0/8")
	// Invalid XFF value → fall back to RemoteAddr.
	ip := ExtractClientIP(r, []*net.IPNet{proxyCIDR})
	if ip != "10.0.0.1" {
		t.Errorf("expected fallback to direct IP 10.0.0.1, got %s", ip)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetPathWithoutVersion tests
// ─────────────────────────────────────────────────────────────────────────────

func TestGetPathWithoutVersion_StripsMajorMinor(t *testing.T) {
	got := GetPathWithoutVersion("/api/5.0/deliveryservices")
	if got != "deliveryservices" {
		t.Errorf("expected deliveryservices, got %s", got)
	}
}

func TestGetPathWithoutVersion_NoAPIPrefix(t *testing.T) {
	got := GetPathWithoutVersion("/health")
	if got != "/health" {
		t.Errorf("expected passthrough /health, got %s", got)
	}
}

func TestGetPathWithoutVersion_NestedPath(t *testing.T) {
	got := GetPathWithoutVersion("/api/3.1/user/api_tokens")
	if got != "user/api_tokens" {
		t.Errorf("expected user/api_tokens, got %s", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// RuleCache.Check tests (using in-memory rules directly)
// ─────────────────────────────────────────────────────────────────────────────

func buildRuleCache(rules []ipRule) *RuleCache {
	return &RuleCache{
		rules:      rules,
		lastLoaded: time.Now(), // prevent immediate TTL expiry in tests
		ttl:        24 * time.Hour,
		// db is nil — fetch must never be triggered in these tests
	}
}

func mustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

func mustParseCIDROnly(cidr string) *net.IPNet {
	_, n, _ := net.ParseCIDR(cidr)
	return n
}

func TestRuleCache_Check_NoRules_PassThrough(t *testing.T) {
	rc := buildRuleCache(nil)
	allowed, name := rc.Check("deliveryservices", "GET", "1.2.3.4", false)
	if !allowed {
		t.Error("expected pass-through (no rules), got denied")
	}
	if name != "" {
		t.Errorf("expected empty rule name, got %q", name)
	}
}

func TestRuleCache_Check_AllowRule_IPInList(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "allow-internal",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("192.168.0.0/16")},
		Priority:          1,
		Active:            true,
	}})
	allowed, name := rc.Check("deliveryservices", "GET", "192.168.1.5", true)
	if !allowed {
		t.Errorf("expected allowed, got denied by rule %q", name)
	}
}

func TestRuleCache_Check_AllowRule_IPNotInList_Denied(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "allow-internal",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("192.168.0.0/16")},
		Priority:          1,
		Active:            true,
	}})
	allowed, name := rc.Check("deliveryservices", "GET", "1.2.3.4", true)
	if allowed {
		t.Error("expected denied for IP not in allowed_cidrs")
	}
	if name != "allow-internal" {
		t.Errorf("expected rule name allow-internal, got %q", name)
	}
}

func TestRuleCache_Check_DenyRule_BeforeAllow(t *testing.T) {
	// deny_cidrs is evaluated before allowed_cidrs in the same rule.
	rc := buildRuleCache([]ipRule{{
		Name:              "deny-override",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("192.168.0.0/16")},
		DeniedCIDRs:       []*net.IPNet{mustParseCIDROnly("192.168.1.0/24")},
		Priority:          1,
		Active:            true,
	}})
	// 192.168.1.5 is in AllowedCIDRs (192.168.0.0/16) AND DeniedCIDRs (192.168.1.0/24).
	// DeniedCIDRs wins.
	allowed, _ := rc.Check("deliveryservices", "GET", "192.168.1.5", true)
	if allowed {
		t.Error("expected DeniedCIDRs to take priority over AllowedCIDRs")
	}
}

func TestRuleCache_Check_AuthTypeFilter_APITokenOnly(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "api-token-only",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: true,
		AppliesToSession:  false, // does NOT apply to session auth
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("10.0.0.0/8")},
		Priority:          1,
		Active:            true,
	}})
	// Session auth request → rule skipped → pass-through.
	allowed, _ := rc.Check("deliveryservices", "GET", "1.2.3.4", false)
	if !allowed {
		t.Error("expected session request to skip API-token-only rule (pass-through)")
	}
	// API token request from non-allowed IP → denied.
	allowed, _ = rc.Check("deliveryservices", "GET", "1.2.3.4", true)
	if allowed {
		t.Error("expected API token request from non-allowed IP to be denied")
	}
}

func TestRuleCache_Check_InactiveRule_Skipped(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "inactive-deny-all",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		DeniedCIDRs:       []*net.IPNet{mustParseCIDROnly("0.0.0.0/0")},
		Priority:          1,
		Active:            false, // INACTIVE
	}})
	allowed, _ := rc.Check("deliveryservices", "GET", "1.2.3.4", true)
	if !allowed {
		t.Error("expected inactive rule to be skipped (pass-through)")
	}
}

func TestRuleCache_Check_RegexFilter_NoMatch(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "token-only-path",
		compiledRegex:     mustCompile("^user/api_tokens"),
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("10.0.0.0/8")},
		Priority:          1,
		Active:            true,
	}})
	// Path doesn't match → rule skipped → pass-through.
	allowed, _ := rc.Check("deliveryservices", "GET", "1.2.3.4", true)
	if !allowed {
		t.Error("expected non-matching path to skip rule (pass-through)")
	}
}

func TestRuleCache_Check_MethodFilter_NoMatch(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "post-restricted",
		compiledRegex:     mustCompile(".*"),
		HTTPMethods:       []string{"POST"},
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("10.0.0.0/8")},
		Priority:          1,
		Active:            true,
	}})
	// GET request → rule skipped (only POST) → pass-through.
	allowed, _ := rc.Check("deliveryservices", "GET", "1.2.3.4", true)
	if !allowed {
		t.Error("expected GET to skip POST-only rule (pass-through)")
	}
}

func TestRuleCache_Check_UnparseableIP_FailClosed(t *testing.T) {
	rc := buildRuleCache([]ipRule{})
	allowed, name := rc.Check("deliveryservices", "GET", "not-an-ip", false)
	if allowed {
		t.Error("expected fail-closed for unparseable IP")
	}
	if name != "unparseable-client-ip" {
		t.Errorf("expected rule name unparseable-client-ip, got %q", name)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// IPRuleMiddleware tests
// ─────────────────────────────────────────────────────────────────────────────

func TestIPRuleMiddleware_NilCache_PassThrough(t *testing.T) {
	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }
	mw := NewIPRuleMiddlewareWithKey(nil, nil, struct{}{})

	r := httptest.NewRequest("GET", "/api/5.0/deliveryservices", nil)
	r.RemoteAddr = "1.2.3.4:5678"
	w := httptest.NewRecorder()
	mw(next)(w, r)

	if !called {
		t.Error("expected nil cache to call next handler (pass-through)")
	}
}

func TestIPRuleMiddleware_DeniedIP_Returns403(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "block-external",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: false,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("10.0.0.0/8")},
		Priority:          1,
		Active:            true,
	}})

	ctxKey := struct{}{}
	mw := NewIPRuleMiddlewareWithKey(rc, nil, ctxKey)

	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }

	r := httptest.NewRequest("GET", "/api/5.0/deliveryservices", nil)
	r.RemoteAddr = "1.2.3.4:5678" // NOT in 10.0.0.0/8
	w := httptest.NewRecorder()
	mw(next)(w, r)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
	if called {
		t.Error("expected next handler NOT to be called on denial")
	}
}

func TestIPRuleMiddleware_AllowedIP_CallsNext(t *testing.T) {
	rc := buildRuleCache([]ipRule{{
		Name:              "internal-only",
		compiledRegex:     mustCompile(".*"),
		AppliesToAPIToken: true,
		AppliesToSession:  true,
		AllowedCIDRs:      []*net.IPNet{mustParseCIDROnly("10.0.0.0/8")},
		Priority:          1,
		Active:            true,
	}})

	ctxKey := struct{}{}
	mw := NewIPRuleMiddlewareWithKey(rc, nil, ctxKey)

	called := false
	next := func(w http.ResponseWriter, r *http.Request) { called = true }

	r := httptest.NewRequest("GET", "/api/5.0/deliveryservices", nil)
	r.RemoteAddr = "10.1.2.3:5678" // IN 10.0.0.0/8
	w := httptest.NewRecorder()
	mw(next)(w, r)

	if w.Code == http.StatusForbidden {
		t.Error("expected allowed IP not to get 403")
	}
	if !called {
		t.Error("expected next handler to be called for allowed IP")
	}
}
