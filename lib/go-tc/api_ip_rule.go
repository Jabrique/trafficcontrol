package tc

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
	"errors"
	"net"
	"regexp"
	"time"
)

// APIIPRule is the full representation of an IP access control rule as returned by the API.
type APIIPRule struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`

	// EndpointRegex is a Go regex matched against the URL path after stripping /api/X.Y/.
	// Examples: "^user/api_tokens", "^deliveryservice_stats$"
	EndpointRegex string `json:"endpointRegex"`

	// HTTPMethods restricts the rule to specific HTTP methods.
	// Empty/null = applies to all methods.
	HTTPMethods []string `json:"httpMethods,omitempty"`

	// AllowedCIDRs lists source IPs that are permitted when this rule matches.
	// Empty/null = no IP restriction (all IPs allowed) when this rule matches.
	AllowedCIDRs []string `json:"allowedCidrs,omitempty"`

	// DeniedCIDRs lists source IPs that are always denied when this rule matches.
	// Evaluated before AllowedCIDRs.
	DeniedCIDRs []string `json:"deniedCidrs,omitempty"`

	// AppliesToAPIToken controls whether this rule applies to API token-authenticated requests.
	AppliesToAPIToken bool `json:"appliesToApiToken"`

	// AppliesToSession controls whether this rule applies to cookie/session-authenticated requests.
	AppliesToSession bool `json:"appliesToSession"`

	// Priority determines evaluation order. Lower number = higher priority. First match wins.
	Priority int `json:"priority"`

	// Active controls whether this rule participates in evaluation.
	Active bool `json:"active"`

	LastUpdated time.Time `json:"lastUpdated"`
}

// APIIPRuleCreateRequest is the request body for creating or updating an IP rule.
type APIIPRuleCreateRequest struct {
	Name              string   `json:"name"`
	Description       *string  `json:"description,omitempty"`
	EndpointRegex     string   `json:"endpointRegex"`
	HTTPMethods       []string `json:"httpMethods,omitempty"`
	AllowedCIDRs      []string `json:"allowedCidrs,omitempty"`
	DeniedCIDRs       []string `json:"deniedCidrs,omitempty"`
	AppliesToAPIToken bool     `json:"appliesToApiToken"`
	AppliesToSession  bool     `json:"appliesToSession"`
	Priority          int      `json:"priority"`
	Active            bool     `json:"active"`
}

// Validate implements api.ParseValidator so that api.Parse() can validate the
// request body after JSON unmarshalling.
//
// Validation performed:
//   - name is required
//   - endpointRegex is required and must compile
//   - allowedCidrs / deniedCidrs entries must be valid CIDR notation
//   - priority must be in [1, 9999]
//
// The tx parameter is present to satisfy the interface but is not used here
// (no DB lookups required for structural validation of an IP rule).
func (r *APIIPRuleCreateRequest) Validate(_ *sql.Tx) error {
	if r.Name == "" {
		return errors.New("name is required")
	}
	if r.EndpointRegex == "" {
		return errors.New("endpointRegex is required")
	}
	if _, err := regexp.Compile(r.EndpointRegex); err != nil {
		return errors.New("endpointRegex is not a valid Go regular expression: " + err.Error())
	}
	for _, cidr := range r.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("invalid CIDR in allowedCidrs: " + cidr)
		}
	}
	for _, cidr := range r.DeniedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("invalid CIDR in deniedCidrs: " + cidr)
		}
	}
	if r.Priority < 1 || r.Priority > 9999 {
		return errors.New("priority must be between 1 and 9999")
	}
	return nil
}


// APIIPRulesResponse is the list response wrapper for GET /api_ip_rules.
type APIIPRulesResponse struct {
Alerts
Response []APIIPRule `json:"response"`
}

// APIIPRuleSingleResponse is the single-item response wrapper for GET/POST/PUT /api_ip_rules/{id}.
type APIIPRuleSingleResponse struct {
Alerts
Response APIIPRule `json:"response"`
}

