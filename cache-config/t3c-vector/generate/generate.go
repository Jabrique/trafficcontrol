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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/apache/trafficcontrol/v8/cache-config/t3c-vector/config"
	"github.com/apache/trafficcontrol/v8/cache-config/t3cutil/toreq"
	atscfg "github.com/apache/trafficcontrol/v8/lib/go-atscfg"
	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
)

// tierParamName is the Traffic Ops Parameter.Name that specifies the log streaming
// tier for a Delivery Service. It is a reserved name and must not be treated as
// a sink type.
const tierParamName = "log_streaming_tier"

// dbMaxAgeBeforeRefresh is how old a local database file can be before
// t3c-vector will re-download it even if it has not been modified on the server.
const dbMaxAgeBeforeRefresh = 7 * 24 * time.Hour

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

	// Sync database files before generating configs so enrichment_tables paths
	// are always valid when Vector reloads.
	dbParams, err := fetchDatabaseParameters(toClient, cfg.DatabaseConfigFileKey)
	if err != nil {
		// Non-fatal: log and continue. Missing DB files degrade enrichment but
		// must not block tenant config generation.
		log.Warnf("fetching database parameters: %s\n", err.Error())
	} else if err := syncDatabases(dbParams, cfg.DatabaseDir); err != nil {
		log.Warnf("syncing databases: %s\n", err.Error())
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
//
// The reserved parameter name "log_streaming_tier" (see tierParamName) is
// extracted into TenantDSConfig.Tier and never added to Sinks.
func buildTenantConfigs(dses []atscfg.DeliveryService, params []atscfgParam) ([]TenantDSConfig, error) {
	// Build a profile-name -> []SinkEntry index from the parameters.
	// Parameter.Name is the sink type (e.g. "aws_s3") OR the reserved
	// tierParamName. tierParamName entries are stored separately.
	// Parameter.Value is the raw JSON sink config (or tier string for tier params).
	// Parameter.Profiles is a JSON array of profile names this parameter applies to.
	profileToSinks := map[string][]SinkEntry{}
	profileToTier := map[string]string{}

	for _, p := range params {
		var profiles []string
		if err := json.Unmarshal(p.Profiles, &profiles); err != nil {
			return nil, fmt.Errorf("parameter %q has invalid Profiles JSON: %w", p.Name, err)
		}

		// Handle reserved tier parameter: store tier value per profile, do not
		// treat it as a sink type.
		if p.Name == tierParamName {
			for _, profile := range profiles {
				profileToTier[profile] = p.Value
			}
			continue
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

		tier := profileToTier[*ds.ProfileName]
		if tier == "" {
			tier = "standard" // default: protect GeoIP data unless explicitly unlocked
		}

		result = append(result, TenantDSConfig{
			Tenant: *ds.Tenant,
			XMLID:  ds.XMLID,
			Tier:   tier,
			Sinks:  sinks,
		})
	}
	return result, nil
}

// generateVectorConfig renders a TenantDSConfig into YAML bytes for one file.
// upstreamID is the Vector transform ID that feeds our filter (e.g. "extract_billing").
//
// Generated pipeline topology:
//
//   [upstreamID]
//        |
//   filter_{tenant}__{xmlid}   (passes events matching tenant + delivery_service)
//        |
//   [tier_remap_{tenant}__{xmlid}]  (standard tier only: drops premium GeoIP/anon fields)
//        |
//   ls_{tenant}__{xmlid}           (customer-facing sink; ls_ prefix enables billing tracking)
func generateVectorConfig(tdc TenantDSConfig, upstreamID string) ([]byte, error) {
	filterID := FilterTransformID(tdc.Tenant, tdc.XMLID)

	// The transform that directly feeds the sinks depends on tier.
	// For standard tier we insert an extra remap transform between the filter
	// and the sinks to drop premium fields before delivery.
	var sinkInputID string

	transforms := map[string]VectorTransform{}

	// Filter transform: passes events where both tenant and delivery_service match.
	transforms[filterID] = VectorTransform{
		Type:      "filter",
		Inputs:    []string{upstreamID},
		Condition: fmt.Sprintf(`.tenant == %q && .delivery_service == %q`, tdc.Tenant, tdc.XMLID),
	}

	// Standard tier: inject a remap transform that deletes premium fields.
	// Zero value ("") is treated as "standard" (safe default).
	if tdc.Tier != "premium" {
		tierRemapID := "tier_remap_" + tdc.Tenant + "__" + tdc.XMLID
		transforms[tierRemapID] = VectorTransform{
			Type:   "remap",
			Inputs: []string{filterID},
			// Drop GeoIP and anonymous-IP fields. Also normalize cache_result
			// so customers only see HIT or MISS (hides internal cache states).
			Source: standardTierVRL(),
		}
		sinkInputID = tierRemapID
	} else {
		sinkInputID = filterID
	}

	sinks := map[string]map[string]interface{}{}
	for _, s := range tdc.Sinks {
		// ls_ prefix marks this as a customer-facing delivery destination.
		// The capture_delivery_billing transform in vector.yaml uses this prefix
		// to identify sinks that should be tracked for log streaming billing.
		sinkID := "ls_" + tdc.Tenant + "__" + tdc.XMLID
		params := make(map[string]interface{}, len(s.SinkParams)+2)
		for k, v := range s.SinkParams {
			params[k] = v
		}
		params["type"] = s.SinkType
		params["inputs"] = []string{sinkInputID}
		sinks[sinkID] = params
	}

	file := VectorConfigFile{
		Transforms: transforms,
		Sinks:      sinks,
	}

	// Header is intentionally static (no timestamp) so that bytesEqual() returns
	// true on unchanged configs and t3c-vector does not rewrite/retrigger Vector
	// reload on every timer cycle when nothing has changed.
	header := fmt.Sprintf(
		"# Auto-generated by t3c-vector. Do not edit manually.\n# Tenant: %s | DS: %s | Tier: %s\n\n",
		tdc.Tenant, tdc.XMLID, tdc.Tier,
	)

	body, err := yaml.Marshal(file)
	if err != nil {
		return nil, fmt.Errorf("marshaling config for %s/%s: %w", tdc.Tenant, tdc.XMLID, err)
	}
	return append([]byte(header), body...), nil
}

// standardTierVRL returns the VRL source that drops premium fields and
// normalizes cache_result for standard-tier customers.
// Premium fields dropped: GeoIP location data and anonymous-IP threat intel.
func standardTierVRL() string {
	return `# Standard tier: remove premium enrichment fields before delivery
del(.client_country)
del(.client_registered_country)
del(.client_continent)
del(.client_city)
del(.client_timezone)
del(.client_latitude)
del(.client_longitude)
del(.is_vpn)
del(.is_hosting)
del(.is_proxy)
del(.is_tor)
del(.is_relay)
# Normalize cache result: customers only see HIT or MISS
.cache_result = if contains(string(.cache_result) ?? "", "HIT") { "HIT" } else { "MISS" }
`
}

// fetchDatabaseParameters fetches Traffic Ops Parameters whose ConfigFile matches
// databaseConfigFileKey. Each returned parameter represents one database file:
//
//	Parameter.Name  = filename stem (e.g. "geoip_city" -> geoip_city.mmdb)
//	Parameter.Value = HTTPS URL to download from
func fetchDatabaseParameters(toClient *toreq.TOClient, databaseConfigFileKey string) ([]atscfgParam, error) {
	params, _, err := toClient.GetConfigFileParameters(databaseConfigFileKey, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching parameters for ConfigFile=%q: %w", databaseConfigFileKey, err)
	}
	return params, nil
}

// syncDatabases downloads database files from the URLs stored in params into
// databaseDir. Each file is named "{param.Name}.mmdb".
//
// Download behaviour:
//   - If the file does not exist locally: download unconditionally.
//   - If the file exists and is younger than dbMaxAgeBeforeRefresh: skip.
//   - If the file is older than dbMaxAgeBeforeRefresh: send an
//     If-Modified-Since header. Only re-download if the server returns 200;
//     a 304 Not Modified just updates the local file's mtime.
//
// All downloads are written atomically via a .tmp file to prevent Vector from
// loading a partially written database.
func syncDatabases(params []atscfgParam, databaseDir string) error {
	if len(params) == 0 {
		return nil
	}
	if err := os.MkdirAll(databaseDir, 0755); err != nil {
		return fmt.Errorf("creating database dir %s: %w", databaseDir, err)
	}

	for _, p := range params {
		filename := p.Name + ".mmdb"
		destPath := filepath.Join(databaseDir, filename)
		downloadURL := p.Value

		if err := syncOneDatabase(destPath, downloadURL); err != nil {
			// Log per-file errors but continue so one bad URL does not block others.
			log.Warnf("syncing database %s from %s: %s\n", filename, downloadURL, err.Error())
		}
	}
	return nil
}

// syncOneDatabase downloads a single database file from downloadURL to destPath
// using conditional HTTP requests to avoid redundant downloads.
//
// If the download URL ends with ".gz" (e.g. GeoIP2-City.mmdb.gz), the response
// body is transparently decompressed before writing to destPath. The destination
// file always ends with ".mmdb" regardless of whether the source was compressed.
func syncOneDatabase(destPath, downloadURL string) error {
	var ifModifiedSince time.Time

	fi, statErr := os.Stat(destPath)
	if statErr == nil {
		// File exists. Skip entirely if it is fresh enough.
		if time.Since(fi.ModTime()) < dbMaxAgeBeforeRefresh {
			log.Infof("database %s is fresh (age %s), skipping download\n",
				filepath.Base(destPath), time.Since(fi.ModTime()).Round(time.Minute))
			return nil
		}
		// File is stale: use If-Modified-Since to avoid re-downloading if the
		// server copy has not changed.
		ifModifiedSince = fi.ModTime()
	}

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("building HTTP request for %s: %w", downloadURL, err)
	}
	if !ifModifiedSince.IsZero() {
		req.Header.Set("If-Modified-Since", ifModifiedSince.UTC().Format(http.TimeFormat))
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusNotModified:
		// Server confirms the local copy is up to date. Touch the file mtime
		// so the freshness check above works correctly next run.
		now := time.Now()
		if err := os.Chtimes(destPath, now, now); err != nil {
			log.Warnf("touching mtime for %s: %s\n", destPath, err.Error())
		}
		log.Infof("database %s not modified on server, local copy is current\n", filepath.Base(destPath))
		return nil
	case http.StatusOK:
		// Fall through to write the new file.
	default:
		return fmt.Errorf("unexpected HTTP %d downloading %s", resp.StatusCode, downloadURL)
	}

	// Determine the data source: if URL ends with .gz, wrap in a gzip reader
	// to transparently decompress. destPath always ends with .mmdb.
	var dataReader io.Reader = resp.Body
	if strings.HasSuffix(strings.ToLower(downloadURL), ".gz") {
		gr, err := gzip.NewReader(resp.Body)
		if err != nil {
			return fmt.Errorf("creating gzip reader for %s: %w", downloadURL, err)
		}
		defer gr.Close()
		dataReader = gr
	}

	// Atomic write: download to a .tmp file first, then rename.
	tmpPath := destPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("creating temp file %s: %w", tmpPath, err)
	}

	if _, err := io.Copy(f, dataReader); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("writing database to %s: %w", tmpPath, err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("closing temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming %s -> %s: %w", tmpPath, destPath, err)
	}

	compressedNote := ""
	if strings.HasSuffix(strings.ToLower(downloadURL), ".gz") {
		compressedNote = " (decompressed from .gz)"
	}
	log.Infof("downloaded database %s from %s%s\n", filepath.Base(destPath), downloadURL, compressedNote)
	return nil
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
