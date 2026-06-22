// Package config handles CLI flag and environment variable parsing for t3c-vector.
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
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/pborman/getopt/v2"
)

// Cfg holds all runtime configuration for t3c-vector.
type Cfg struct {
	// Traffic Ops connection
	TOUrl      string
	TOUser     string
	TOPassword string
	TOInsecure bool
	TOTimeout  time.Duration

	// CDN scope
	CDNName string

	// Vector-specific
	ConfigFileKey       string // Parameter.ConfigFile value to filter on
	OutputDir           string // directory to write generated .yaml files
	UpstreamTransformID string // the Vector transform ID that feeds our filters
	ReloadCommand       string // optional shell command to trigger Vector reload
	DryRun              bool   // print changes without writing

	// Logging
	LogLocationError   string
	LogLocationWarning string
	LogLocationInfo    string

	Version     string
	GitRevision string
}

// DefaultOutputDir is the default directory for generated tenant configs.
const DefaultOutputDir = "/etc/vector/tenants.d"

// DefaultConfigFileKey is the Traffic Ops Parameter.ConfigFile value that marks
// a parameter as a Vector sink configuration.
const DefaultConfigFileKey = "vector-tenant.yaml"

// DefaultUpstreamTransformID is the Vector transform ID that extracts billing
// fields from the raw access log. Matches the main vector.yaml transform name.
const DefaultUpstreamTransformID = "extract_billing"

// ErrorLog implements log.Config.
func (cfg Cfg) ErrorLog() log.LogLocation { return log.LogLocation(cfg.LogLocationError) }

// WarningLog implements log.Config.
func (cfg Cfg) WarningLog() log.LogLocation { return log.LogLocation(cfg.LogLocationWarning) }

// InfoLog implements log.Config.
func (cfg Cfg) InfoLog() log.LogLocation { return log.LogLocation(cfg.LogLocationInfo) }

// DebugLog implements log.Config. Debug logging is not used by t3c-vector.
func (cfg Cfg) DebugLog() log.LogLocation { return log.LogLocation(log.LogLocationNull) }

// EventLog implements log.Config. Event logging is not used by t3c-vector.
func (cfg Cfg) EventLog() log.LogLocation { return log.LogLocation(log.LogLocationNull) }

// GetCfg parses CLI flags and environment variables, returning a validated Cfg.
func GetCfg(appName, version, gitRevision string) (Cfg, error) {
	cfg := Cfg{
		Version:             version,
		GitRevision:         gitRevision,
		ConfigFileKey:       DefaultConfigFileKey,
		OutputDir:           DefaultOutputDir,
		UpstreamTransformID: DefaultUpstreamTransformID,
		TOInsecure:          false,
		TOTimeout:           30 * time.Second,
		LogLocationError:    "stderr",
		LogLocationWarning:  "stderr",
		LogLocationInfo:     "stderr",
	}

	toURL := getopt.StringLong("traffic-ops-url", 'u', "", "Traffic Ops URL (required). Env: TO_URL")
	toUser := getopt.StringLong("traffic-ops-user", 'U', "", "Traffic Ops username (required). Env: TO_USER")
	toPass := getopt.StringLong("traffic-ops-password", 'P', "", "Traffic Ops password (required). Env: TO_PASS")
	toInsecure := getopt.BoolLong("traffic-ops-insecure", 'i', "Skip TLS certificate verification")
	toTimeoutMs := getopt.IntLong("traffic-ops-timeout-milliseconds", 't', 30000, "Traffic Ops request timeout in milliseconds")

	cdnName := getopt.StringLong("cdn-name", 'n', "", "CDN name to fetch Delivery Services for (required)")

	configFileKey := getopt.StringLong("config-file-key", 'k', DefaultConfigFileKey,
		"Traffic Ops Parameter.ConfigFile value identifying Vector sink parameters")
	outputDir := getopt.StringLong("output-dir", 'o', DefaultOutputDir,
		"Directory to write per-tenant Vector config files")
	upstreamID := getopt.StringLong("upstream-transform-id", 's', DefaultUpstreamTransformID,
		"Vector transform ID that feeds the per-tenant filter transforms")
	reloadCmd := getopt.StringLong("reload-command", 'r', "",
		"Shell command to run after config changes (leave empty when --watch-config is used)")
	dryRun := getopt.BoolLong("dry-run", 'd', "Print changes without writing files or running reload")
	logError := getopt.StringLong("log-location-error", 'l', "stderr", "Log location for error messages")
	logWarn := getopt.StringLong("log-location-warning", 'w', "stderr", "Log location for warning messages")
	logInfo := getopt.StringLong("log-location-info", 'f', "stderr", "Log location for info messages")
	printVersion := getopt.BoolLong("version", 'v', "Print version and exit")
	help := getopt.BoolLong("help", 'h', "Print help and exit")

	getopt.Parse()

	if *help {
		getopt.Usage()
		os.Exit(0)
	}
	if *printVersion {
		fmt.Printf("%s %s (%s)\n", appName, version, gitRevision)
		os.Exit(0)
	}

	// Environment variable fallbacks (mirrors t3c-apply convention).
	if *toURL == "" {
		*toURL = os.Getenv("TO_URL")
	}
	if *toUser == "" {
		*toUser = os.Getenv("TO_USER")
	}
	if *toPass == "" {
		*toPass = os.Getenv("TO_PASS")
	}

	cfg.TOUrl = *toURL
	cfg.TOUser = *toUser
	cfg.TOPassword = *toPass
	cfg.TOInsecure = *toInsecure
	cfg.TOTimeout = time.Duration(*toTimeoutMs) * time.Millisecond
	cfg.CDNName = *cdnName
	cfg.ConfigFileKey = *configFileKey
	cfg.OutputDir = *outputDir
	cfg.UpstreamTransformID = *upstreamID
	cfg.ReloadCommand = *reloadCmd
	cfg.DryRun = *dryRun
	cfg.LogLocationError = *logError
	cfg.LogLocationWarning = *logWarn
	cfg.LogLocationInfo = *logInfo

	return cfg, validateCfg(cfg)
}

// validateCfg checks that all required fields are set.
func validateCfg(cfg Cfg) error {
	var errs []error
	if cfg.TOUrl == "" {
		errs = append(errs, errors.New("--traffic-ops-url is required (or set TO_URL env var)"))
	}
	if cfg.TOUser == "" {
		errs = append(errs, errors.New("--traffic-ops-user is required (or set TO_USER env var)"))
	}
	if cfg.TOPassword == "" {
		errs = append(errs, errors.New("--traffic-ops-password is required (or set TO_PASS env var)"))
	}
	if cfg.CDNName == "" {
		errs = append(errs, errors.New("--cdn-name is required"))
	}
	if cfg.OutputDir == "" {
		errs = append(errs, errors.New("--output-dir cannot be empty"))
	}
	if cfg.UpstreamTransformID == "" {
		errs = append(errs, errors.New("--upstream-transform-id cannot be empty"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("configuration errors: %v", errs)
	}
	return nil
}
