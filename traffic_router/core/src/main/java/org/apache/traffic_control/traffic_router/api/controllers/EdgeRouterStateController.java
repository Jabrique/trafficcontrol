/*
 *
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

package org.apache.traffic_control.traffic_router.api.controllers;

import org.apache.traffic_control.traffic_router.core.util.DataExporter;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.http.MediaType;
import org.springframework.http.ResponseEntity;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;

import java.util.HashMap;
import java.util.List;
import java.util.Map;

/**
 * Exposes the availability state of edge Traffic Routers as seen by this TR instance.
 * Accessible via /crs/edge-router-states on the API port (3333).
 *
 * This endpoint allows operators to verify that the TR health filtering is working
 * by showing each edge TR's state, whether Traffic Monitor has reported for it
 * (monitorHasReported), and whether it is effectively included in routing decisions.
 */
@Controller
@RequestMapping("/edge-router-states")
public class EdgeRouterStateController {

	@Autowired
	private DataExporter dataExporter;

	@GetMapping
	public ResponseEntity<Map<String, Object>> getEdgeRouterStates() {
		final Map<String, Object> response = new HashMap<>();
		final List<Map<String, Object>> states = dataExporter.getEdgeRouterStates();
		response.put("edgeRouters", states);
		response.put("totalCount", states.size());
		final long availableCount = states.stream()
				.filter(s -> Boolean.TRUE.equals(s.get("includedInRouting")))
				.count();
		response.put("includedInRoutingCount", availableCount);
		response.put("excludedFromRoutingCount", states.size() - availableCount);
		return ResponseEntity.ok()
				.contentType(MediaType.APPLICATION_JSON)
				.body(response);
	}
}
