// Package apiiprule provides HTTP handlers for the API IP rule management endpoints.
// It is separate from the iprule package (which contains the cache and middleware logic)
// to avoid a circular import: api → iprule → api.
package apiiprule

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
	"net"
	"net/http"
	"regexp"
	"strconv"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/api"
	"github.com/lib/pq"
)

// GetAll handles GET /api/5.0/api_ip_rules
// Returns all IP rules (active and inactive) sorted by priority ASC.
func GetAll(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, nil, nil)
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	const qry = `
SELECT
    id, name, COALESCE(description, '') AS description, endpoint_regex,
    COALESCE(http_methods, ARRAY[]::TEXT[]) AS http_methods,
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]) AS allowed_cidrs,
    COALESCE(denied_cidrs, ARRAY[]::TEXT[]) AS denied_cidrs,
    applies_to_api_token, applies_to_session,
    priority, active, last_updated
FROM api_ip_rule
ORDER BY priority ASC, id ASC
`
	rows, err := inf.Tx.Tx.Query(qry)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, errors.New("querying api_ip_rule: "+err.Error()))
		return
	}
	defer rows.Close()

	var rules []tc.APIIPRule
	for rows.Next() {
		var rule tc.APIIPRule
		var descStr string
		var httpMethods pq.StringArray
		var allowedCIDRs pq.StringArray
		var deniedCIDRs pq.StringArray
		if err := rows.Scan(
			&rule.ID, &rule.Name, &descStr, &rule.EndpointRegex,
			&httpMethods, &allowedCIDRs, &deniedCIDRs,
			&rule.AppliesToAPIToken, &rule.AppliesToSession,
			&rule.Priority, &rule.Active, &rule.LastUpdated,
		); err != nil {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, errors.New("scanning api_ip_rule: "+err.Error()))
			return
		}
		if descStr != "" {
			rule.Description = &descStr
		}
		rule.HTTPMethods = []string(httpMethods)
		rule.AllowedCIDRs = []string(allowedCIDRs)
		rule.DeniedCIDRs = []string(deniedCIDRs)
		rules = append(rules, rule)
	}
	if err := rows.Err(); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, errors.New("iterating api_ip_rule rows: "+err.Error()))
		return
	}

	api.WriteResp(w, r, rules)
}

// Get handles GET /api/5.0/api_ip_rules/{id}
func Get(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, []string{"id"}, []string{"id"})
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	id, err := strconv.ParseInt(inf.Params["id"], 10, 64)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, errors.New("id must be an integer"), nil)
		return
	}

	var rule tc.APIIPRule
	var descStr string
	var httpMethods pq.StringArray
	var allowedCIDRs pq.StringArray
	var deniedCIDRs pq.StringArray

	err = inf.Tx.Tx.QueryRow(`
SELECT
    id, name, COALESCE(description, '') AS description, endpoint_regex,
    COALESCE(http_methods, ARRAY[]::TEXT[]) AS http_methods,
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]) AS allowed_cidrs,
    COALESCE(denied_cidrs, ARRAY[]::TEXT[]) AS denied_cidrs,
    applies_to_api_token, applies_to_session,
    priority, active, last_updated
FROM api_ip_rule WHERE id = $1
`, id).Scan(
		&rule.ID, &rule.Name, &descStr, &rule.EndpointRegex,
		&httpMethods, &allowedCIDRs, &deniedCIDRs,
		&rule.AppliesToAPIToken, &rule.AppliesToSession,
		&rule.Priority, &rule.Active, &rule.LastUpdated,
	)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusNotFound, errors.New("ip rule not found"), nil)
		return
	}
	if descStr != "" {
		rule.Description = &descStr
	}
	rule.HTTPMethods = []string(httpMethods)
	rule.AllowedCIDRs = []string(allowedCIDRs)
	rule.DeniedCIDRs = []string(deniedCIDRs)

	api.WriteResp(w, r, rule)
}

// Create handles POST /api/5.0/api_ip_rules
func Create(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, nil, nil)
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	var req tc.APIIPRuleCreateRequest
	if err := api.Parse(r.Body, inf.Tx.Tx, &req); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, err, nil)
		return
	}

	if userErr := validateIPRuleRequest(req); userErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, userErr, nil)
		return
	}

	var rule tc.APIIPRule
	var descStr string
	err := inf.Tx.Tx.QueryRow(`
INSERT INTO api_ip_rule
    (name, description, endpoint_regex, http_methods, allowed_cidrs, denied_cidrs,
     applies_to_api_token, applies_to_session, priority, active, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, name, COALESCE(description,'') AS description, endpoint_regex,
    COALESCE(http_methods, ARRAY[]::TEXT[]) AS http_methods,
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]) AS allowed_cidrs,
    COALESCE(denied_cidrs, ARRAY[]::TEXT[]) AS denied_cidrs,
    applies_to_api_token, applies_to_session, priority, active, last_updated
`,
		req.Name, req.Description, req.EndpointRegex,
		pq.Array(req.HTTPMethods), pq.Array(req.AllowedCIDRs), pq.Array(req.DeniedCIDRs),
		req.AppliesToAPIToken, req.AppliesToSession,
		req.Priority, req.Active, inf.User.ID,
	).Scan(
		&rule.ID, &rule.Name, &descStr, &rule.EndpointRegex,
		pq.Array(&rule.HTTPMethods), pq.Array(&rule.AllowedCIDRs), pq.Array(&rule.DeniedCIDRs),
		&rule.AppliesToAPIToken, &rule.AppliesToSession,
		&rule.Priority, &rule.Active, &rule.LastUpdated,
	)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, errors.New("inserting api_ip_rule: "+err.Error()))
		return
	}
	if descStr != "" {
		rule.Description = &descStr
	}

	api.WriteRespAlertObj(w, r, tc.SuccessLevel, "IP rule created", rule)
}

// Update handles PUT /api/5.0/api_ip_rules/{id}
func Update(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, []string{"id"}, []string{"id"})
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	id, err := strconv.ParseInt(inf.Params["id"], 10, 64)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, errors.New("id must be an integer"), nil)
		return
	}

	var req tc.APIIPRuleCreateRequest
	if err := api.Parse(r.Body, inf.Tx.Tx, &req); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, err, nil)
		return
	}
	if userErr := validateIPRuleRequest(req); userErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, userErr, nil)
		return
	}

	var rule tc.APIIPRule
	var descStr string
	err = inf.Tx.Tx.QueryRow(`
UPDATE api_ip_rule SET
    name = $1, description = $2, endpoint_regex = $3,
    http_methods = $4, allowed_cidrs = $5, denied_cidrs = $6,
    applies_to_api_token = $7, applies_to_session = $8,
    priority = $9, active = $10, last_updated = NOW()
WHERE id = $11
RETURNING id, name, COALESCE(description,'') AS description, endpoint_regex,
    COALESCE(http_methods, ARRAY[]::TEXT[]) AS http_methods,
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]) AS allowed_cidrs,
    COALESCE(denied_cidrs, ARRAY[]::TEXT[]) AS denied_cidrs,
    applies_to_api_token, applies_to_session, priority, active, last_updated
`,
		req.Name, req.Description, req.EndpointRegex,
		pq.Array(req.HTTPMethods), pq.Array(req.AllowedCIDRs), pq.Array(req.DeniedCIDRs),
		req.AppliesToAPIToken, req.AppliesToSession,
		req.Priority, req.Active, id,
	).Scan(
		&rule.ID, &rule.Name, &descStr, &rule.EndpointRegex,
		pq.Array(&rule.HTTPMethods), pq.Array(&rule.AllowedCIDRs), pq.Array(&rule.DeniedCIDRs),
		&rule.AppliesToAPIToken, &rule.AppliesToSession,
		&rule.Priority, &rule.Active, &rule.LastUpdated,
	)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusNotFound, errors.New("ip rule not found or update failed"), nil)
		return
	}
	if descStr != "" {
		rule.Description = &descStr
	}

	api.WriteRespAlertObj(w, r, tc.SuccessLevel, "IP rule updated", rule)
}

// Delete handles DELETE /api/5.0/api_ip_rules/{id}
func Delete(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, []string{"id"}, []string{"id"})
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	id, err := strconv.ParseInt(inf.Params["id"], 10, 64)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, errors.New("id must be an integer"), nil)
		return
	}

	result, err := inf.Tx.Tx.Exec(`DELETE FROM api_ip_rule WHERE id = $1`, id)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, errors.New("deleting api_ip_rule: "+err.Error()))
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusNotFound, errors.New("ip rule not found"), nil)
		return
	}

	api.WriteRespAlert(w, r, tc.SuccessLevel, "IP rule deleted")
}

// validateIPRuleRequest validates the fields of an API IP rule create/update request.
func validateIPRuleRequest(req tc.APIIPRuleCreateRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if req.EndpointRegex == "" {
		return errors.New("endpointRegex is required")
	}
	// Verify the regex compiles.
	if _, err := regexp.Compile(req.EndpointRegex); err != nil {
		return errors.New("endpointRegex is not a valid Go regular expression: " + err.Error())
	}
	// Validate CIDR lists.
	for _, cidr := range req.AllowedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("invalid CIDR in allowedCidrs: " + cidr)
		}
	}
	for _, cidr := range req.DeniedCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return errors.New("invalid CIDR in deniedCidrs: " + cidr)
		}
	}
	if req.Priority < 1 || req.Priority > 9999 {
		return errors.New("priority must be between 1 and 9999")
	}
	return nil
}
