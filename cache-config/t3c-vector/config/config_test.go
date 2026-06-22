package config

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
	"strings"
	"testing"
)

// Test 9: validateCfg must return an error containing the flag name for each
// missing required field.
func TestValidateCfg_MissingRequired(t *testing.T) {
	tests := []struct {
		name        string
		cfg         Cfg
		wantErrFrag string
	}{
		{
			name:        "missing TO URL",
			cfg:         Cfg{TOUser: "u", TOPassword: "p", CDNName: "c", OutputDir: "/x", UpstreamTransformID: "u"},
			wantErrFrag: "traffic-ops-url",
		},
		{
			name:        "missing TO user",
			cfg:         Cfg{TOUrl: "https://to", TOPassword: "p", CDNName: "c", OutputDir: "/x", UpstreamTransformID: "u"},
			wantErrFrag: "traffic-ops-user",
		},
		{
			name:        "missing TO password",
			cfg:         Cfg{TOUrl: "https://to", TOUser: "u", CDNName: "c", OutputDir: "/x", UpstreamTransformID: "u"},
			wantErrFrag: "traffic-ops-password",
		},
		{
			name:        "missing CDN name",
			cfg:         Cfg{TOUrl: "https://to", TOUser: "u", TOPassword: "p", OutputDir: "/x", UpstreamTransformID: "u"},
			wantErrFrag: "cdn-name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCfg(tc.cfg)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErrFrag) {
				t.Errorf("expected error containing %q, got: %s", tc.wantErrFrag, err.Error())
			}
		})
	}
}

// Test 10: A fully populated Cfg must pass validation without error.
func TestValidateCfg_ValidConfig(t *testing.T) {
	cfg := Cfg{
		TOUrl:               "https://to.example.com",
		TOUser:              "admin",
		TOPassword:          "secret",
		CDNName:             "myCDN",
		OutputDir:           DefaultOutputDir,
		ConfigFileKey:       DefaultConfigFileKey,
		UpstreamTransformID: DefaultUpstreamTransformID,
	}
	if err := validateCfg(cfg); err != nil {
		t.Errorf("expected no error for valid config, got: %s", err.Error())
	}
}

// Test (additional): Package-level constants must match documented defaults.
func TestValidateCfg_DefaultValues(t *testing.T) {
	if DefaultOutputDir != "/etc/vector/tenants.d" {
		t.Errorf("DefaultOutputDir changed unexpectedly: %s", DefaultOutputDir)
	}
	if DefaultConfigFileKey != "vector-tenant.yaml" {
		t.Errorf("DefaultConfigFileKey changed unexpectedly: %s", DefaultConfigFileKey)
	}
	if DefaultUpstreamTransformID != "extract_billing" {
		t.Errorf("DefaultUpstreamTransformID changed unexpectedly: %s", DefaultUpstreamTransformID)
	}
}
