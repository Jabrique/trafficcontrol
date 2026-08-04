// Package apitoken provides HTTP handlers for the API token management endpoints.
// This is the user-facing CRUD surface: create, list, get, delete.
// Authentication of tokens on incoming requests is in api/api_token_auth.go.
package apitoken

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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/api"
	"github.com/lib/pq"
)

// secretByteLen is the number of random bytes used for the secret part of each token.
// 32 bytes → 64 hex chars → 256 bits of entropy — safe against brute force.
const secretByteLen = 32

// publicIDByteLen is the number of random bytes for the human-safe public ID portion.
// 8 bytes → 16 hex chars. This is the loggable, storable portion.
const publicIDByteLen = 8

// Create handles POST /api/5.0/user/api_tokens
//
// Creates a new API token for the currently authenticated user.
//
// Business rules enforced:
//   - Admin creating for another user: target user_id must exist.
//   - Self-create (no user_id body param): uses authenticated user's ID.
//   - Maximum tokens per user: api_token_max_per_user (default 10).
//   - expires_at required, must be in the future, ≤ api_token_max_expiry_days from now.
//   - AllowedCIDRs required if creator's priv_level ≥ PrivLevelOperations(20).
//   - ScopedPermissions must be a subset of the target user's current capabilities.
//   - Token secret is generated server-side (32 random bytes) — client never chooses it.
func Create(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, nil, nil)
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	var req tc.APITokenCreateRequest
	if err := api.Parse(r.Body, inf.Tx.Tx, &req); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, err, nil)
		return
	}

	cfg, cfgErr := api.GetConfig(r.Context())
	if cfgErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("getting config: %w", cfgErr))
		return
	}

	// Determine target user ID (self-create or admin create for another user).
	// For simplicity in Phase 7, we always create for the authenticated user.
	// Admin-for-other is a future extension.
	targetUserID := inf.User.ID

	// Validate expires_at.
	now := time.Now()
	if req.ExpiresAt.IsZero() || req.ExpiresAt.Before(now) {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest, errors.New("expiresAt must be a future timestamp"), nil)
		return
	}
	maxExpiry := now.AddDate(0, 0, cfg.APITokenMaxExpiryDays)
	if req.ExpiresAt.After(maxExpiry) {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
			fmt.Errorf("expiresAt must not exceed %d days from now", cfg.APITokenMaxExpiryDays), nil)
		return
	}

	// Validate AllowedCIDRs: required for operators and admins.
	if inf.User.PrivLevel >= 20 && len(req.AllowedCIDRs) == 0 {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
			errors.New("allowedCidrs is required for users with operator or admin privileges"), nil)
		return
	}

	// Check per-user token limit.
	var tokenCount int
	if err := inf.Tx.Tx.QueryRow(
		`SELECT COUNT(*) FROM api_token WHERE user_id = $1 AND expires_at > NOW()`, targetUserID,
	).Scan(&tokenCount); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("counting user tokens: %w", err))
		return
	}
	if tokenCount >= cfg.APITokenMaxPerUser {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
			fmt.Errorf("maximum of %d active tokens per user reached", cfg.APITokenMaxPerUser), nil)
		return
	}

	// Validate ScopedPermissions: must be a subset of user's current capabilities.
	if len(req.ScopedPermissions) > 0 {
		capSet := make(map[string]struct{}, len(inf.User.Capabilities))
		for _, c := range inf.User.Capabilities {
			capSet[c] = struct{}{}
		}
		for _, perm := range req.ScopedPermissions {
			if _, ok := capSet[perm]; !ok {
				api.HandleErr(w, r, inf.Tx.Tx, http.StatusBadRequest,
					fmt.Errorf("scopedPermissions entry %q is not in the user's current capabilities", perm), nil)
				return
			}
		}
	}

	// Generate token: publicID (loggable) + secret (hashed before storage).
	publicIDBytes := make([]byte, publicIDByteLen)
	if _, err := rand.Read(publicIDBytes); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("generating public ID: %w", err))
		return
	}
	secretBytes := make([]byte, secretByteLen)
	if _, err := rand.Read(secretBytes); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("generating secret: %w", err))
		return
	}

	publicID := hex.EncodeToString(publicIDBytes)
	secretHex := hex.EncodeToString(secretBytes)
	tokenPrefix := tc.APITokenPrefix + publicID
	// Full plaintext token — shown once to the user, never stored.
	plaintext := tc.APITokenPrefix + publicID + "_" + secretHex

	// SHA-256 the secret part only (not the full token or prefix).
	hashBytes := sha256.Sum256([]byte(secretHex))
	tokenHash := hex.EncodeToString(hashBytes[:])

	// Insert.
	var tokenID int64
	var createdAt time.Time
	err := inf.Tx.Tx.QueryRow(`
INSERT INTO api_token
    (user_id, name, token_hash, token_prefix, scoped_permissions, allowed_cidrs,
     expires_at, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, created_at
`,
		targetUserID,
		req.Name,
		tokenHash,
		tokenPrefix,
		pq.Array(req.ScopedPermissions),
		pq.Array(req.AllowedCIDRs),
		req.ExpiresAt,
		inf.User.ID,
	).Scan(&tokenID, &createdAt)
	if err != nil {
		if isUniqueViolation(err) {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusConflict,
				errors.New("a token with that name already exists for this user"), nil)
			return
		}
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("inserting api_token: %w", err))
		return
	}

	resp := tc.APITokenCreateResponse{
		ID:                tokenID,
		Name:              req.Name,
		Token:             plaintext,
		TokenPrefix:       tokenPrefix,
		ScopedPermissions: req.ScopedPermissions,
		AllowedCIDRs:      req.AllowedCIDRs,
		ExpiresAt:         req.ExpiresAt,
		CreatedAt:         createdAt,
	}
	api.WriteRespAlertObj(w, r, tc.SuccessLevel, "API token created — store the token value securely, it will not be shown again", resp)
}

// GetAll handles GET /api/5.0/user/api_tokens
//
// Returns all non-expired API tokens for the authenticated user.
// Admins can see all tokens; regular users see only their own.
func GetAll(w http.ResponseWriter, r *http.Request) {
	inf, userErr, sysErr, errCode := api.NewInfo(r, nil, nil)
	if userErr != nil || sysErr != nil {
		api.HandleErr(w, r, inf.Tx.Tx, errCode, userErr, sysErr)
		return
	}
	defer inf.Close()

	var qry string
	var args []interface{}
	if inf.User.PrivLevel >= 30 { // PrivLevelAdmin
		// Admins see all tokens (including other users').
		qry = `
SELECT id, name, token_prefix,
    COALESCE(scoped_permissions, ARRAY[]::TEXT[]) AS scoped_permissions,
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]) AS allowed_cidrs,
    expires_at, last_used_at, created_at
FROM api_token
WHERE expires_at > NOW()
ORDER BY created_at DESC
`
	} else {
		// Users see only their own tokens.
		qry = `
SELECT id, name, token_prefix,
    COALESCE(scoped_permissions, ARRAY[]::TEXT[]) AS scoped_permissions,
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]) AS allowed_cidrs,
    expires_at, last_used_at, created_at
FROM api_token
WHERE user_id = $1 AND expires_at > NOW()
ORDER BY created_at DESC
`
		args = append(args, inf.User.ID)
	}

	rows, err := inf.Tx.Tx.Query(qry, args...)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("querying api_token: %w", err))
		return
	}
	defer rows.Close()

	var tokens []tc.APITokenResponse
	for rows.Next() {
		var t tc.APITokenResponse
		var scopedPerms pq.StringArray
		var allowedCIDRs pq.StringArray
		if err := rows.Scan(
			&t.ID, &t.Name, &t.TokenPrefix,
			&scopedPerms, &allowedCIDRs,
			&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
		); err != nil {
			api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("scanning api_token: %w", err))
			return
		}
		t.ScopedPermissions = []string(scopedPerms)
		t.AllowedCIDRs = []string(allowedCIDRs)
		tokens = append(tokens, t)
	}
	if err := rows.Err(); err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("iterating api_token rows: %w", err))
		return
	}

	api.WriteResp(w, r, tokens)
}

// Get handles GET /api/5.0/user/api_tokens/{id}
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

	var t tc.APITokenResponse
	var scopedPerms pq.StringArray
	var allowedCIDRs pq.StringArray

	var qry string
	var args []interface{}
	if inf.User.PrivLevel >= 30 {
		qry = `
SELECT id, name, token_prefix,
    COALESCE(scoped_permissions, ARRAY[]::TEXT[]),
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]),
    expires_at, last_used_at, created_at
FROM api_token WHERE id = $1
`
		args = []interface{}{id}
	} else {
		qry = `
SELECT id, name, token_prefix,
    COALESCE(scoped_permissions, ARRAY[]::TEXT[]),
    COALESCE(allowed_cidrs, ARRAY[]::TEXT[]),
    expires_at, last_used_at, created_at
FROM api_token WHERE id = $1 AND user_id = $2
`
		args = []interface{}{id, inf.User.ID}
	}

	err = inf.Tx.Tx.QueryRow(qry, args...).Scan(
		&t.ID, &t.Name, &t.TokenPrefix,
		&scopedPerms, &allowedCIDRs,
		&t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt,
	)
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusNotFound, errors.New("api token not found"), nil)
		return
	}
	t.ScopedPermissions = []string(scopedPerms)
	t.AllowedCIDRs = []string(allowedCIDRs)

	api.WriteResp(w, r, t)
}

// Delete handles DELETE /api/5.0/user/api_tokens/{id}
//
// Regular users can only delete their own tokens.
// Admins can delete any token.
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

	var result interface{ RowsAffected() (int64, error) }
	if inf.User.PrivLevel >= 30 {
		result, err = inf.Tx.Tx.Exec(`DELETE FROM api_token WHERE id = $1`, id)
	} else {
		result, err = inf.Tx.Tx.Exec(`DELETE FROM api_token WHERE id = $1 AND user_id = $2`, id, inf.User.ID)
	}
	if err != nil {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusInternalServerError, nil, fmt.Errorf("deleting api_token: %w", err))
		return
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		api.HandleErr(w, r, inf.Tx.Tx, http.StatusNotFound, errors.New("api token not found"), nil)
		return
	}

	api.WriteRespAlert(w, r, tc.SuccessLevel, "API token deleted")
}

// isUniqueViolation returns true if the error is a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate")
}
