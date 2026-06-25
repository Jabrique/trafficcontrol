package generate

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
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-atscfg"
	"github.com/apache/trafficcontrol/v8/lib/go-util"
)

// makeTestDS is a test helper that builds a minimal atscfg.DeliveryService.
func makeTestDS(tenant, xmlID, profileName string) atscfg.DeliveryService {
	ds := atscfg.DeliveryService{}
	ds.XMLID = xmlID
	if tenant != "" {
		ds.Tenant = util.Ptr(tenant)
	}
	if profileName != "" {
		ds.ProfileName = util.Ptr(profileName)
	}
	return ds
}

// makeTestParam is a test helper that builds a ParameterV5 for testing.
func makeTestParam(name, configFile, value string, profiles []string) atscfgParam {
	profilesJSON, _ := json.Marshal(profiles)
	return atscfgParam{
		Name:       name,
		ConfigFile: configFile,
		Value:      value,
		Profiles:   json.RawMessage(profilesJSON),
	}
}

// Test 4: Normal case -- single DS with matching profile and one sink.
func TestBuildTenantConfigs_NormalCase(t *testing.T) {
	ds := makeTestDS("wowrack", "ds-video", "profile-wowrack")
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"test","region":"us-east-1"}`, []string{"profile-wowrack"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Tenant != "wowrack" {
		t.Errorf("expected tenant 'wowrack', got %q", configs[0].Tenant)
	}
	if configs[0].XMLID != "ds-video" {
		t.Errorf("expected xmlID 'ds-video', got %q", configs[0].XMLID)
	}
	if len(configs[0].Sinks) != 1 {
		t.Errorf("expected 1 sink, got %d", len(configs[0].Sinks))
	}
	if configs[0].Sinks[0].SinkType != "aws_s3" {
		t.Errorf("expected sink type 'aws_s3', got %q", configs[0].Sinks[0].SinkType)
	}
}

// Test 5: DS with nil Tenant must be silently skipped.
func TestBuildTenantConfigs_NilTenant(t *testing.T) {
	ds := makeTestDS("", "ds-orphan", "profile-a")
	ds.Tenant = nil // explicitly nil
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"test"}`, []string{"profile-a"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for nil tenant, got %d", len(configs))
	}
}

// Test 6: DS whose profile has no matching parameters must be skipped (not an error).
func TestBuildTenantConfigs_NoMatchingProfile(t *testing.T) {
	ds := makeTestDS("wowrack", "ds-no-logs", "profile-a")
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"test"}`, []string{"profile-b"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// DS has no matching parameter: no log sink configured. Normal case.
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for non-matching profile, got %d", len(configs))
	}
}

// Test 7: A Parameter.Value that is not valid JSON must return an error immediately.
func TestBuildTenantConfigs_InvalidJSON(t *testing.T) {
	ds := makeTestDS("wowrack", "ds-bad", "profile-a")
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{not: valid json}`, []string{"profile-a"}),
	}

	_, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err == nil {
		t.Error("expected error for invalid JSON in Parameter.Value, got nil")
	}
}

// Test 8: generateVectorConfig must produce valid YAML with the correct top-level
// transforms/sinks keys and the correct component IDs.
func TestGenerateVectorConfig_OutputFormat(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "wowrack",
		XMLID:  "ds-video",
		Tier:   "premium", // use premium so output is simpler (no tier_remap transform)
		Sinks: []SinkEntry{
			{
				SinkType:   "aws_s3",
				SinkParams: map[string]interface{}{"bucket": "test-bucket", "region": "us-east-1"},
			},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	// The filter transform ID must be present.
	if !strings.Contains(yamlStr, "filter_wowrack__ds-video") {
		t.Errorf("missing filter transform ID in output:\n%s", yamlStr)
	}
	// Sink component ID must use ls_ prefix (customer-facing billing convention).
	if !strings.Contains(yamlStr, "ls_wowrack__ds-video") {
		t.Errorf("missing ls_ sink component ID in output:\n%s", yamlStr)
	}
	// The upstream transform reference must be present.
	if !strings.Contains(yamlStr, "extract_billing") {
		t.Errorf("missing upstream transform reference in output:\n%s", yamlStr)
	}
	// The VRL condition must be present.
	if !strings.Contains(yamlStr, "wowrack") || !strings.Contains(yamlStr, "ds-video") {
		t.Errorf("missing tenant/DS name in VRL condition:\n%s", yamlStr)
	}
	// Must have top-level transforms and sinks keys (Vector format).
	if !strings.Contains(yamlStr, "transforms:") {
		t.Errorf("missing top-level transforms: key:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "sinks:") {
		t.Errorf("missing top-level sinks: key:\n%s", yamlStr)
	}
}

// Bonus: DS with two matching parameters on the same profile must produce one
// TenantDSConfig with two SinkEntry values.
func TestBuildTenantConfigs_MultipleSinks(t *testing.T) {
	ds := makeTestDS("acme", "ds-api", "profile-acme")
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"acme-logs"}`, []string{"profile-acme"}),
		makeTestParam("splunk_hec", "vector-tenant.yaml", `{"endpoint":"https://splunk.acme.com","token":"xxx"}`, []string{"profile-acme"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config (one per DS, not per sink), got %d", len(configs))
	}
	if len(configs[0].Sinks) != 2 {
		t.Errorf("expected 2 sinks for DS with 2 parameters, got %d", len(configs[0].Sinks))
	}
}

// Test: DS with log_streaming_tier = "standard" must have Tier set to "standard".
func TestBuildTenantConfigs_StandardTier(t *testing.T) {
	ds := makeTestDS("wowrack", "ds-cdn", "profile-wowrack")
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"wowrack-logs"}`, []string{"profile-wowrack"}),
		makeTestParam("log_streaming_tier", "vector-tenant.yaml", "standard", []string{"profile-wowrack"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Tier != "standard" {
		t.Errorf("expected Tier 'standard', got %q", configs[0].Tier)
	}
	// log_streaming_tier must NOT appear as a sink entry.
	for _, sink := range configs[0].Sinks {
		if sink.SinkType == "log_streaming_tier" {
			t.Error("log_streaming_tier must not be treated as a sink type")
		}
	}
}

// Test: DS with log_streaming_tier = "premium" must have Tier set to "premium".
func TestBuildTenantConfigs_PremiumTier(t *testing.T) {
	ds := makeTestDS("cloudraya", "ds-enterprise", "profile-cloudraya")
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"cloudraya-logs"}`, []string{"profile-cloudraya"}),
		makeTestParam("log_streaming_tier", "vector-tenant.yaml", "premium", []string{"profile-cloudraya"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Tier != "premium" {
		t.Errorf("expected Tier 'premium', got %q", configs[0].Tier)
	}
}

// Test: DS with no log_streaming_tier param must default to "standard".
func TestBuildTenantConfigs_DefaultTierIsStandard(t *testing.T) {
	ds := makeTestDS("wowrack", "ds-basic", "profile-wowrack")
	params := []atscfgParam{
		// No log_streaming_tier param: should default to "standard".
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"wowrack-logs"}`, []string{"profile-wowrack"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Tier != "standard" {
		t.Errorf("expected default Tier 'standard', got %q", configs[0].Tier)
	}
}

// Test: generateVectorConfig with standard tier must include a remap transform
// that deletes premium GeoIP and anonymous-IP fields.
func TestGenerateVectorConfig_StandardTierDropsPremiumFields(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "wowrack",
		XMLID:  "ds-video",
		Tier:   "standard",
		Sinks: []SinkEntry{
			{SinkType: "aws_s3", SinkParams: map[string]interface{}{"bucket": "test"}},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	// Standard tier must inject a remap transform that deletes premium fields.
	for _, field := range []string{"client_country", "client_city", "client_latitude", "client_longitude", "is_vpn", "is_tor"} {
		if !strings.Contains(yamlStr, field) {
			t.Errorf("expected standard tier to reference field %q in del() call, not found in:\n%s", field, yamlStr)
		}
	}
}

// Test: generateVectorConfig with premium tier must NOT inject the remap transform
// that deletes premium fields.
func TestGenerateVectorConfig_PremiumTierKeepsAllFields(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "cloudraya",
		XMLID:  "ds-enterprise",
		Tier:   "premium",
		Sinks: []SinkEntry{
			{SinkType: "aws_s3", SinkParams: map[string]interface{}{"bucket": "test"}},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	// Premium tier must NOT have a del(client_country) or similar VRL deletion.
	// The remap transform injected by standard tier should be absent.
	if strings.Contains(yamlStr, "del(.client_country)") {
		t.Errorf("premium tier must not delete client_country, but found del() in:\n%s", yamlStr)
	}
	if strings.Contains(yamlStr, "del(.is_vpn)") {
		t.Errorf("premium tier must not delete is_vpn, but found del() in:\n%s", yamlStr)
	}
}

// Test: sink component ID must use "ls_" prefix for customer-facing billing.
func TestGenerateVectorConfig_SinkIDHasLsPrefix(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "wowrack",
		XMLID:  "ds-video",
		Tier:   "standard",
		Sinks: []SinkEntry{
			{SinkType: "aws_s3", SinkParams: map[string]interface{}{"bucket": "test"}},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	// Sink component ID must start with "ls_" for capture_delivery_billing to detect it.
	if !strings.Contains(yamlStr, "ls_wowrack__ds-video") {
		t.Errorf("expected sink ID with ls_ prefix 'ls_wowrack__ds-video', not found in:\n%s", yamlStr)
	}
	// Old-style sink ID (without ls_ prefix) must NOT appear.
	if strings.Contains(yamlStr, "aws_s3_wowrack__ds-video") {
		t.Errorf("old-style sink ID without ls_ prefix must not appear in:\n%s", yamlStr)
	}
}

// --- Gap coverage tests added below ---

// Test: DS with nil ProfileName must be silently skipped.
func TestBuildTenantConfigs_NilProfileName(t *testing.T) {
	ds := makeTestDS("wowrack", "ds-no-profile", "")
	ds.ProfileName = nil // explicitly nil
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"test"}`, []string{"profile-a"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for nil ProfileName DS, got %d", len(configs))
	}
}

// Test: empty tenant string must be treated the same as nil tenant (skipped).
func TestBuildTenantConfigs_EmptyTenantString(t *testing.T) {
	ds := makeTestDS("", "ds-no-tenant", "profile-a")
	ds.Tenant = util.Ptr("") // explicitly empty string (not nil)
	params := []atscfgParam{
		makeTestParam("aws_s3", "vector-tenant.yaml", `{"bucket":"test"}`, []string{"profile-a"}),
	}

	configs, err := buildTenantConfigs([]atscfg.DeliveryService{ds}, params)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 0 {
		t.Errorf("expected 0 configs for empty tenant string, got %d", len(configs))
	}
}

// Test: standard tier must inject a tier_remap transform whose ID follows
// the expected naming convention (tier_remap_{tenant}__{xmlid}).
func TestGenerateVectorConfig_StandardTierInjectsRemapTransform(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "wowrack",
		XMLID:  "ds-video",
		Tier:   "standard",
		Sinks: []SinkEntry{
			{SinkType: "aws_s3", SinkParams: map[string]interface{}{"bucket": "test"}},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	// The tier remap transform ID must appear in the YAML.
	if !strings.Contains(yamlStr, "tier_remap_wowrack__ds-video") {
		t.Errorf("expected tier_remap transform ID 'tier_remap_wowrack__ds-video' in output:\n%s", yamlStr)
	}
	// The sink must reference the tier_remap transform as its input, not the filter directly.
	if !strings.Contains(yamlStr, "tier_remap_wowrack__ds-video") {
		t.Errorf("sink inputs must reference tier_remap transform in standard tier:\n%s", yamlStr)
	}
}

// Test: premium tier must NOT inject a tier_remap transform.
func TestGenerateVectorConfig_PremiumTierNoRemapTransform(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "cloudraya",
		XMLID:  "ds-enterprise",
		Tier:   "premium",
		Sinks: []SinkEntry{
			{SinkType: "aws_s3", SinkParams: map[string]interface{}{"bucket": "test"}},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	if strings.Contains(yamlStr, "tier_remap_") {
		t.Errorf("premium tier must not inject a tier_remap transform, but found one in:\n%s", yamlStr)
	}
}

// Test: zero-value Tier ("") must behave identically to "standard"
// (tier_remap injected, premium fields deleted).
func TestGenerateVectorConfig_ZeroValueTierIsStandard(t *testing.T) {
	tdc := TenantDSConfig{
		Tenant: "wowrack",
		XMLID:  "ds-basic",
		Tier:   "", // zero value
		Sinks: []SinkEntry{
			{SinkType: "aws_s3", SinkParams: map[string]interface{}{"bucket": "test"}},
		},
	}

	content, err := generateVectorConfig(tdc, "extract_billing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	yamlStr := string(content)

	// Zero value must inject tier_remap (same as standard).
	if !strings.Contains(yamlStr, "tier_remap_") {
		t.Errorf("zero-value Tier must behave as 'standard' and inject tier_remap, not found in:\n%s", yamlStr)
	}
	if !strings.Contains(yamlStr, "del(.client_country)") {
		t.Errorf("zero-value Tier must delete premium fields, del(.client_country) not found in:\n%s", yamlStr)
	}
}

// Test: standardTierVRL must reference all 12 premium fields that must be deleted.
func TestStandardTierVRL_ContainsAllPremiumFields(t *testing.T) {
	vrl := standardTierVRL()
	requiredFields := []string{
		"client_country",
		"client_registered_country",
		"client_continent",
		"client_city",
		"client_timezone",
		"client_latitude",
		"client_longitude",
		"is_vpn",
		"is_hosting",
		"is_proxy",
		"is_tor",
		"is_relay",
	}
	for _, field := range requiredFields {
		if !strings.Contains(vrl, field) {
			t.Errorf("standardTierVRL() missing del() for field %q", field)
		}
	}
	// Must also normalize cache_result.
	if !strings.Contains(vrl, "cache_result") {
		t.Error("standardTierVRL() must normalize cache_result field")
	}
}

// Test: syncDatabases must be a no-op when params slice is empty.
func TestSyncDatabases_EmptyParams(t *testing.T) {
	// Should return nil without creating any directories or files.
	if err := syncDatabases(nil, "/tmp/nonexistent-should-not-be-created"); err != nil {
		t.Errorf("expected nil error for empty params, got: %v", err)
	}
	// Directory must NOT have been created.
	if _, err := os.Stat("/tmp/nonexistent-should-not-be-created"); !os.IsNotExist(err) {
		t.Error("syncDatabases with empty params must not create the database directory")
		os.RemoveAll("/tmp/nonexistent-should-not-be-created")
	}
}

// Test: syncDatabases must download a missing file and write it to disk.
func TestSyncDatabases_DownloadsMissingFile(t *testing.T) {
	const fakeContent = "fake-mmdb-content"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fakeContent))
	}))
	defer server.Close()

	dir := t.TempDir()
	params := []atscfgParam{
		makeTestParam("geoip_city", "vector_database", server.URL, nil),
	}

	if err := syncDatabases(params, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destPath := filepath.Join(dir, "geoip_city.mmdb")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("expected file %s to exist after download: %v", destPath, err)
	}
	if string(data) != fakeContent {
		t.Errorf("expected file content %q, got %q", fakeContent, string(data))
	}
}

// Test: syncDatabases must skip download when the local file is fresh (<7 days old).
func TestSyncDatabases_SkipsFreshFile(t *testing.T) {
	downloadCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloadCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("updated-content"))
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "geoip_city.mmdb")

	// Write a "fresh" file (mtime = now).
	if err := os.WriteFile(destPath, []byte("original-content"), 0644); err != nil {
		t.Fatalf("setup: writing fresh file: %v", err)
	}
	now := time.Now()
	if err := os.Chtimes(destPath, now, now); err != nil {
		t.Fatalf("setup: setting mtime: %v", err)
	}

	params := []atscfgParam{
		makeTestParam("geoip_city", "vector_database", server.URL, nil),
	}

	if err := syncDatabases(params, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Server must NOT have been contacted.
	if downloadCount != 0 {
		t.Errorf("expected 0 downloads for fresh file, got %d", downloadCount)
	}
	// File content must be unchanged.
	data, _ := os.ReadFile(destPath)
	if string(data) != "original-content" {
		t.Errorf("fresh file must not be overwritten, content changed to %q", string(data))
	}
}

// Test: syncDatabases must contact the server for a stale file (>7 days old)
// and update the local copy when server returns 200.
func TestSyncDatabases_DownloadsStaleFile(t *testing.T) {
	const updatedContent = "updated-mmdb-content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore If-Modified-Since and always return 200 with new content.
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(updatedContent))
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "anonymous_ip.mmdb")

	// Write a stale file (mtime = 8 days ago).
	if err := os.WriteFile(destPath, []byte("old-content"), 0644); err != nil {
		t.Fatalf("setup: writing stale file: %v", err)
	}
	staleTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(destPath, staleTime, staleTime); err != nil {
		t.Fatalf("setup: setting mtime: %v", err)
	}

	params := []atscfgParam{
		makeTestParam("anonymous_ip", "vector_database", server.URL, nil),
	}

	if err := syncDatabases(params, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File must have been updated.
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("file must exist after stale download: %v", err)
	}
	if string(data) != updatedContent {
		t.Errorf("expected updated content %q, got %q", updatedContent, string(data))
	}
}

// Test: syncDatabases must handle a server 304 Not Modified by touching the
// local file's mtime without rewriting the file content.
func TestSyncDatabases_NotModifiedTouchesMtime(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always respond 304 (simulate server saying "not changed").
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	dir := t.TempDir()
	destPath := filepath.Join(dir, "geoip_city.mmdb")

	const originalContent = "original-mmdb"
	if err := os.WriteFile(destPath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("setup: writing file: %v", err)
	}
	// Set mtime to 8 days ago so the freshness check triggers a request.
	staleTime := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(destPath, staleTime, staleTime); err != nil {
		t.Fatalf("setup: setting mtime: %v", err)
	}

	params := []atscfgParam{
		makeTestParam("geoip_city", "vector_database", server.URL, nil),
	}

	if err := syncDatabases(params, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// File content must be unchanged.
	data, _ := os.ReadFile(destPath)
	if string(data) != originalContent {
		t.Errorf("304 response must not overwrite file, content changed to %q", string(data))
	}
	// Mtime must have been touched to now (within a few seconds).
	fi, _ := os.Stat(destPath)
	age := time.Since(fi.ModTime())
	if age > 5*time.Second {
		t.Errorf("304 response must touch mtime, but mtime is still %v old", age.Round(time.Second))
	}
}

// Test: syncDatabases must log-and-continue when one URL fails; other files
// in the same batch must still be downloaded.
func TestSyncDatabases_ContinuesOnOneFailure(t *testing.T) {
	const goodContent = "good-mmdb"
	goodServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(goodContent))
	}))
	defer goodServer.Close()

	dir := t.TempDir()
	params := []atscfgParam{
		// First param: bad URL -- should fail gracefully.
		makeTestParam("bad_db", "vector_database", "http://127.0.0.1:0/nonexistent", nil),
		// Second param: good URL -- must still be downloaded.
		makeTestParam("good_db", "vector_database", goodServer.URL, nil),
	}

	// Must not return an error (failures are logged, not propagated).
	if err := syncDatabases(params, dir); err != nil {
		t.Fatalf("syncDatabases must not return error on partial failure, got: %v", err)
	}

	// good_db.mmdb must have been created.
	goodPath := filepath.Join(dir, "good_db.mmdb")
	data, err := os.ReadFile(goodPath)
	if err != nil {
		t.Fatalf("good_db.mmdb must exist despite bad_db failure: %v", err)
	}
	if string(data) != goodContent {
		t.Errorf("expected %q, got %q", goodContent, string(data))
	}

	// bad_db.mmdb must NOT exist.
	badPath := filepath.Join(dir, "bad_db.mmdb")
	if _, err := os.Stat(badPath); !os.IsNotExist(err) {
		t.Error("bad_db.mmdb must not exist after failed download")
	}
}

// Test: syncDatabases must transparently decompress a .mmdb.gz response and
// write the raw .mmdb content to disk. The destination file is always .mmdb.
func TestSyncDatabases_DecompressesGzFile(t *testing.T) {
	const rawContent = "fake-uncompressed-mmdb-content"

	// Serve a gzip-compressed body.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)

		gw := gzip.NewWriter(w)
		defer gw.Close()
		gw.Write([]byte(rawContent))
	}))
	defer server.Close()

	dir := t.TempDir()
	// URL ends with .gz to trigger decompression.
	gzURL := server.URL + "/GeoIP2-City.mmdb.gz"

	params := []atscfgParam{
		makeTestParam("geoip_city", "vector_database", gzURL, nil),
	}

	if err := syncDatabases(params, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	destPath := filepath.Join(dir, "geoip_city.mmdb")
	data, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("expected geoip_city.mmdb to exist after decompressed download: %v", err)
	}
	// Must contain the raw (decompressed) content, not the gzip bytes.
	if string(data) != rawContent {
		t.Errorf("expected decompressed content %q, got %q", rawContent, string(data))
	}
}

// Test: syncDatabases must return an error for a .gz URL whose body is not
// valid gzip data (corrupt download). The .mmdb file must not be written.
func TestSyncDatabases_CorruptGzReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Serve invalid gzip bytes.
		w.Write([]byte("this is NOT gzip data"))
	}))
	defer server.Close()

	dir := t.TempDir()
	gzURL := server.URL + "/corrupt.mmdb.gz"

	params := []atscfgParam{
		makeTestParam("corrupt_db", "vector_database", gzURL, nil),
	}

	// syncDatabases logs-and-continues; it does NOT return an error upward.
	// But the .mmdb file must NOT have been created.
	_ = syncDatabases(params, dir)

	destPath := filepath.Join(dir, "corrupt_db.mmdb")
	if _, err := os.Stat(destPath); !os.IsNotExist(err) {
		t.Error("corrupt_db.mmdb must not exist after corrupt gzip download")
		os.Remove(destPath)
	}
}

