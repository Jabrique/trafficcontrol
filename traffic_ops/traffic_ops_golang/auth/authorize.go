package auth

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
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_ops/traffic_ops_golang/config"

	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
)

type CurrentUser struct {
	UserName     string         `json:"userName" db:"username"`
	ID           int            `json:"id" db:"id"`
	PrivLevel    int            `json:"privLevel" db:"priv_level"`
	TenantID     int            `json:"tenantId" db:"tenant_id"`
	Role         int            `json:"role" db:"role"`
	RoleName     string         `json:"roleName" db:"role_name"`
	Capabilities pq.StringArray `json:"capabilities" db:"capabilities"`
	UCDN         string         `json:"ucdn" db:"ucdn"`
	perms        map[string]struct{}

	// IsAPITokenScoped is true when the request is authenticated via a scoped API token.
	// When true:
	//   1. Admin bypass in Can() and MissingPermissions() is disabled.
	//   2. EffectivePrivLevel is capped at PrivLevelReadOnly (10).
	// Always false for cookie/session auth and unscoped API tokens.
	IsAPITokenScoped bool `json:"-" db:"-"`

	// EffectivePrivLevel is the privilege level used for endpoint authorization.
	// For scoped API tokens: always PrivLevelReadOnly (10), regardless of actual role.
	// For all other auth: equals PrivLevel.
	// This prevents a scoped admin token from bypassing PrivLevel-gated endpoints.
	EffectivePrivLevel int `json:"-" db:"-"`
}

// Can returns whether or not the user has the specified Permission, i.e.
// whether or not they "can" do something.
// Admin bypass is disabled for scoped API tokens (IsAPITokenScoped=true) —
// the scope must be explicitly checked.
func (cu CurrentUser) Can(permission string) bool {
	if cu.RoleName == tc.AdminRoleName && !cu.IsAPITokenScoped {
		return true
	}
	_, ok := cu.perms[permission]
	return ok
}

// MissingPermissions returns all of the passed Permissions that the user does
// not have. Admin bypass is disabled for scoped API tokens (IsAPITokenScoped=true).
func (cu CurrentUser) MissingPermissions(permissions ...string) []string {
	var ret []string
	if cu.RoleName == tc.AdminRoleName && !cu.IsAPITokenScoped {
		return ret
	}
	for _, perm := range permissions {
		if _, ok := cu.perms[perm]; !ok {
			ret = append(ret, perm)
		}
	}
	return ret
}

type PasswordForm struct {
	Username string `json:"u"`
	Password string `json:"p"`
}

const (
	// DisallowedRoleName is the role name for disabled/blocked users.
	// Exported so api package can reference it in authenticateAPIToken.
	// Replaces the unexported `disallowed` constant below.
	DisallowedRoleName = "disallowed"

	// disallowed is kept for internal backward compatibility within this file.
	// All new code outside this package must use DisallowedRoleName.
	disallowed = DisallowedRoleName
)

// PrivLevelInvalid - The Default Priv level
const PrivLevelInvalid = -1

const PrivLevelUnauthenticated = 0

const PrivLevelReadOnly = 10

const PrivLevelSteering = 15

const PrivLevelFederation = 15

const PrivLevelPortal = 15

const PrivLevelOperations = 20

const PrivLevelAdmin = 30

// TenantIDInvalid - The default Tenant ID
const TenantIDInvalid = -1

type key int

const CurrentUserKey key = iota

// GetCurrentUserFromDB  - returns the id and privilege level of the given user along with the username, or -1 as the id, - as the userName and PrivLevelInvalid if the user doesn't exist, along with a user facing error, a system error to log, and an error code to return
func GetCurrentUserFromDB(DB *sqlx.DB, user string, timeout time.Duration) (CurrentUser, error, error, int) {
	invalidUser := CurrentUser{UserName: "-", ID: -1, PrivLevel: PrivLevelInvalid, TenantID: TenantIDInvalid, Role: -1}
	if usersCacheIsEnabled() {
		u, exists := getUserFromCache(user)
		if !exists {
			return invalidUser, errors.New("user not found"), fmt.Errorf("checking user '%s' info: user not in cache", user), http.StatusUnauthorized
		}
		return u.CurrentUser, nil, nil, http.StatusOK
	}
	qry := `
SELECT
  r.priv_level,
  r.id as role,
  r.name as role_name, 
  u.id,
  u.username,
  u.tenant_id,
  ARRAY(SELECT rc.cap_name FROM role_capability AS rc WHERE rc.role_id=r.id) AS capabilities,
  u.ucdn
FROM
  tm_user AS u
JOIN
  role AS r ON u.role = r.id
WHERE
  u.username = $1
`

	var currentUserInfo CurrentUser
	if DB == nil {
		return CurrentUser{UserName: "-", ID: -1, PrivLevel: PrivLevelInvalid, TenantID: TenantIDInvalid, Role: -1}, nil, errors.New("no db provided to GetCurrentUserFromDB"), http.StatusInternalServerError
	}
	dbCtx, dbClose := context.WithTimeout(context.Background(), timeout)
	defer dbClose()

	err := DB.GetContext(dbCtx, &currentUserInfo, qry, user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invalidUser, errors.New("user not found"), fmt.Errorf("checking user %v info: user not in database", user), http.StatusUnauthorized
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return invalidUser, nil, fmt.Errorf("db access timed out: %w number of open connections: %d", err, DB.Stats().OpenConnections), http.StatusServiceUnavailable
		}
		return invalidUser, nil, fmt.Errorf("checking user %v info: %w", user, err), http.StatusInternalServerError
	}

	currentUserInfo.perms = make(map[string]struct{}, len(currentUserInfo.Capabilities))
	for _, perm := range currentUserInfo.Capabilities {
		currentUserInfo.perms[perm] = struct{}{}
	}
	return currentUserInfo, nil, nil, http.StatusOK
}

func GetCurrentUser(ctx context.Context) (*CurrentUser, error) {
	val := ctx.Value(CurrentUserKey)
	if val != nil {
		switch v := val.(type) {
		case CurrentUser:
			return &v, nil
		default:
			return nil, fmt.Errorf("CurrentUser found with bad type: %T", v)
		}
	}
	return &CurrentUser{UserName: "-", ID: -1, PrivLevel: PrivLevelInvalid, TenantID: TenantIDInvalid, Role: -1}, errors.New("No user found in Context")
}

func CheckLocalUserIsAllowed(username string, db *sqlx.DB, ctx context.Context) (bool, error, error) {
	if usersCacheIsEnabled() {
		u, exists := getUserFromCache(username)
		if !exists {
			return false, fmt.Errorf("user '%s' not found in cache", username), nil
		}
		allowed := u.RoleName != disallowed
		return allowed, nil, nil
	}
	var roleName string

	err := db.GetContext(ctx, &roleName, "SELECT role.name FROM role INNER JOIN tm_user ON tm_user.role = role.id where username=$1", username)
	if err != nil {
		if err == context.DeadlineExceeded || err == context.Canceled {
			return false, nil, err
		}
		return false, err, nil
	}
	if roleName != "" {
		if roleName != disallowed { //relies on unchanging role name assumption.
			return true, nil, nil
		}
	}
	return false, nil, nil
}

// GetUserUcdn returns the Upstream CDN to which the user belongs for CDNi operations.
func GetUserUcdn(form PasswordForm, db *sqlx.DB, ctx context.Context) (string, error) {
	if usersCacheIsEnabled() {
		u, exists := getUserFromCache(form.Username)
		if !exists {
			return "", fmt.Errorf("user '%s' not found in cache", form.Username)
		}
		return u.UCDN, nil
	}
	var ucdn string

	err := db.GetContext(ctx, &ucdn, "SELECT ucdn FROM tm_user where username=$1", form.Username)
	if err != nil {
		return "", err
	}

	return ucdn, nil
}

func CheckLocalUserPassword(form PasswordForm, db *sqlx.DB, ctx context.Context) (bool, error, error) {
	var hashedPassword string
	if usersCacheIsEnabled() {
		u, exists := getUserFromCache(form.Username)
		if !exists {
			return false, fmt.Errorf("user '%s' not found in cache", form.Username), nil
		}
		if u.LocalPasswd == nil {
			return false, nil, nil
		}
		hashedPassword = *u.LocalPasswd
	} else {
		err := db.GetContext(ctx, &hashedPassword, "SELECT local_passwd FROM tm_user WHERE username=$1", form.Username)
		if err != nil {
			if err == context.DeadlineExceeded || err == context.Canceled {
				return false, nil, err
			}
			return false, err, nil
		}
	}

	err := VerifySCRYPTPassword(form.Password, hashedPassword)
	if err != nil {
		hashedInput, err := sha1Hex(form.Password)
		if err != nil {
			return false, err, nil
		}
		if hashedPassword == hashedInput { // for backwards compatibility
			return true, nil, nil
		}
		return false, err, nil
	}
	return true, nil, nil
}

// CheckLocalUserToken checks the passed token against the records in the db for a match, up to a
// maximum duration of timeout.
func CheckLocalUserToken(token string, db *sqlx.DB, timeout time.Duration) (bool, string, error) {
	if usersCacheIsEnabled() {
		username, matched := getUserNameFromCacheByToken(token)
		return matched, username, nil
	}
	dbCtx, dbClose := context.WithTimeout(context.Background(), timeout)
	defer dbClose()

	var username string
	err := db.GetContext(dbCtx, &username, `SELECT username FROM tm_user WHERE token=$1 AND role!=(SELECT role.id FROM role WHERE role.name=$2)`, token, disallowed)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		}
		return false, "", err
	}
	return true, username, nil
}

func sha1Hex(s string) (string, error) {
	// SHA1 hash
	hash := sha1.New()
	if _, err := hash.Write([]byte(s)); err != nil {
		return "", err
	}
	hashBytes := hash.Sum(nil)
	hexSha1 := hex.EncodeToString(hashBytes)
	return hexSha1, nil
}

func CheckLDAPUser(form PasswordForm, cfg *config.ConfigLDAP) (bool, error) {
	userDN, valid, err := LookupUserDN(form.Username, cfg)
	if err != nil {
		return false, err
	}
	if valid {
		return AuthenticateUserDN(userDN, form.Password, cfg)
	}
	return false, errors.New("User not found in LDAP")
}

// ApplyTokenPermissionScope restricts a CurrentUser's capabilities to only those
// present in both the token's scoped_permissions AND the user's current role capabilities.
//
// This function MUST live in the auth package because `perms` is an unexported field.
// It is called during API token authentication when the token has scoped_permissions set.
//
// Effect:
//   - Capabilities = intersection of scopedPerms ∩ user.Capabilities
//   - perms map updated to match
//   - IsAPITokenScoped = true (disables admin bypass in Can() / MissingPermissions())
//   - EffectivePrivLevel = PrivLevelReadOnly (10) — prevents scoped admin token from
//     bypassing PrivLevel-gated endpoints regardless of actual role
func ApplyTokenPermissionScope(user CurrentUser, scopedPerms []string) CurrentUser {
	scopeSet := make(map[string]struct{}, len(scopedPerms))
	for _, p := range scopedPerms {
		scopeSet[p] = struct{}{}
	}

	var effective []string
	effectiveMap := make(map[string]struct{})
	for _, cap := range user.Capabilities {
		if _, ok := scopeSet[cap]; ok {
			effective = append(effective, cap)
			effectiveMap[cap] = struct{}{}
		}
	}

	user.Capabilities = effective
	user.perms = effectiveMap
	user.IsAPITokenScoped = true
	// Cap privilege level at ReadOnly — scoped tokens cannot escalate beyond their scope
	// even if the underlying user has PrivLevelOperations or PrivLevelAdmin.
	user.EffectivePrivLevel = PrivLevelReadOnly
	return user
}

// GetCurrentUserFromDBDirect always queries the database directly, bypassing the in-memory
// user cache. This is used for API token authentication so that role changes (e.g. role
// downgrade after token issuance) take effect immediately on the next request.
//
// Cookie session auth uses GetCurrentUserFromDB (may use cache) for performance.
// API token auth uses this function for correctness.
func GetCurrentUserFromDBDirect(DB *sqlx.DB, user string, timeout time.Duration) (CurrentUser, error, error, int) {
	// Named-field init: safe against future struct field additions.
	invalidUser := CurrentUser{UserName: "-", ID: -1, PrivLevel: PrivLevelInvalid, TenantID: TenantIDInvalid, Role: -1}

	// Query is copied verbatim from GetCurrentUserFromDB.
	// Join directly against tm_user → role → role_capability: no materialized view,
	// no caching layer. Capabilities are always fresh from the database.
	qry := `
SELECT
  r.priv_level,
  r.id as role,
  r.name as role_name,
  u.id,
  u.username,
  u.tenant_id,
  ARRAY(SELECT rc.cap_name FROM role_capability AS rc WHERE rc.role_id=r.id) AS capabilities,
  u.ucdn
FROM
  tm_user AS u
JOIN
  role AS r ON u.role = r.id
WHERE
  u.username = $1
`
	dbCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var currentUser CurrentUser
	err := DB.GetContext(dbCtx, &currentUser, qry, user)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return invalidUser, errors.New("no such user"), nil, http.StatusUnauthorized
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return invalidUser, nil, fmt.Errorf("db access timed out: %w", err), http.StatusServiceUnavailable
		}
		return invalidUser, nil, fmt.Errorf("querying user %s: %w", user, err), http.StatusInternalServerError
	}

	// Populate the unexported perms map — identical to GetCurrentUserFromDB.
	currentUser.perms = make(map[string]struct{}, len(currentUser.Capabilities))
	for _, perm := range currentUser.Capabilities {
		currentUser.perms[perm] = struct{}{}
	}
	return currentUser, nil, nil, http.StatusOK
}
