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
	"errors"
	"testing"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
)

func TestEvalRouterHealthOnlineAlwaysAvailable(t *testing.T) {
	result := RouterResult{
		ID:          "tr-01",
		Available:   true,
		Status:      tc.CacheStatusOnline,
		RequestTime: 100 * time.Millisecond,
	}
	profile := tc.TMProfile{
		Parameters: tc.TMParameters{
			Thresholds: map[string]tc.HealthThreshold{
				"queryTime": {Comparator: "<", Val: 1000},
			},
		},
	}

	avail, why := EvalRouterHealth(result, profile)
	if !avail {
		t.Fatalf("ONLINE router should always be available, got unavailable: %s", why)
	}
}

func TestEvalRouterHealthAdminDownAlwaysUnavailable(t *testing.T) {
	result := RouterResult{
		ID:          "tr-01",
		Available:   true,
		Status:      tc.CacheStatusAdminDown,
		RequestTime: 50 * time.Millisecond,
	}
	profile := tc.TMProfile{}

	avail, _ := EvalRouterHealth(result, profile)
	if avail {
		t.Fatal("ADMIN_DOWN router should always be unavailable")
	}
}

func TestEvalRouterHealthConnectionError(t *testing.T) {
	result := RouterResult{
		ID:     "tr-01",
		Error:  errors.New("connection refused"),
		Status: tc.CacheStatusReported,
	}
	profile := tc.TMProfile{}

	avail, why := EvalRouterHealth(result, profile)
	if avail {
		t.Fatal("router with connection error should be unavailable")
	}
	if why == "" {
		t.Fatal("expected descriptive reason for unavailability")
	}
}

func TestEvalRouterHealthQueryTimeThresholdExceeded(t *testing.T) {
	result := RouterResult{
		ID:          "tr-01",
		Available:   true,
		Status:      tc.CacheStatusReported,
		RequestTime: 2000 * time.Millisecond,
		HealthStats: map[string]float64{
			"queryTime": 2000,
		},
	}
	profile := tc.TMProfile{
		Parameters: tc.TMParameters{
			Thresholds: map[string]tc.HealthThreshold{
				"queryTime": {Comparator: "<", Val: 1000},
			},
		},
	}

	avail, _ := EvalRouterHealth(result, profile)
	if avail {
		t.Fatal("router exceeding queryTime threshold should be unavailable")
	}
}

func TestEvalRouterHealthWithinThresholds(t *testing.T) {
	result := RouterResult{
		ID:          "tr-01",
		Available:   true,
		Status:      tc.CacheStatusReported,
		RequestTime: 200 * time.Millisecond,
		HealthStats: map[string]float64{
			"queryTime":          200,
			"loadavg":            1.5,
			"cpuUsage":           0.30,
			"memoryUsagePercent": 0.50,
		},
	}
	profile := tc.TMProfile{
		Parameters: tc.TMParameters{
			Thresholds: map[string]tc.HealthThreshold{
				"queryTime":          {Comparator: "<", Val: 1000},
				"loadavg":            {Comparator: "<", Val: 25.0},
				"cpuUsage":           {Comparator: "<", Val: 0.90},
				"memoryUsagePercent": {Comparator: "<", Val: 0.85},
			},
		},
	}

	avail, why := EvalRouterHealth(result, profile)
	if !avail {
		t.Fatalf("router within all thresholds should be available, got unavailable: %s", why)
	}
}

func TestEvalRouterHealthOfflineStatus(t *testing.T) {
	result := RouterResult{
		ID:     "tr-01",
		Status: tc.CacheStatusOffline,
	}
	profile := tc.TMProfile{}

	avail, _ := EvalRouterHealth(result, profile)
	if avail {
		t.Fatal("OFFLINE router should be unavailable")
	}
}

func TestRouterResultDefaults(t *testing.T) {
	result := RouterResult{}
	if result.HealthStats != nil {
		t.Fatal("expected nil HealthStats by default")
	}
	if result.Error != nil {
		t.Fatal("expected nil Error by default")
	}
}
