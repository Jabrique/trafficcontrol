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
	"encoding/json"
	"fmt"
	"time"

	"github.com/apache/trafficcontrol/v8/traffic_monitor/threadsafe"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/towrap"
)

func srvTRConfig(opsConfig threadsafe.OpsConfig, toSession towrap.TrafficOpsSessionThreadsafe) ([]byte, time.Time, error) {
	config := opsConfig.Get()
	if !toSession.Initialized() {
		return nil, time.Time{}, fmt.Errorf("Unable to connect to Traffic Ops")
	}
	if config.CdnName == "" {
		return nil, time.Time{}, fmt.Errorf("No CDN Configured")
	}

	// Check if multiple CDNs are managed
	if len(config.ManagedCdns) > 1 {
		// Return multi-CDN response
		return srvMultiCDNCRConfig(opsConfig, toSession)
	}

	// Single CDN response (backward compatibility)
	return toSession.LastCRConfig(config.CdnName)
}

// srvMultiCDNCRConfig returns CRConfig for multiple CDNs in array format
func srvMultiCDNCRConfig(opsConfig threadsafe.OpsConfig, toSession towrap.TrafficOpsSessionThreadsafe) ([]byte, time.Time, error) {
	config := opsConfig.Get()
	response := map[string]interface{}{
		"cdnConfigs": []map[string]interface{}{},
	}
	
	var latestTime time.Time
	
	// Get CRConfig for each managed CDN
	for _, cdnName := range config.ManagedCdns {
		if cdnName == "" {
			continue
		}
		
		crconfig, configTime, err := toSession.LastCRConfig(cdnName)
		if err != nil {
			// Log error but continue with other CDNs
			fmt.Printf("Error getting CRConfig for CDN '%s': %v\n", cdnName, err)
			continue
		}
		
		// Track latest modification time
		if configTime.After(latestTime) {
			latestTime = configTime
		}
		
		// Parse CRConfig JSON
		var crconfigObj interface{}
		if err := json.Unmarshal(crconfig, &crconfigObj); err != nil {
			fmt.Printf("Error parsing CRConfig for CDN '%s': %v\n", cdnName, err)
			continue
		}
		
		// Add to response
		cdnConfig := map[string]interface{}{
			"cdnName": cdnName,
			"crconfig": crconfigObj,
		}
		response["cdnConfigs"] = append(response["cdnConfigs"].([]map[string]interface{}), cdnConfig)
	}
	
	// Marshal response
	responseBytes, err := json.Marshal(response)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("Error marshaling multi-CDN CRConfig response: %v", err)
	}
	
	return responseBytes, latestTime, nil
}
