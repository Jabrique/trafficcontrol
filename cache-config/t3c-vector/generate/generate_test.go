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
	"encoding/json"
	"strings"
	"testing"

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
	// The sink component ID must be present.
	if !strings.Contains(yamlStr, "aws_s3_wowrack__ds-video") {
		t.Errorf("missing sink component ID in output:\n%s", yamlStr)
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
