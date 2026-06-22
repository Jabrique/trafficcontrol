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
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/apache/trafficcontrol/v8/cache-config/t3c-vector/config"
	"github.com/apache/trafficcontrol/v8/cache-config/t3cutil/toreq"
	atscfg "github.com/apache/trafficcontrol/v8/lib/go-atscfg"
	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
)

// atscfgParam is an internal alias for tc.ParameterV5 used by the toreq client.
// Using a type alias here keeps generate_test.go decoupled from lib/go-tc directly.
type atscfgParam = tc.ParameterV5

// Run is the top-level entry point. It fetches data from Traffic Ops, generates
// Vector config files, writes changed files atomically, removes stale files, and
// optionally triggers a Vector reload.
func Run(cfg config.Cfg) (RunResult, error) {
	toClient, err := connectToTrafficOps(cfg)
	if err != nil {
		return RunResult{}, fmt.Errorf("connecting to Traffic Ops: %w", err)
	}

	dses, err := fetchDeliveryServices(toClient, cfg.CDNName)
	if err != nil {
		return RunResult{}, fmt.Errorf("fetching delivery services: %w", err)
	}

	params, err := fetchVectorParameters(toClient, cfg.ConfigFileKey)
	if err != nil {
		return RunResult{}, fmt.Errorf("fetching parameters: %w", err)
	}

	tenantConfigs, err := buildTenantConfigs(dses, params)
	if err != nil {
		return RunResult{}, fmt.Errorf("building tenant configs: %w", err)
	}

	if cfg.DryRun {
		return printDryRun(tenantConfigs, cfg.UpstreamTransformID), nil
	}

	result, err := applyConfigs(tenantConfigs, cfg)
	if err != nil {
		return RunResult{}, fmt.Errorf("applying configs: %w", err)
	}

	if (result.Written > 0 || result.Removed > 0) && cfg.ReloadCommand != "" {
		if err := triggerReload(cfg.ReloadCommand); err != nil {
			// Log but do not fail: Vector's --watch-config may still handle it.
			log.Warnf("reload command failed: %s\n", err.Error())
		}
	}

	return result, nil
}

// connectToTrafficOps creates an authenticated Traffic Ops client.
func connectToTrafficOps(cfg config.Cfg) (*toreq.TOClient, error) {
	toURL, err := url.Parse(cfg.TOUrl)
	if err != nil {
		return nil, fmt.Errorf("parsing Traffic Ops URL %q: %w", cfg.TOUrl, err)
	}
	client, err := toreq.New(
		toURL,
		cfg.TOUser,
		cfg.TOPassword,
		cfg.TOInsecure,
		cfg.TOTimeout,
		fmt.Sprintf("t3c-vector/%s", cfg.Version),
	)
	if err != nil {
		return nil, fmt.Errorf("authenticating to Traffic Ops at %s: %w", cfg.TOUrl, err)
	}
	return client, nil
}

// fetchDeliveryServices fetches all Delivery Services for the named CDN.
// Returns an error if the CDN is not found or the API call fails.
func fetchDeliveryServices(toClient *toreq.TOClient, cdnName string) ([]atscfg.DeliveryService, error) {
	cdn, _, err := toClient.GetCDN(tc.CDNName(cdnName), nil)
	if err != nil {
		return nil, fmt.Errorf("fetching CDN %q: %w", cdnName, err)
	}

	dses, _, err := toClient.GetCDNDeliveryServices(cdn.ID, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching delivery services for CDN %q (ID %d): %w", cdnName, cdn.ID, err)
	}
	return dses, nil
}

// fetchVectorParameters fetches Traffic Ops Parameters whose ConfigFile matches configFileKey.
func fetchVectorParameters(toClient *toreq.TOClient, configFileKey string) ([]atscfgParam, error) {
	params, _, err := toClient.GetConfigFileParameters(configFileKey, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching parameters for ConfigFile=%q: %w", configFileKey, err)
	}
	return params, nil
}

// buildTenantConfigs matches each Delivery Service to its sink Parameters and
// builds the list of TenantDSConfig values to generate. DSes with no matching
// parameters are skipped (no log sink configured). DSes with nil Tenant are
// skipped with a warning.
func buildTenantConfigs(dses []atscfg.DeliveryService, params []atscfgParam) ([]TenantDSConfig, error) {
	// Build a profile-name -> []SinkEntry index from the parameters.
	// Parameter.Name is the sink type (e.g. "aws_s3").
	// Parameter.Value is the raw JSON sink config.
	// Parameter.Profiles is a JSON array of profile names this parameter applies to.
	profileToSinks := map[string][]SinkEntry{}
	for _, p := range params {
		var profiles []string
		if err := json.Unmarshal(p.Profiles, &profiles); err != nil {
			return nil, fmt.Errorf("parameter %q has invalid Profiles JSON: %w", p.Name, err)
		}

		// Use json.Decoder with UseNumber to preserve integer types.
		// json.Unmarshal with interface{} converts all JSON numbers to float64,
		// which yaml.Marshal emits as scientific notation (e.g. 1.048576e+07
		// instead of 10485760). Vector rejects float values for integer fields
		// like max_bytes with "expected usize".
		dec := json.NewDecoder(strings.NewReader(p.Value))
		dec.UseNumber()
		var rawParams map[string]interface{}
		if err := dec.Decode(&rawParams); err != nil {
			return nil, fmt.Errorf("parameter %q has invalid Value JSON (must be valid Vector sink config): %w", p.Name, err)
		}
		sinkParams := normalizeJSONNumbers(rawParams).(map[string]interface{})

		entry := SinkEntry{SinkType: p.Name, SinkParams: sinkParams}
		for _, profile := range profiles {
			profileToSinks[profile] = append(profileToSinks[profile], entry)
		}
	}

	var result []TenantDSConfig
	for _, ds := range dses {
		if ds.Tenant == nil || *ds.Tenant == "" {
			log.Warnf("DS %q has no tenant, skipping\n", ds.XMLID)
			continue
		}
		if ds.ProfileName == nil {
			log.Warnf("DS %q has no profile, skipping\n", ds.XMLID)
			continue
		}

		sinks, ok := profileToSinks[*ds.ProfileName]
		if !ok || len(sinks) == 0 {
			// No vector-tenant.yaml parameters on this DS's profile. Normal case:
			// this DS has no log sink configured. Not an error.
			continue
		}

		result = append(result, TenantDSConfig{
			Tenant: *ds.Tenant,
			XMLID:  ds.XMLID,
			Sinks:  sinks,
		})
	}
	return result, nil
}

// generateVectorConfig renders a TenantDSConfig into YAML bytes for one file.
// upstreamID is the Vector transform ID that feeds our filter (e.g. "extract_billing").
func generateVectorConfig(tdc TenantDSConfig, upstreamID string) ([]byte, error) {
	filterID := FilterTransformID(tdc.Tenant, tdc.XMLID)

	// Filter transform: passes events where both fields match.
	transform := VectorTransform{
		Type:      "filter",
		Inputs:    []string{upstreamID},
		Condition: fmt.Sprintf(`.tenant == %q && .delivery_service == %q`, tdc.Tenant, tdc.XMLID),
	}

	sinks := map[string]map[string]interface{}{}
	for _, s := range tdc.Sinks {
		sinkID := SinkComponentID(s.SinkType, tdc.Tenant, tdc.XMLID)
		// Clone the sink params and inject the required Vector fields.
		params := make(map[string]interface{}, len(s.SinkParams)+2)
		for k, v := range s.SinkParams {
			params[k] = v
		}
		params["type"] = s.SinkType
		params["inputs"] = []string{filterID}
		sinks[sinkID] = params
	}

	file := VectorConfigFile{
		Transforms: map[string]VectorTransform{filterID: transform},
		Sinks:      sinks,
	}

	// Header is intentionally static (no timestamp) so that bytesEqual() returns
	// true on unchanged configs and t3c-vector does not rewrite/retrigger Vector
	// reload on every timer cycle when nothing has changed.
	header := fmt.Sprintf(
		"# Auto-generated by t3c-vector. Do not edit manually.\n# Tenant: %s | DS: %s\n\n",
		tdc.Tenant, tdc.XMLID,
	)

	body, err := yaml.Marshal(file)
	if err != nil {
		return nil, fmt.Errorf("marshaling config for %s/%s: %w", tdc.Tenant, tdc.XMLID, err)
	}
	return append([]byte(header), body...), nil
}

// applyConfigs writes generated files to disk (atomic rename) and removes orphans.
// Returns a summary of changes made.
func applyConfigs(tenantConfigs []TenantDSConfig, cfg config.Cfg) (RunResult, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0755); err != nil {
		return RunResult{}, fmt.Errorf("creating output dir %s: %w", cfg.OutputDir, err)
	}

	// Step 1: generate all files into memory first (fail fast before touching disk).
	generated := map[string][]byte{}
	for _, tdc := range tenantConfigs {
		filename := SanitizeFileName(tdc.Tenant, tdc.XMLID)
		content, err := generateVectorConfig(tdc, cfg.UpstreamTransformID)
		if err != nil {
			return RunResult{}, err
		}
		generated[filename] = content
	}

	// Step 2: read existing files in the output dir.
	existing, err := readExistingYamlFiles(cfg.OutputDir)
	if err != nil {
		return RunResult{}, err
	}

	var result RunResult

	// Step 3: write changed/new files atomically.
	for filename, content := range generated {
		destPath := filepath.Join(cfg.OutputDir, filename)
		if existingContent, found := existing[filename]; found && bytesEqual(existingContent, content) {
			result.Unchanged++
			continue
		}
		if err := atomicWrite(destPath, content); err != nil {
			return RunResult{}, fmt.Errorf("writing %s: %w", destPath, err)
		}
		log.Infof("wrote %s\n", destPath)
		result.Written++
	}

	// Step 4: remove stale files (tenant removed, DS deleted, profile changed).
	for filename := range existing {
		if _, stillNeeded := generated[filename]; !stillNeeded {
			stalePath := filepath.Join(cfg.OutputDir, filename)
			if err := os.Remove(stalePath); err != nil && !os.IsNotExist(err) {
				return RunResult{}, fmt.Errorf("removing stale file %s: %w", stalePath, err)
			}
			log.Infof("removed stale config %s\n", stalePath)
			result.Removed++
		}
	}

	return result, nil
}

// readExistingYamlFiles returns the content of all .yaml and .yml files in dir,
// keyed by filename (not full path). Ignores non-yaml files silently.
func readExistingYamlFiles(dir string) (map[string][]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]byte{}, nil
		}
		return nil, fmt.Errorf("reading output dir %s: %w", dir, err)
	}
	result := map[string][]byte{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("reading existing file %s: %w", name, err)
		}
		result[name] = content
	}
	return result, nil
}

// atomicWrite writes content to path atomically by writing to a .tmp file first
// and then renaming. This prevents Vector from loading a half-written file.
// The .tmp extension is not recognized by Vector's format detector (format.rs:65-72)
// so it is safe to write even while --watch-config is active.
func atomicWrite(path string, content []byte) error {
	tmpPath := path + ".tmp"
	// Mode 0644: vector user (running as a different UID) must be able to read
	// these files. Owner write + world read is correct here since the directory
	// itself is protected (mode 750, owned by vector:vector).
	if err := os.WriteFile(tmpPath, content, 0644); err != nil {
		return fmt.Errorf("writing temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming %s -> %s: %w", tmpPath, path, err)
	}
	return nil
}

// normalizeJSONNumbers recursively converts json.Number values (produced by
// json.Decoder with UseNumber) to int64 for whole numbers or float64 for
// decimals. This ensures yaml.Marshal emits clean integers (10485760) instead
// of floating-point notation (1.048576e+07), which Vector rejects for
// integer-typed fields like batch.max_bytes.
func normalizeJSONNumbers(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, v2 := range val {
			val[k] = normalizeJSONNumbers(v2)
		}
		return val
	case []interface{}:
		for i, v2 := range val {
			val[i] = normalizeJSONNumbers(v2)
		}
		return val
	case json.Number:
		// Prefer int64 for whole numbers so YAML emits clean integers.
		if i64, err := val.Int64(); err == nil {
			return i64
		}
		if f64, err := val.Float64(); err == nil {
			return f64
		}
		return val.String()
	default:
		return v
	}
}

// bytesEqual compares two byte slices. Used for diff-and-apply: only write if changed.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// triggerReload runs the optional reload command in a shell.
// A non-zero exit code is returned as an error but does not fail the entire run.
func triggerReload(cmd string) error {
	log.Infof("running reload command: %s\n", cmd)
	out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
	if err != nil {
		return fmt.Errorf("reload command %q failed: %s\noutput: %s", cmd, err.Error(), string(out))
	}
	log.Infof("reload command output: %s\n", string(out))
	return nil
}

// printDryRun prints what would be written/removed without touching the filesystem.
func printDryRun(tenantConfigs []TenantDSConfig, upstreamID string) RunResult {
	fmt.Println("-- DRY RUN: no files will be written --")
	for _, tdc := range tenantConfigs {
		filename := SanitizeFileName(tdc.Tenant, tdc.XMLID)
		content, err := generateVectorConfig(tdc, upstreamID)
		if err != nil {
			fmt.Printf("ERROR generating %s: %s\n", filename, err.Error())
			continue
		}
		fmt.Printf("\n=== %s ===\n%s\n", filename, string(content))
	}
	return RunResult{Written: 0, Removed: 0, Unchanged: 0}
}
