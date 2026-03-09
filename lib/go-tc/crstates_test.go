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
	"encoding/json"
	"testing"
	"time"
)

func TestNewCRStatesInitializesRouters(t *testing.T) {
	states := NewCRStates(1, 1)
	if states.Routers == nil {
		t.Fatal("expected NewCRStates to initialize Routers map, got nil")
	}
}

func TestCRStatesCopyIncludesRouters(t *testing.T) {
	states := NewCRStates(0, 0)
	routerName := RouterName("tr-01")
	states.Routers[routerName] = IsAvailable{
		IsAvailable:   true,
		Ipv4Available: true,
		Ipv6Available: true,
		Status:        "REPORTED - available",
		LastPoll:      time.Now(),
	}

	copied := states.Copy()

	if len(copied.Routers) != 1 {
		t.Fatalf("expected copied Routers length 1, got %d", len(copied.Routers))
	}
	if !copied.Routers[routerName].IsAvailable {
		t.Fatal("expected copied router to be available")
	}

	// verify deep copy: mutating original should not affect copy
	states.Routers[RouterName("tr-02")] = IsAvailable{IsAvailable: false}
	if len(copied.Routers) != 1 {
		t.Fatal("modifying original Routers affected the copy")
	}
}

func TestCopyRouters(t *testing.T) {
	states := NewCRStates(0, 0)
	routerName := RouterName("tr-01")
	states.Routers[routerName] = IsAvailable{
		IsAvailable:   true,
		Ipv4Available: true,
		Ipv6Available: false,
		Status:        "REPORTED - available",
	}

	routersCopy := states.CopyRouters()

	if len(routersCopy) != 1 {
		t.Fatalf("expected CopyRouters length 1, got %d", len(routersCopy))
	}
	if !routersCopy[routerName].IsAvailable {
		t.Fatal("expected copied router to be available")
	}
	if routersCopy[routerName].Ipv6Available {
		t.Fatal("expected copied router Ipv6Available to be false")
	}

	// verify deep copy
	states.Routers[RouterName("tr-02")] = IsAvailable{IsAvailable: false}
	if len(routersCopy) != 1 {
		t.Fatal("modifying original Routers affected the CopyRouters result")
	}
}

func TestCRStatesMarshallIncludesRouters(t *testing.T) {
	states := NewCRStates(0, 0)
	states.Routers[RouterName("tr-01")] = IsAvailable{
		IsAvailable:   true,
		Ipv4Available: true,
		Ipv6Available: true,
		Status:        "REPORTED - available",
	}

	data, err := CRStatesMarshall(states)
	if err != nil {
		t.Fatalf("CRStatesMarshall error: %v", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to unmarshal CRStates JSON: %v", err)
	}

	if _, ok := raw["routers"]; !ok {
		t.Fatal("expected 'routers' field in CRStates JSON, not found")
	}
}

func TestCRStatesUnMarshallWithRouters(t *testing.T) {
	jsonStr := `{
		"caches": {},
		"deliveryServices": {},
		"routers": {
			"tr-01": {
				"isAvailable": true,
				"ipv4Available": true,
				"ipv6Available": false,
				"status": "REPORTED - available",
				"lastPoll": "2026-01-01T00:00:00Z"
			}
		}
	}`

	states, err := CRStatesUnMarshall([]byte(jsonStr))
	if err != nil {
		t.Fatalf("CRStatesUnMarshall error: %v", err)
	}

	if states.Routers == nil {
		t.Fatal("expected Routers map to be non-nil after unmarshal")
	}
	router, ok := states.Routers[RouterName("tr-01")]
	if !ok {
		t.Fatal("expected router 'tr-01' in unmarshalled Routers")
	}
	if !router.IsAvailable {
		t.Fatal("expected router to be available")
	}
	if !router.Ipv4Available {
		t.Fatal("expected router Ipv4Available to be true")
	}
	if router.Ipv6Available {
		t.Fatal("expected router Ipv6Available to be false")
	}
}

func TestCRStatesUnMarshallBackwardCompatible(t *testing.T) {
	// JSON without "routers" field should unmarshal without error
	jsonStr := `{
		"caches": {"edge-01": {"isAvailable": true}},
		"deliveryServices": {}
	}`

	states, err := CRStatesUnMarshall([]byte(jsonStr))
	if err != nil {
		t.Fatalf("CRStatesUnMarshall without routers field should not error: %v", err)
	}

	if states.Routers != nil {
		t.Logf("Routers is %v (nil is acceptable, empty map is also acceptable)", states.Routers)
	}
}
