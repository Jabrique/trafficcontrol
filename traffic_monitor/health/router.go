package health

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
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
)

// RouterResult contains the result of polling a Traffic Router's health endpoint.
type RouterResult struct {
	// ID is the hostname of the Traffic Router.
	ID string
	// Error is non-nil if the health poll failed (connection error, timeout, non-200, etc.).
	Error error
	// Available indicates whether the router was reachable and responded with a healthy status.
	Available bool
	// Status is the administrative status of the router from Traffic Ops (ONLINE, REPORTED, ADMIN_DOWN, OFFLINE).
	Status tc.CacheStatus
	// RequestTime is the round-trip time of the health poll request.
	RequestTime time.Duration
	// UsingIPv4 indicates whether this result was obtained via IPv4 (false means IPv6).
	UsingIPv4 bool
	// PollID is a unique identifier for this poll cycle.
	PollID uint64
	// PollFinished is a channel that signals when this poll result has been fully processed.
	PollFinished chan<- uint64
	// HealthStats contains stats parsed from the router's /crs/health response
	// (e.g., queryTime, loadavg, cpuUsage, memoryUsagePercent).
	HealthStats map[string]float64
	// Time is the time this result was received.
	Time time.Time
}

// EvalRouterHealth evaluates the health of a Traffic Router based on its poll
// result and profile thresholds. Returns whether the router is available and
// a descriptive reason string.
func EvalRouterHealth(result RouterResult, profile tc.TMProfile) (bool, string) {
	switch {
	case result.Status == tc.CacheStatusOnline:
		return true, eventDesc(result.Status, AvailableStr)
	case result.Status == tc.CacheStatusAdminDown:
		return false, eventDesc(result.Status, UnavailableStr)
	case result.Status == tc.CacheStatusOffline:
		if result.Error != nil {
			return false, eventDesc(result.Status, UnavailableStr)
		}
		// Physically reachable despite OFFLINE status.
		// Return available so Traffic Ops can detect recovery via TM quorum.
		return true, eventDesc(result.Status, AvailableStr+"; physical recovery detected")
	case result.Status == tc.CacheStatusInvalid:
		log.Errorf("Router %v got invalid status - treating as OFFLINE", result.ID)
		return false, eventDesc(result.Status, UnavailableStr+"; invalid status")
	case result.Error != nil:
		return false, eventDesc(result.Status, fmt.Sprintf("error: %v", result.Error))
	}

	for stat, threshold := range profile.Parameters.Thresholds {
		val, ok := result.HealthStats[stat]
		if !ok {
			continue
		}
		if !inThreshold(threshold, val) {
			return false, eventDesc(result.Status, exceedsThresholdMsg(stat, threshold, val))
		}
	}

	return true, eventDesc(result.Status, AvailableStr)
}
