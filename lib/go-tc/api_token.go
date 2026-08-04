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
	"regexp"
	"time"
)

// tokenNamePattern validates that a token name contains only safe characters.
// This prevents injection and display issues in logs and UI.
var tokenNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// permPattern validates the format of a scoped permission string: e.g. "DS:READ".
var permPattern = regexp.MustCompile(`^[A-Z][A-Z0-9-]*:[A-Z]+$`)

const (
	// APITokenPrefix is the fixed prefix for all API tokens.
	// This is part of the token protocol (used for detection in GetUserFromReq),
	// not a configuration value.
	// Numeric limits (MaxPerUser, MaxExpiryDays, etc.) are controlled via cdn.conf.
	APITokenPrefix = "to_at_"
)

// APITokenCreateRequest is the request payload for creating a new API token.
// expires_at is a required field — the server does not auto-apply a default.
type APITokenCreateRequest struct {
	// Name is a human-readable identifier for the token. Required.
	// Must match ^[a-zA-Z0-9_-]+$ and be unique per user.
	Name string `json:"name"`

	// ExpiresAt is when the token becomes invalid. Required.
	// Must be in the future and no more than api_token_max_expiry_days from now.
	ExpiresAt time.Time `json:"expiresAt"`

	// ScopedPermissions optionally restricts which capabilities this token can use.
	// If omitted, the token inherits all of the user's role permissions.
	// Each entry must match ^[A-Z][A-Z0-9-]*:[A-Z]+$ (e.g. "DS:READ").
	// Effective capabilities = intersection of ScopedPermissions ∩ user's current capabilities.
	ScopedPermissions []string `json:"scopedPermissions,omitempty"`

	// AllowedCIDRs optionally restricts which source IPs can use this token.
	// If omitted, any IP is allowed (subject to IP rule system separately).
	// Required when token owner has priv_level >= PrivLevelOperations (20).
	AllowedCIDRs []string `json:"allowedCidrs,omitempty"`
}

// APITokenCreateResponse is returned exactly once when a token is created.
// The Token field contains the plaintext token — it is NOT stored server-side
// and will NEVER be retrievable again after this response.
type APITokenCreateResponse struct {
	ID int64 `json:"id"`
	// Name is the human-readable identifier.
	Name string `json:"name"`
	// Token is the full plaintext token (to_at_<publicID>_<secret>).
	// Store this immediately — it cannot be retrieved later.
	Token string `json:"token"`
	// TokenPrefix is the "to_at_<publicID>" portion — safe to log and store.
	TokenPrefix       string    `json:"tokenPrefix"`
	ScopedPermissions []string  `json:"scopedPermissions,omitempty"`
	AllowedCIDRs      []string  `json:"allowedCidrs,omitempty"`
	ExpiresAt         time.Time `json:"expiresAt"`
	CreatedAt         time.Time `json:"createdAt"`
}

// APITokenResponse is the representation used in list and get responses.
// It deliberately omits the token secret and hash — they are never exposed after creation.
type APITokenResponse struct {
	ID int64 `json:"id"`
	// Name is the human-readable identifier.
	Name string `json:"name"`
	// TokenPrefix is "to_at_<publicID>" — safe to display and log.
	TokenPrefix       string     `json:"tokenPrefix"`
	ScopedPermissions []string   `json:"scopedPermissions,omitempty"`
	AllowedCIDRs      []string   `json:"allowedCidrs,omitempty"`
	ExpiresAt         time.Time  `json:"expiresAt"`
	LastUsedAt        *time.Time `json:"lastUsedAt"`
	CreatedAt         time.Time  `json:"createdAt"`
}

// Validate implements api.ParseValidator so api.Parse() validates the request body
// after JSON unmarshalling.
//
// Structural validations (config-independent):
//   - name required, must match ^[a-zA-Z0-9_-]+$
//   - expiresAt must not be zero
//   - each scopedPermissions entry must match ^[A-Z][A-Z0-9-]*:[A-Z]+$
//
// Config-dependent validations (future expiry, max expiry days, allowed_cidrs
// requirement) happen in the handler where cfg is available.
func (r *APITokenCreateRequest) Validate(_ *sql.Tx) error {
	if r.Name == "" {
		return errors.New("name is required")
	}
	if !tokenNamePattern.MatchString(r.Name) {
		return errors.New("name must match ^[a-zA-Z0-9_-]+$")
	}
	if r.ExpiresAt.IsZero() {
		return errors.New("expiresAt is required")
	}
	for _, perm := range r.ScopedPermissions {
		if !permPattern.MatchString(perm) {
			return errors.New("invalid scopedPermissions entry: " + perm + " (must match ^[A-Z][A-Z0-9-]*:[A-Z]+$)")
		}
	}
	return nil
}


// APITokensResponse is the list response wrapper for GET /user/api_tokens.
type APITokensResponse struct {
Alerts
Response []APITokenResponse `json:"response"`
}

// APITokenSingleResponse is the single-item response wrapper for GET /user/api_tokens/{id}.
type APITokenSingleResponse struct {
Alerts
Response APITokenResponse `json:"response"`
}

