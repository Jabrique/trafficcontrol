package router_health

/*
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

import (
	"strings"
	"testing"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
)

// -- shouldSetOffline --

func TestShouldSetOffline(t *testing.T) {
	tests := []struct {
		name           string
		downCount      int
		threshold      int
		alreadyManaged bool
		currentStatus  string
		want           bool
	}{
		{
			name:           "threshold met, REPORTED, not already managed",
			downCount:      1,
			threshold:      1,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusReported),
			want:           true,
		},
		{
			name:           "threshold not met (0 < 1)",
			downCount:      0,
			threshold:      1,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusReported),
			want:           false,
		},
		{
			name:           "already managed by watcher",
			downCount:      1,
			threshold:      1,
			alreadyManaged: true,
			currentStatus:  string(tc.CacheStatusReported),
			want:           false,
		},
		{
			name:           "status is ONLINE, not touchable",
			downCount:      1,
			threshold:      1,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusOnline),
			want:           false,
		},
		{
			name:           "status is OFFLINE (operator-set), not touchable",
			downCount:      1,
			threshold:      1,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusOffline),
			want:           false,
		},
		{
			name:           "status is ADMIN_DOWN, not touchable",
			downCount:      1,
			threshold:      1,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusAdminDown),
			want:           false,
		},
		{
			name:           "higher threshold, count below",
			downCount:      2,
			threshold:      3,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusReported),
			want:           false,
		},
		{
			name:           "higher threshold, count exactly met",
			downCount:      3,
			threshold:      3,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusReported),
			want:           true,
		},
		{
			name:           "count exceeds threshold",
			downCount:      5,
			threshold:      3,
			alreadyManaged: false,
			currentStatus:  string(tc.CacheStatusReported),
			want:           true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSetOffline(tt.downCount, tt.threshold, tt.alreadyManaged, tt.currentStatus)
			if got != tt.want {
				t.Errorf("shouldSetOffline(%d, %d, %v, %q) = %v, want %v",
					tt.downCount, tt.threshold, tt.alreadyManaged, tt.currentStatus, got, tt.want)
			}
		})
	}
}

// -- shouldRestore --

func TestShouldRestore(t *testing.T) {
	tests := []struct {
		name          string
		upCount       int
		threshold     int
		hasAutoTag    bool
		currentStatus string
		want          bool
	}{
		{
			name:          "threshold met, OFFLINE, has auto tag",
			upCount:       2,
			threshold:     2,
			hasAutoTag:    true,
			currentStatus: string(tc.CacheStatusOffline),
			want:          true,
		},
		{
			name:          "threshold not met (1 < 2)",
			upCount:       1,
			threshold:     2,
			hasAutoTag:    true,
			currentStatus: string(tc.CacheStatusOffline),
			want:          false,
		},
		{
			name:          "auto tag cleared by operator",
			upCount:       2,
			threshold:     2,
			hasAutoTag:    false,
			currentStatus: string(tc.CacheStatusOffline),
			want:          false,
		},
		{
			name:          "status changed by operator to REPORTED",
			upCount:       2,
			threshold:     2,
			hasAutoTag:    true,
			currentStatus: string(tc.CacheStatusReported),
			want:          false,
		},
		{
			name:          "status changed by operator to ADMIN_DOWN",
			upCount:       2,
			threshold:     2,
			hasAutoTag:    true,
			currentStatus: string(tc.CacheStatusAdminDown),
			want:          false,
		},
		{
			name:          "high threshold, count exactly met",
			upCount:       5,
			threshold:     5,
			hasAutoTag:    true,
			currentStatus: string(tc.CacheStatusOffline),
			want:          true,
		},
		{
			name:          "count exceeds threshold",
			upCount:       10,
			threshold:     2,
			hasAutoTag:    true,
			currentStatus: string(tc.CacheStatusOffline),
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRestore(tt.upCount, tt.threshold, tt.hasAutoTag, tt.currentStatus)
			if got != tt.want {
				t.Errorf("shouldRestore(%d, %d, %v, %q) = %v, want %v",
					tt.upCount, tt.threshold, tt.hasAutoTag, tt.currentStatus, got, tt.want)
			}
		})
	}
}


// -- autoOffline constants --

// TestAutoOfflineReasonHasPrefix guards the contract that the DB query for
// repopulation (LIKE '[auto-tm]:%') will match what we write into offline_reason.
// If someone changes autoOfflineReason without updating autoOfflinePrefix, the
// startup repopulation query breaks silently.
func TestAutoOfflineReasonHasPrefix(t *testing.T) {
	if !strings.HasPrefix(autoOfflineReason, autoOfflinePrefix) {
		t.Errorf("autoOfflineReason %q does not start with prefix %q -- "+
			"the DB startup query (LIKE '[auto-tm]:%%') will no longer match "+
			"offline_reason values set by this watcher", autoOfflineReason, autoOfflinePrefix)
	}
}

// TestAutoOfflinePrefixNonEmpty guards the LIKE pattern: an empty prefix would
// match every offline_reason in the database, causing all OFFLINE servers to be
// treated as auto-managed on restart.
func TestAutoOfflinePrefixNonEmpty(t *testing.T) {
	if autoOfflinePrefix == "" {
		t.Fatal("autoOfflinePrefix is empty: startup repopulation query " +
			"'LIKE [auto-tm]:%' would match every OFFLINE server")
	}
}

// -- Default thresholds and intervals --

// TestDefaultValues pins the default configuration values so that a careless
// change to the constants does not go unnoticed. Each value has been chosen
// deliberately and changing them has operational consequences.
func TestDefaultValues(t *testing.T) {
	if defaultDownThreshold != 1 {
		t.Errorf("defaultDownThreshold = %d, want 1: "+
			"TM already applies quorum before reporting unavailability, "+
			"so a single reading is already conservative enough", defaultDownThreshold)
	}
	if defaultUpThreshold != 2 {
		t.Errorf("defaultUpThreshold = %d, want 2: "+
			"recovery must require at least two consecutive healthy readings "+
			"to avoid restoring a flapping router prematurely", defaultUpThreshold)
	}
	if defaultPollInterval != 5*time.Second {
		t.Errorf("defaultPollInterval = %v, want 5s", defaultPollInterval)
	}
}

// -- Decision function edge cases and boundary conditions --

// TestShouldSetOffline_ZeroThreshold verifies that a threshold of zero always
// triggers the action regardless of count, since 0 >= 0 is always true.
// This is an edge case that should not appear in production but must be
// understood if someone sets downThreshold=0 via config.
func TestShouldSetOffline_ZeroThreshold(t *testing.T) {
	got := shouldSetOffline(0, 0, false, string(tc.CacheStatusReported))
	if !got {
		t.Error("shouldSetOffline(0, 0, false, REPORTED) = false, want true: " +
			"count=0 >= threshold=0 must be true")
	}
}

// TestShouldSetOffline_IdempotentOnceManaged verifies that after a router has
// been marked as auto-managed (autoOffline=true), further down polls never
// re-trigger shouldSetOffline. This is the guard against double-OFFLINE calls.
func TestShouldSetOffline_IdempotentOnceManaged(t *testing.T) {
	for extraDownPolls := 1; extraDownPolls <= 10; extraDownPolls++ {
		got := shouldSetOffline(extraDownPolls, 1, true, string(tc.CacheStatusReported))
		if got {
			t.Errorf("shouldSetOffline(%d, 1, alreadyManaged=true, REPORTED) = true, "+
				"want false: already-managed router must never be re-OFFLINE'd", extraDownPolls)
		}
	}
}

// TestShouldRestore_RequiresBothTagAndStatus verifies that shouldRestore only
// returns true when BOTH conditions hold simultaneously: status=OFFLINE AND
// hasAutoTag=true. Failing either one independently must return false.
func TestShouldRestore_RequiresBothTagAndStatus(t *testing.T) {
	// Tag present but status wrong.
	if shouldRestore(2, 2, true, string(tc.CacheStatusReported)) {
		t.Error("shouldRestore with hasAutoTag=true but status=REPORTED should be false: " +
			"operator may have already restored the server, watcher must not double-act")
	}
	// Status correct but tag missing.
	if shouldRestore(2, 2, false, string(tc.CacheStatusOffline)) {
		t.Error("shouldRestore with status=OFFLINE but hasAutoTag=false should be false: " +
			"tag is the only reliable way to distinguish auto-managed from operator-set OFFLINE")
	}
	// Both correct.
	if !shouldRestore(2, 2, true, string(tc.CacheStatusOffline)) {
		t.Error("shouldRestore with hasAutoTag=true and status=OFFLINE should be true")
	}
}

// TestShouldRestore_ZeroThreshold mirrors TestShouldSetOffline_ZeroThreshold.
func TestShouldRestore_ZeroThreshold(t *testing.T) {
	got := shouldRestore(0, 0, true, string(tc.CacheStatusOffline))
	if !got {
		t.Error("shouldRestore(0, 0, hasTag=true, OFFLINE) = false, want true")
	}
}

// TestShouldSetOffline_InteractionWithShouldRestore verifies the core invariant
// of the state machine: the two decision functions must never both return true
// for the same router at the same time, because that would mean the watcher is
// both trying to set a router OFFLINE and restore it simultaneously.
//
// This is possible to violate if someone changes the status guards incorrectly.
func TestShouldSetOffline_InteractionWithShouldRestore(t *testing.T) {
	statuses := []string{
		string(tc.CacheStatusReported),
		string(tc.CacheStatusOffline),
		string(tc.CacheStatusOnline),
		string(tc.CacheStatusAdminDown),
	}
	for _, status := range statuses {
		setOffline := shouldSetOffline(1, 1, false, status)
		restore := shouldRestore(2, 2, true, status)
		if setOffline && restore {
			t.Errorf("status=%q: shouldSetOffline=true AND shouldRestore=true simultaneously -- "+
				"state machine invariant violated: cannot both set OFFLINE and restore at once", status)
		}
	}
}
