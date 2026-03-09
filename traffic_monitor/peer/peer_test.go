package peer

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
	"io/ioutil"
	"testing"

	"github.com/apache/trafficcontrol/v8/lib/go-tc"
)

func TestCrStates(t *testing.T) {
	t.Log("Running Peer Tests")

	text, err := ioutil.ReadFile("crstates.json")
	if err != nil {
		t.Log(err)
	}
	crStates, err := tc.CRStatesUnMarshall(text)
	if err != nil {
		t.Log(err)
	}
	fmt.Println(len(crStates.Caches), "caches found")
	for cacheName, crState := range crStates.Caches {
		t.Logf("%v -> %v", cacheName, crState.IsAvailable)
	}

	fmt.Println(len(crStates.DeliveryService), "deliveryservices found")
	for dsName, deliveryService := range crStates.DeliveryService {
		t.Logf("%v -> %v (len:%v)", dsName, deliveryService.IsAvailable, len(deliveryService.DisabledLocations))
	}

}

func TestCRStatesThreadsafeRouterAddGetDelete(t *testing.T) {
	crs := NewCRStatesThreadsafe()
	routerName := tc.RouterName("tr-01")
	avail := tc.IsAvailable{
		IsAvailable:   true,
		Ipv4Available: true,
		Ipv6Available: true,
		Status:        "REPORTED - available",
	}

	crs.AddRouter(routerName, avail)

	got, ok := crs.GetRouter(routerName)
	if !ok {
		t.Fatal("expected GetRouter to return ok=true after AddRouter")
	}
	if !got.IsAvailable {
		t.Fatal("expected router to be available")
	}

	crs.DeleteRouter(routerName)

	_, ok = crs.GetRouter(routerName)
	if ok {
		t.Fatal("expected GetRouter to return ok=false after DeleteRouter")
	}
}

func TestCRStatesThreadsafeSetRouterOnlyUpdatesExisting(t *testing.T) {
	crs := NewCRStatesThreadsafe()
	routerName := tc.RouterName("tr-01")
	nonExistent := tc.RouterName("tr-nonexistent")

	crs.AddRouter(routerName, tc.IsAvailable{IsAvailable: true, Status: "REPORTED"})

	// SetRouter on non-existent should be no-op
	crs.SetRouter(nonExistent, tc.IsAvailable{IsAvailable: false, Status: "REPORTED"})
	_, ok := crs.GetRouter(nonExistent)
	if ok {
		t.Fatal("SetRouter should not create a new router entry")
	}

	// SetRouter on existing should update
	crs.SetRouter(routerName, tc.IsAvailable{IsAvailable: false, Status: "ADMIN_DOWN"})
	got, ok := crs.GetRouter(routerName)
	if !ok {
		t.Fatal("expected router to still exist after SetRouter")
	}
	if got.IsAvailable {
		t.Fatal("expected router to be unavailable after SetRouter update")
	}
}

func TestCRStatesThreadsafeGetRouters(t *testing.T) {
	crs := NewCRStatesThreadsafe()
	crs.AddRouter(tc.RouterName("tr-01"), tc.IsAvailable{IsAvailable: true})
	crs.AddRouter(tc.RouterName("tr-02"), tc.IsAvailable{IsAvailable: false})

	routers := crs.GetRouters()
	if len(routers) != 2 {
		t.Fatalf("expected 2 routers, got %d", len(routers))
	}

	// verify deep copy: mutating returned map shouldn't affect internal state
	routers[tc.RouterName("tr-03")] = tc.IsAvailable{IsAvailable: true}
	routersAgain := crs.GetRouters()
	if len(routersAgain) != 2 {
		t.Fatal("GetRouters returned map is not a deep copy")
	}
}
