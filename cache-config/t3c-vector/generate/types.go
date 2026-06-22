// Package generate implements the core config generation logic for t3c-vector.
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

// TenantDSConfig holds all data needed to generate one Vector config file.
// One TenantDSConfig maps to one file in the output directory.
type TenantDSConfig struct {
	// Tenant is the Traffic Ops tenant name (e.g. "wowrack").
	Tenant string

	// XMLID is the Delivery Service XML ID (e.g. "ds-video-streaming").
	XMLID string

	// Sinks is a list of Vector sink configurations to generate for this DS.
	// A DS may have multiple sinks (e.g. both S3 and Splunk).
	Sinks []SinkEntry
}

// SinkEntry represents one Vector sink for a tenant-DS pair.
type SinkEntry struct {
	// SinkType is the Vector sink type, taken from Parameter.Name
	// (e.g. "aws_s3", "splunk_hec", "http", "elasticsearch").
	SinkType string

	// SinkParams is the raw sink config from Parameter.Value (JSON blob).
	// This is a pass-through: t3c-vector does not validate sink-specific fields.
	// The admin is responsible for providing valid Vector sink config.
	SinkParams map[string]interface{}
}

// VectorConfigFile is the top-level structure of a generated .yaml file.
// It maps directly to Vector's config format with top-level transforms/sinks keys.
type VectorConfigFile struct {
	Transforms map[string]VectorTransform         `yaml:"transforms"`
	Sinks      map[string]map[string]interface{}  `yaml:"sinks"`
}

// VectorTransform represents a Vector filter transform.
type VectorTransform struct {
	Type      string   `yaml:"type"`
	Inputs    []string `yaml:"inputs"`
	Condition string   `yaml:"condition"`
}

// RunResult is the operation summary returned by Run().
type RunResult struct {
	Written   int
	Removed   int
	Unchanged int
}
