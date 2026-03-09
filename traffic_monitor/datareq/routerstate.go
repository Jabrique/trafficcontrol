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

package datareq

import (
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/peer"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/threadsafe"

	jsoniter "github.com/json-iterator/go"
)

// RouterStatus contains summary status data for a Traffic Router.
type RouterStatus struct {
	Status            string `json:"status"`
	IPv4Available     bool   `json:"ipv4_available"`
	IPv6Available     bool   `json:"ipv6_available"`
	CombinedAvailable bool   `json:"combined_available"`
	Type              string `json:"type"`
	CacheGroup        string `json:"cachegroup,omitempty"`
	FQDN              string `json:"fqdn,omitempty"`
}

func srvAPIRouterStatuses(
	localStates peer.CRStatesThreadsafe,
	combinedStates peer.CRStatesThreadsafe,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
) ([]byte, error) {
	json := jsoniter.ConfigFastest
	return json.Marshal(createRouterStatuses(localStates, combinedStates, monitorConfig))
}

func createRouterStatuses(
	localStates peer.CRStatesThreadsafe,
	combinedStates peer.CRStatesThreadsafe,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
) map[string]RouterStatus {
	routers := localStates.GetRouters()
	combined := combinedStates.GetRouters()
	configCopy := monitorConfig.Get()

	statuses := make(map[string]RouterStatus, len(routers))
	for routerName, localState := range routers {
		routerNameStr := string(routerName)
		status := RouterStatus{
			CombinedAvailable: localState.IsAvailable,
			IPv4Available:     localState.Ipv4Available,
			IPv6Available:     localState.Ipv6Available,
		}

		if combinedState, ok := combined[routerName]; ok {
			status.CombinedAvailable = combinedState.IsAvailable
			status.IPv4Available = combinedState.Ipv4Available
			status.IPv6Available = combinedState.Ipv6Available
		}

		if routerCfg, ok := configCopy.TrafficRouter[routerNameStr]; ok {
			status.Status = routerCfg.ServerStatus
			status.Type = routerCfg.Type
			status.CacheGroup = routerCfg.Location
			status.FQDN = routerCfg.FQDN
		} else {
			status.Status = tc.CacheStatusFromString("UNKNOWN").String()
			status.Type = tc.RouterTypeName
		}

		statuses[routerNameStr] = status
	}
	return statuses
}
