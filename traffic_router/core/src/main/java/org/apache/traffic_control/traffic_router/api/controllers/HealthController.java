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

import org.apache.traffic_control.traffic_router.core.router.StatTracker;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.ResponseBody;

import java.lang.management.ManagementFactory;
import java.lang.management.MemoryMXBean;
import java.lang.management.MemoryUsage;
import java.lang.management.OperatingSystemMXBean;
import java.lang.management.ThreadMXBean;
import java.util.HashMap;
import java.util.Map;

/**
 * Health endpoint for Traffic Monitor to poll.
 * Returns a lightweight JSON response with system and request metrics.
 * Accessible via /crs/health on the API port (3333).
 */
@Controller
@RequestMapping("/health")
public class HealthController {

	@Autowired
	private StatTracker statTracker;

	private final long startTimeMs = System.currentTimeMillis();
	private final Object rateLock = new Object();
	private long lastTotalCount = 0;
	private long lastRateComputeTimeMs = System.currentTimeMillis();
	private volatile double currentRequestRate = 0.0;

	@GetMapping
	public @ResponseBody Map<String, Object> getHealth() {
		final Map<String, Object> health = new HashMap<>();
		health.put("healthy", true);
		health.put("uptime", System.currentTimeMillis() - startTimeMs);

		final long totalDns = statTracker.getTotalDnsCount();
		final long totalHttp = statTracker.getTotalHttpCount();
		final long totalRequests = totalDns + totalHttp;

		health.put("requestCount", totalRequests);
		health.put("requestRate", computeRequestRate(totalRequests));
		health.put("system", getSystemStats());

		return health;
	}

	private double computeRequestRate(final long currentTotal) {
		synchronized (rateLock) {
			final long now = System.currentTimeMillis();
			final long elapsed = now - lastRateComputeTimeMs;
			if (elapsed < 1000) {
				return currentRequestRate;
			}
			final long prevTotal = lastTotalCount;
			lastTotalCount = currentTotal;
			lastRateComputeTimeMs = now;
			currentRequestRate = (currentTotal - prevTotal) * 1000.0 / elapsed;
		}

		return currentRequestRate;
	}

	private Map<String, Object> getSystemStats() {
		final Map<String, Object> system = new HashMap<>();

		final OperatingSystemMXBean osBean = ManagementFactory.getOperatingSystemMXBean();
		system.put("loadAvg", osBean.getSystemLoadAverage());
		system.put("availableProcessors", osBean.getAvailableProcessors());

		if (osBean instanceof com.sun.management.OperatingSystemMXBean) {
			final com.sun.management.OperatingSystemMXBean sunOs = (com.sun.management.OperatingSystemMXBean) osBean;
			system.put("cpuUsage", sunOs.getProcessCpuLoad());
		}

		final MemoryMXBean memBean = ManagementFactory.getMemoryMXBean();
		final MemoryUsage heapUsage = memBean.getHeapMemoryUsage();
		system.put("memoryUsed", heapUsage.getUsed());
		system.put("memoryAvailable", heapUsage.getMax());
		if (heapUsage.getMax() > 0) {
			system.put("memoryUsagePercent", (double) heapUsage.getUsed() / heapUsage.getMax());
		}

		final ThreadMXBean threadBean = ManagementFactory.getThreadMXBean();
		final Map<String, Object> threads = new HashMap<>();
		threads.put("active", threadBean.getThreadCount());
		threads.put("peak", threadBean.getPeakThreadCount());
		system.put("threads", threads);

		return system;
	}
}
