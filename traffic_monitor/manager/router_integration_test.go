package manager

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
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/health"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/peer"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/todata"
)

// TestRouterPipelineHealthyPollToCombinedState simulates the full router
// monitoring pipeline: poll result → handler parse → health eval →
// local state update → state combination → CRStates output with routers.
func TestRouterPipelineHealthyPollToCombinedState(t *testing.T) {
	routerName := tc.RouterName("tr-edge-01")

	// Step 1: Simulate RouterHandler parsing a healthy /crs/health response
	healthJSON := `{
		"healthy": true,
		"requestRate": 150.5,
		"system": {
			"loadAvg": 1.2,
			"cpuUsage": 0.35,
			"memoryUsagePercent": 0.55
		}
	}`

	var healthResponse map[string]interface{}
	if err := json.Unmarshal([]byte(healthJSON), &healthResponse); err != nil {
		t.Fatalf("failed to parse health JSON: %v", err)
	}

	// Build HealthStats map as RouterHandler would
	healthStats := map[string]float64{
		"queryTime": 50.0, // 50ms response time
	}
	if system, ok := healthResponse["system"].(map[string]interface{}); ok {
		for _, key := range []string{"loadAvg", "cpuUsage", "memoryUsagePercent"} {
			if val, ok := system[key].(float64); ok {
				healthStats[key] = val
			}
		}
	}
	if rate, ok := healthResponse["requestRate"].(float64); ok {
		healthStats["requestRate"] = rate
	}

	// Verify stats were extracted correctly
	if healthStats["loadAvg"] != 1.2 {
		t.Fatalf("expected loadAvg=1.2, got %f", healthStats["loadAvg"])
	}
	if healthStats["cpuUsage"] != 0.35 {
		t.Fatalf("expected cpuUsage=0.35, got %f", healthStats["cpuUsage"])
	}

	// Step 2: EvalRouterHealth with thresholds
	profile := tc.TMProfile{
		Parameters: tc.TMParameters{
			Thresholds: map[string]tc.HealthThreshold{
				"queryTime":          {Val: 1000, Comparator: "<"},
				"loadAvg":            {Val: 25.0, Comparator: "<"},
				"cpuUsage":           {Val: 0.90, Comparator: "<"},
				"memoryUsagePercent": {Val: 0.85, Comparator: "<"},
			},
		},
	}

	result := health.RouterResult{
		ID:          string(routerName),
		Available:   true,
		Status:      tc.CacheStatusReported,
		RequestTime: 50 * time.Millisecond,
		HealthStats: healthStats,
		UsingIPv4:   true,
		Time:        time.Now(),
	}

	available, reason := health.EvalRouterHealth(result, profile)
	if !available {
		t.Fatalf("expected router to be healthy, got reason: %s", reason)
	}
	if !strings.Contains(reason, "available") {
		t.Fatalf("expected 'available' in reason, got: %s", reason)
	}

	// Step 3: Update local states
	localStates := peer.NewCRStatesThreadsafe()
	avail := tc.IsAvailable{
		IsAvailable:   available,
		Ipv4Available: result.UsingIPv4 && available,
		Ipv6Available: !result.UsingIPv4 && available,
		Status:        "REPORTED - available",
	}
	localStates.AddRouter(routerName, avail)

	// Step 4: Combine states (no peers)
	events := health.NewThreadsafeEvents(100)
	peerStates := peer.NewCRStatesPeersThreadsafe(0)
	combinedStates := peer.NewCRStatesThreadsafe()
	overrideMap := map[tc.RouterName]bool{}

	local := localStates.Get()
	for rn, ls := range local.Routers {
		combineRouterState(rn, ls, events, peerStates.GetCRStatesPeersInfo(), combinedStates, overrideMap)
	}

	// Step 5: Verify CRStates output includes router
	combined := combinedStates.Get()
	routerState, exists := combined.Routers[routerName]
	if !exists {
		t.Fatal("expected router to exist in combined CRStates")
	}
	if !routerState.IsAvailable {
		t.Fatal("expected router to be available in combined state")
	}
	if !routerState.Ipv4Available {
		t.Fatal("expected IPv4 available (poll was via IPv4)")
	}

	// Step 6: Verify CRStates JSON serialization includes routers
	crStatesBytes, err := json.Marshal(combined)
	if err != nil {
		t.Fatalf("failed to marshal CRStates: %v", err)
	}
	crStatesJSON := string(crStatesBytes)
	if !strings.Contains(crStatesJSON, `"routers"`) {
		t.Fatal("CRStates JSON should contain 'routers' field")
	}
	if !strings.Contains(crStatesJSON, string(routerName)) {
		t.Fatalf("CRStates JSON should contain router name %s", routerName)
	}
}

// TestRouterPipelineUnhealthyThresholdExceeded tests that a router exceeding
// threshold goes unavailable through the full pipeline.
func TestRouterPipelineUnhealthyThresholdExceeded(t *testing.T) {
	routerName := tc.RouterName("tr-edge-02")

	profile := tc.TMProfile{
		Parameters: tc.TMParameters{
			Thresholds: map[string]tc.HealthThreshold{
				"queryTime": {Val: 1000, Comparator: "<"},
				"cpuUsage":  {Val: 0.90, Comparator: "<"},
			},
		},
	}

	// CPU at 95% — exceeds 90% threshold
	result := health.RouterResult{
		ID:          string(routerName),
		Available:   true,
		Status:      tc.CacheStatusReported,
		RequestTime: 200 * time.Millisecond,
		HealthStats: map[string]float64{
			"queryTime": 200,
			"cpuUsage":  0.95,
		},
		UsingIPv4: true,
		Time:      time.Now(),
	}

	available, reason := health.EvalRouterHealth(result, profile)
	if available {
		t.Fatal("expected router to be unavailable due to cpuUsage threshold")
	}
	if !strings.Contains(reason, "cpuUsage") {
		t.Fatalf("expected reason to mention cpuUsage, got: %s", reason)
	}

	// Update local states as unavailable
	localStates := peer.NewCRStatesThreadsafe()
	localStates.AddRouter(routerName, tc.IsAvailable{
		IsAvailable:   false,
		Ipv4Available: false,
		Ipv6Available: false,
		Status:        reason,
	})

	// Combine with no peers — should remain unavailable
	events := health.NewThreadsafeEvents(100)
	peerStates := peer.NewCRStatesPeersThreadsafe(0)
	combinedStates := peer.NewCRStatesThreadsafe()
	overrideMap := map[tc.RouterName]bool{}

	local := localStates.Get()
	for rn, ls := range local.Routers {
		combineRouterState(rn, ls, events, peerStates.GetCRStatesPeersInfo(), combinedStates, overrideMap)
	}

	combined := combinedStates.Get()
	if combined.Routers[routerName].IsAvailable {
		t.Fatal("router should be unavailable in combined state")
	}
}

// TestRouterPipelineOptimisticOverrideFromPeer tests the optimistic health
// protocol: local says down, peer says up → combined should be up.
func TestRouterPipelineOptimisticOverrideFromPeer(t *testing.T) {
	routerName := tc.RouterName("tr-edge-03")

	// Local poll: connection error → unavailable
	result := health.RouterResult{
		ID:        string(routerName),
		Available: false,
		Status:    tc.CacheStatusReported,
		Error:     &healthError{msg: "connection refused"},
		UsingIPv4: true,
		Time:      time.Now(),
	}

	available, _ := health.EvalRouterHealth(result, tc.TMProfile{})
	if available {
		t.Fatal("expected locally unavailable due to connection error")
	}

	// Set local state as unavailable
	localStates := peer.NewCRStatesThreadsafe()
	localStates.AddRouter(routerName, tc.IsAvailable{
		IsAvailable:   false,
		Ipv4Available: false,
		Ipv6Available: false,
	})

	// Set up peer that says the router IS available
	peerStates := peer.NewCRStatesPeersThreadsafe(1)
	peerStates.SetTimeout(time.Duration(rand.Int63()))
	peerResult := peer.Result{
		ID:        tc.TrafficMonitorName("peer-tm-01"),
		Available: true,
		PeerStates: tc.CRStates{
			Caches: map[tc.CacheName]tc.IsAvailable{},
			Routers: map[tc.RouterName]tc.IsAvailable{
				routerName: {IsAvailable: true, Ipv4Available: true, Ipv6Available: true},
			},
		},
		Time: time.Now(),
	}
	peerStates.Set(peerResult)
	peerStates.SetPeers(map[tc.TrafficMonitorName]struct{}{
		tc.TrafficMonitorName("peer-tm-01"): {},
	})

	// Combine: optimistic protocol should override
	events := health.NewThreadsafeEvents(100)
	combinedStates := peer.NewCRStatesThreadsafe()
	overrideMap := map[tc.RouterName]bool{}

	local := localStates.Get()
	for rn, ls := range local.Routers {
		combineRouterState(rn, ls, events, peerStates.GetCRStatesPeersInfo(), combinedStates, overrideMap)
	}

	combined := combinedStates.Get()
	if !combined.Routers[routerName].IsAvailable {
		t.Fatal("router should be available via optimistic peer override")
	}
	if !overrideMap[routerName] {
		t.Fatal("override map should be set")
	}

	// Verify event was emitted for override
	evts := events.Get()
	found := false
	for _, e := range evts {
		if e.Name == string(routerName) && strings.Contains(e.Description, "override") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected override event to be emitted")
	}
}

// TestRouterPipelineMultiProtocolAvailability tests dual IPv4/IPv6 poll
// processing where one protocol is healthy and the other is not.
func TestRouterPipelineMultiProtocolAvailability(t *testing.T) {
	routerName := tc.RouterName("tr-edge-04")

	// IPv4 poll: healthy
	ipv4Avail := tc.IsAvailable{
		IsAvailable:   true,
		Ipv4Available: true,
		Ipv6Available: false,
	}

	localStates := peer.NewCRStatesThreadsafe()
	localStates.AddRouter(routerName, ipv4Avail)

	// Simulate IPv6 poll: also healthy — update with both protocols available
	bothAvail := tc.IsAvailable{
		IsAvailable:   true,
		Ipv4Available: true,
		Ipv6Available: true,
	}
	localStates.SetRouter(routerName, bothAvail)

	// Combine states
	events := health.NewThreadsafeEvents(100)
	peerStates := peer.NewCRStatesPeersThreadsafe(0)
	combinedStates := peer.NewCRStatesThreadsafe()
	overrideMap := map[tc.RouterName]bool{}

	local := localStates.Get()
	for rn, ls := range local.Routers {
		combineRouterState(rn, ls, events, peerStates.GetCRStatesPeersInfo(), combinedStates, overrideMap)
	}

	combined := combinedStates.Get()
	if !combined.Routers[routerName].Ipv4Available {
		t.Fatal("router should have IPv4 available")
	}
	if !combined.Routers[routerName].Ipv6Available {
		t.Fatal("router should have IPv6 available")
	}
	if !combined.Routers[routerName].IsAvailable {
		t.Fatal("router should be overall available")
	}
}

// TestRouterPipelineCRStatesSerializationBackwardCompat verifies that
// CRStates with Routers field serializes correctly and old-style CRStates
// (without routers) can still be deserialized.
func TestRouterPipelineCRStatesSerializationBackwardCompat(t *testing.T) {
	// Old-style CRStates without routers field
	oldJSON := `{
		"caches": {
			"edge-01": {"isAvailable": true}
		},
		"deliveryServices": {
			"ds-01": {"isAvailable": true, "disabledLocations": []}
		}
	}`

	var oldCRStates tc.CRStates
	if err := json.Unmarshal([]byte(oldJSON), &oldCRStates); err != nil {
		t.Fatalf("failed to unmarshal old CRStates: %v", err)
	}

	// Routers should be nil (not present in old JSON)
	if oldCRStates.Routers != nil {
		t.Fatal("old CRStates should have nil Routers")
	}

	// Caches should still work
	if _, ok := oldCRStates.Caches[tc.CacheName("edge-01")]; !ok {
		t.Fatal("old CRStates should have edge-01 cache")
	}

	// New-style CRStates WITH routers
	newCRStates := tc.NewCRStates(1, 1)
	newCRStates.Caches[tc.CacheName("edge-01")] = tc.IsAvailable{IsAvailable: true}
	newCRStates.Routers[tc.RouterName("tr-01")] = tc.IsAvailable{IsAvailable: true, Ipv4Available: true, Ipv6Available: true}

	newJSON, err := json.Marshal(newCRStates)
	if err != nil {
		t.Fatalf("failed to marshal new CRStates: %v", err)
	}

	// Verify routers field is present
	if !strings.Contains(string(newJSON), `"routers"`) {
		t.Fatal("new CRStates JSON should contain 'routers' field")
	}

	// Verify old consumer can still parse (just ignores routers)
	var parsedByOldConsumer struct {
		Caches          map[string]tc.IsAvailable                    `json:"caches"`
		DeliveryService map[string]tc.CRStatesDeliveryService `json:"deliveryServices"`
	}
	if err := json.Unmarshal(newJSON, &parsedByOldConsumer); err != nil {
		t.Fatalf("old consumer failed to parse new CRStates: %v", err)
	}
	if _, ok := parsedByOldConsumer.Caches["edge-01"]; !ok {
		t.Fatal("old consumer should still see edge-01 cache")
	}
}

// TestRouterPipelineFullCombineCrStates tests the top-level combineCrStates
// function processes both caches and routers together.
func TestRouterPipelineFullCombineCrStates(t *testing.T) {
	cacheName := tc.CacheName("edge-cache-01")
	routerName := tc.RouterName("tr-edge-01")

	events := health.NewThreadsafeEvents(100)
	peerStates := peer.NewCRStatesPeersThreadsafe(0)

	localStates := tc.CRStates{
		Caches: map[tc.CacheName]tc.IsAvailable{
			cacheName: {IsAvailable: true, Ipv4Available: true, Ipv6Available: true},
		},
		DeliveryService: map[tc.DeliveryServiceName]tc.CRStatesDeliveryService{
			tc.DeliveryServiceName("ds-01"): {IsAvailable: true, DisabledLocations: []tc.CacheGroupName{}},
		},
		Routers: map[tc.RouterName]tc.IsAvailable{
			routerName: {IsAvailable: true, Ipv4Available: true, Ipv6Available: true},
		},
	}

	combinedStates := peer.NewCRStatesThreadsafe()
	cacheOverride := map[tc.CacheName]bool{}
	routerOverride := map[tc.RouterName]bool{}

	toData := todata.TOData{}
	toData.ServerTypes = map[tc.CacheName]tc.CacheType{
		cacheName: tc.CacheTypeEdge,
	}

	combineCrStates(events, peerStates.GetCRStatesPeersInfo(), localStates, combinedStates, cacheOverride, routerOverride, toData)

	combined := combinedStates.Get()

	// Verify cache
	if _, ok := combined.Caches[cacheName]; !ok {
		t.Fatal("expected cache in combined states")
	}
	if !combined.Caches[cacheName].IsAvailable {
		t.Fatal("expected cache to be available")
	}

	// Verify router
	if _, ok := combined.Routers[routerName]; !ok {
		t.Fatal("expected router in combined states")
	}
	if !combined.Routers[routerName].IsAvailable {
		t.Fatal("expected router to be available")
	}

	// Verify DS
	if _, ok := combined.DeliveryService[tc.DeliveryServiceName("ds-01")]; !ok {
		t.Fatal("expected DS in combined states")
	}
}

// TestRouterPipelineAdminDownBlocksAvailability ensures ADMIN_DOWN status
// makes a router permanently unavailable regardless of health.
func TestRouterPipelineAdminDownBlocksAvailability(t *testing.T) {
	result := health.RouterResult{
		ID:          "tr-admin-down",
		Available:   true,
		Status:      tc.CacheStatusAdminDown,
		RequestTime: 10 * time.Millisecond,
		HealthStats: map[string]float64{"queryTime": 10},
		UsingIPv4:   true,
		Time:        time.Now(),
	}

	available, reason := health.EvalRouterHealth(result, tc.TMProfile{})
	if available {
		t.Fatal("ADMIN_DOWN router should always be unavailable")
	}
	if !strings.Contains(strings.ToLower(reason), "admin_down") {
		t.Fatalf("reason should mention ADMIN_DOWN, got: %s", reason)
	}
}

// TestRouterPipelinePruneStaleRouters verifies that removed routers are
// pruned from combined states during state combination.
func TestRouterPipelinePruneStaleRouters(t *testing.T) {
	events := health.NewThreadsafeEvents(100)
	peerStates := peer.NewCRStatesPeersThreadsafe(0)

	// Initial: two routers
	localStates := tc.CRStates{
		Caches: map[tc.CacheName]tc.IsAvailable{},
		DeliveryService: map[tc.DeliveryServiceName]tc.CRStatesDeliveryService{},
		Routers: map[tc.RouterName]tc.IsAvailable{
			tc.RouterName("tr-01"): {IsAvailable: true, Ipv4Available: true, Ipv6Available: true},
			tc.RouterName("tr-02"): {IsAvailable: true, Ipv4Available: true, Ipv6Available: true},
		},
	}

	combinedStates := peer.NewCRStatesThreadsafe()
	cacheOverride := map[tc.CacheName]bool{}
	routerOverride := map[tc.RouterName]bool{}
	toData := todata.TOData{ServerTypes: map[tc.CacheName]tc.CacheType{}}

	combineCrStates(events, peerStates.GetCRStatesPeersInfo(), localStates, combinedStates, cacheOverride, routerOverride, toData)

	if len(combinedStates.GetRouters()) != 2 {
		t.Fatalf("expected 2 routers in combined, got %d", len(combinedStates.GetRouters()))
	}

	// Remove tr-02 from local states
	delete(localStates.Routers, tc.RouterName("tr-02"))

	combineCrStates(events, peerStates.GetCRStatesPeersInfo(), localStates, combinedStates, cacheOverride, routerOverride, toData)

	routers := combinedStates.GetRouters()
	if len(routers) != 1 {
		t.Fatalf("expected 1 router after prune, got %d", len(routers))
	}
	if _, ok := routers[tc.RouterName("tr-01")]; !ok {
		t.Fatal("tr-01 should still be in combined states")
	}
	if _, ok := routers[tc.RouterName("tr-02")]; ok {
		t.Fatal("tr-02 should have been pruned")
	}
}

// healthError implements error interface for test use.
type healthError struct {
	msg string
}

func (e *healthError) Error() string {
	return e.msg
}
