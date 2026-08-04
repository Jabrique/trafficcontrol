// Package apiiprule provides HTTP handlers for the API IP rule management endpoints.
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
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/api"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/iprule"
)

// evaluateRequest is the request body for POST /api/5.0/api_ip_rules/evaluate.
type evaluateRequest struct {
	// IP is the client IP address to test (IPv4 or IPv6, without CIDR notation).
	IP string `json:"ip"`
	// Method is the HTTP method to test (e.g. "GET", "POST"). Case-insensitive.
	Method string `json:"method"`
	// Path is the API path to test (e.g. "user/api_tokens"). Should not include /api/5.0/ prefix.
	Path string `json:"path"`
	// AuthType is "token" for API token auth, "session" for cookie/session auth.
	// Defaults to "token" if omitted.
	AuthType string `json:"authType"`
}

// evaluateResponse is the response body for POST /api/5.0/api_ip_rules/evaluate.
type evaluateResponse struct {
	// Allowed is true if the request would be permitted by the current rule set.
	Allowed bool `json:"allowed"`
	// MatchedRule is the name of the first rule that matched the request, or empty
	// string if no rule matched (fail-open: allowed with no explicit restriction).
	MatchedRule string `json:"matchedRule"`
	// Message is a human-readable summary of the evaluation result.
	Message string `json:"message"`
}

// EvaluateHandler returns an http.HandlerFunc for POST /api/5.0/api_ip_rules/evaluate.
// It uses a closure to capture the RuleCache, avoiding global state.
//
// The endpoint lets admins test IP rule evaluation without making real requests.
// It reuses the same Check() logic as the middleware, so results are authoritative.
func EvaluateHandler(cache *iprule.RuleCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		inf, userErr, sysErr, errCode := api.NewInfo(r, nil, nil)
		if userErr != nil || sysErr != nil {
			api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
			return
		}
		defer inf.Close()

		var req evaluateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, err, nil)
			return
		}

		if req.IP == "" {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
				errMsg("ip is required"), nil)
			return
		}
		if req.Method == "" {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
				errMsg("method is required"), nil)
			return
		}
		if req.Path == "" {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
				errMsg("path is required"), nil)
			return
		}

		// Normalise inputs.
		ip := req.IP
		if idx := strings.Index(ip, "/"); idx != -1 {
			ip = ip[:idx] // strip CIDR mask if user accidentally included it
		}
		if net.ParseIP(ip) == nil {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
				errMsg("ip is not a valid IPv4 or IPv6 address"), nil)
			return
		}

		path := strings.TrimLeft(req.Path, "/")
		method := strings.ToUpper(req.Method)

		// "token" is the default; only "session" opts out.
		isAPITokenAuth := !strings.EqualFold(req.AuthType, "session")

		if cache == nil {
			// Cache not yet initialised — mirror middleware's fail-open behaviour.
			api.WriteRespRaw(w, r, evaluateResponse{
				Allowed:     true,
				MatchedRule: "",
				Message:     "IP rule cache not initialised — evaluated as allowed (fail-open).",
			})
			return
		}

		allowed, matchedRule := cache.Check(path, method, ip, isAPITokenAuth)

		var message string
		switch {
		case matchedRule == "":
			message = "No rule matched — request is allowed (fail-open default)."
		case allowed:
			message = "Rule \"" + matchedRule + "\" matched and ALLOWS this request."
		default:
			message = "Rule \"" + matchedRule + "\" matched and DENIES this request."
		}

		api.WriteRespRaw(w, r, evaluateResponse{
			Allowed:     allowed,
			MatchedRule: matchedRule,
			Message:     message,
		})
	}
}

// errMsg wraps a string as an error for use with api.HandleErr.
func errMsg(s string) error { return &simpleError{s} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
