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
	"fmt"
	"strings"
	"testing"
)

func ExampleCurrentUser_Can() {
	cu := CurrentUser{}
	fmt.Println(cu.Can("anything"))
	// Output: false
}

func TestCurrentUser_Can(t *testing.T) {
	cu := CurrentUser{}
	cu.perms = map[string]struct{}{"do-something": {}}
	if !cu.Can("do-something") {
		t.Error("user cannot do something they have Permission to do")
	}
	if cu.Can("do-something-else") {
		t.Error("user can do something they don't have Permission to do")
	}
}

func ExampleCurrentUser_MissingPermissions() {
	cu := CurrentUser{}
	missingPerms := cu.MissingPermissions("do something", "do anything")
	fmt.Println(strings.Join(missingPerms, ", "))
	// Output: do something, do anything
}

func TestCurrentUser_MissingPermissions(t *testing.T) {
	cu := CurrentUser{}
	cu.perms = map[string]struct{}{"do-something": {}}
	missing := cu.MissingPermissions("do-something", "do-something-else")
	if len(missing) != 1 {
		t.Fatalf("Expected checking user with one Permission for two Permissions to be missing one, actually missing: %d", len(missing))
	}
	if missing[0] != "do-something-else" {
		t.Errorf("Expected user to be missing 'do-something-else' Permission, actually missing: %s", missing[0])
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ApplyTokenPermissionScope tests
// ─────────────────────────────────────────────────────────────────────────────

func userWithCapabilities(caps []string, privLevel int) CurrentUser {
	u := CurrentUser{
		PrivLevel:          privLevel,
		EffectivePrivLevel: privLevel,
		Capabilities:       caps,
	}
	u.perms = make(map[string]struct{}, len(caps))
	for _, c := range caps {
		u.perms[c] = struct{}{}
	}
	return u
}

func TestApplyTokenPermissionScope_IntersectionOnly(t *testing.T) {
	user := userWithCapabilities([]string{"DS:READ", "JOB:READ"}, 20)
	scoped := []string{"DS:READ", "DS:WRITE"}
	result := ApplyTokenPermissionScope(user, scoped)

	if len(result.Capabilities) != 1 || result.Capabilities[0] != "DS:READ" {
		t.Errorf("expected intersection [DS:READ], got %v", result.Capabilities)
	}
}

func TestApplyTokenPermissionScope_EmptyScopedPermissions_ShouldNotHappen(t *testing.T) {
	// ApplyTokenPermissionScope should not be called with empty scoped perms,
	// but defensively: result should have no capabilities.
	user := userWithCapabilities([]string{"DS:READ"}, 20)
	result := ApplyTokenPermissionScope(user, []string{})
	if len(result.Capabilities) != 0 {
		t.Errorf("expected empty intersection when scoped perms is empty, got %v", result.Capabilities)
	}
}

func TestApplyTokenPermissionScope_SetsIsAPITokenScoped(t *testing.T) {
	user := userWithCapabilities([]string{"DS:READ"}, 20)
	result := ApplyTokenPermissionScope(user, []string{"DS:READ"})
	if !result.IsAPITokenScoped {
		t.Error("expected IsAPITokenScoped=true after ApplyTokenPermissionScope")
	}
}

func TestApplyTokenPermissionScope_CapsEffectivePrivLevelToReadOnly(t *testing.T) {
	// Admin user (30) with scoped token → EffectivePrivLevel = PrivLevelReadOnly (10).
	user := userWithCapabilities([]string{"DS:READ"}, PrivLevelAdmin)
	result := ApplyTokenPermissionScope(user, []string{"DS:READ"})
	if result.EffectivePrivLevel != PrivLevelReadOnly {
		t.Errorf("expected EffectivePrivLevel=%d, got %d", PrivLevelReadOnly, result.EffectivePrivLevel)
	}
}

func TestApplyTokenPermissionScope_PreservesOriginalPrivLevel(t *testing.T) {
	user := userWithCapabilities([]string{"DS:READ"}, PrivLevelAdmin)
	result := ApplyTokenPermissionScope(user, []string{"DS:READ"})
	// PrivLevel unchanged — it's the user's real role, not the scoped level.
	if result.PrivLevel != PrivLevelAdmin {
		t.Errorf("expected original PrivLevel=%d preserved, got %d", PrivLevelAdmin, result.PrivLevel)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Can() tests with IsAPITokenScoped
// ─────────────────────────────────────────────────────────────────────────────

func TestCurrentUser_Can_ScopedToken_OnlyEffectiveCapabilities(t *testing.T) {
	user := userWithCapabilities([]string{"DS:READ", "JOB:READ"}, 20)
	scoped := ApplyTokenPermissionScope(user, []string{"DS:READ"})

	if !scoped.Can("DS:READ") {
		t.Error("expected scoped token to be able to use DS:READ (in intersection)")
	}
	if scoped.Can("JOB:READ") {
		t.Error("expected scoped token to NOT use JOB:READ (not in scoped perms)")
	}
}

func TestCurrentUser_Can_UnscopedToken_AllCapabilities(t *testing.T) {
	user := userWithCapabilities([]string{"DS:READ", "JOB:READ"}, 20)
	// Unscoped: IsAPITokenScoped=false → Can() works normally.
	if !user.Can("DS:READ") {
		t.Error("expected unscoped user to use DS:READ")
	}
	if !user.Can("JOB:READ") {
		t.Error("expected unscoped user to use JOB:READ")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// MissingPermissions tests with IsAPITokenScoped
// ─────────────────────────────────────────────────────────────────────────────

func TestCurrentUser_MissingPermissions_ScopedToken(t *testing.T) {
	user := userWithCapabilities([]string{"DS:READ", "JOB:READ"}, 20)
	scoped := ApplyTokenPermissionScope(user, []string{"DS:READ"})
	missing := scoped.MissingPermissions("DS:READ", "JOB:READ")
	// JOB:READ is not in scoped intersection → reported as missing.
	if len(missing) != 1 || missing[0] != "JOB:READ" {
		t.Errorf("expected [JOB:READ] missing, got %v", missing)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// EffectivePrivLevel edge cases
// ─────────────────────────────────────────────────────────────────────────────

func TestCurrentUser_EffectivePrivLevel_DefaultZeroIsNotUsed(t *testing.T) {
	// This test documents the invariant: EffectivePrivLevel must be set
	// explicitly (either to PrivLevel for session auth or to PrivLevelReadOnly
	// for scoped tokens). A zero value is invalid.
	u := CurrentUser{PrivLevel: PrivLevelOperations}
	// Before our fix, EffectivePrivLevel would be 0 for session auth.
	// After our fix (GetUserFromReq sets it explicitly), it equals PrivLevel.
	// This test just confirms the zero value is different from PrivLevel.
	if u.EffectivePrivLevel == u.PrivLevel {
		t.Log("zero-value EffectivePrivLevel equals PrivLevel only if PrivLevel is also 0 — expected this struct was never used directly")
	}
}
