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

package org.apache.traffic_control.traffic_router.core.edge;

import java.util.*;
import java.util.stream.Collectors;

import com.fasterxml.jackson.databind.JsonNode;

import org.apache.traffic_control.traffic_router.core.ds.DeliveryService;
import org.apache.traffic_control.traffic_router.core.ds.DeliveryServiceMatcher;
import org.apache.traffic_control.traffic_router.core.request.Request;

@SuppressWarnings({"PMD.LooseCoupling", "PMD.CyclomaticComplexity"})
public class CacheRegister {
	// Constants for JSON field names to avoid duplicate literals
	private static final String CDN_NAME_FIELD = "cdnName";
	private static final String CRCONFIG_FIELD = "crconfig";
	private static final String CONFIG_FIELD = "config";
	private static final String DELIVERY_SERVICES_FIELD = "deliveryServices";
	private static final String DOMAINS_FIELD = "domains";
	private static final String DOMAIN_NAME_FIELD = "domain_name";
	private final Map<String, CacheLocation> configuredLocations;
	private final Map<String, TrafficRouterLocation> edgeTrafficRouterLocations;
	private JsonNode trafficRouters;
	private Map<String,Cache> allCaches;
	private TreeSet<DeliveryServiceMatcher> deliveryServiceMatchers;
	private Map<String, DeliveryService> dsMap;
	private Map<String, DeliveryService> fqdnToDeliveryServiceMap;
	private JsonNode config;
	private JsonNode stats;
	private int edgeTrafficRouterCount;
	
	// Multi-CDN support fields
	private final Map<String, JsonNode> cdnConfigs;
	private final Map<String, String> domainToCDNMapping;

	public CacheRegister() {
		configuredLocations = new HashMap<String, CacheLocation>();
		edgeTrafficRouterLocations = new HashMap<String, TrafficRouterLocation>();
		cdnConfigs = new HashMap<String, JsonNode>();
		domainToCDNMapping = new HashMap<String, String>();
	}

	public CacheLocation getCacheLocation(final String id) {
		return configuredLocations.get(id);
	}

	public Set<CacheLocation> getCacheLocations() {
		final Set<CacheLocation> result = new HashSet<CacheLocation>(configuredLocations.size());
		result.addAll(configuredLocations.values());
		return result;
	}

	public CacheLocation getCacheLocationById(final String id) {
		for (final CacheLocation location : configuredLocations.values()) {
			if (id.equals(location.getId())) {
				return location;
			}
		}

		return null;
	}

	public TrafficRouterLocation getEdgeTrafficRouterLocation(final String id) {
		return edgeTrafficRouterLocations.get(id);
	}

	public List<TrafficRouterLocation> getEdgeTrafficRouterLocations() {
		return new ArrayList<TrafficRouterLocation>(edgeTrafficRouterLocations.values());
	}

	private void setEdgeTrafficRouterCount(final int count) {
		this.edgeTrafficRouterCount = count;
	}

	public int getEdgeTrafficRouterCount() {
		return edgeTrafficRouterCount;
	}

	public List<Node> getAllEdgeTrafficRouters() {
		final List<Node> edgeTrafficRouters = new ArrayList<>();

		for (final TrafficRouterLocation location : getEdgeTrafficRouterLocations()) {
			edgeTrafficRouters.addAll(location.getTrafficRouters());
		}

		return edgeTrafficRouters;
	}

	/**
	 * Sets the configured locations.
	 * 
	 * @param locations
	 *            the new configured locations
	 */
	public void setConfiguredLocations(final Set<CacheLocation> locations) {
		configuredLocations.clear();
		for (final CacheLocation newLoc : locations) {
			configuredLocations.put(newLoc.getId(), newLoc);
		}
	}

	public void setEdgeTrafficRouterLocations(final Collection<TrafficRouterLocation> locations) {
		int count = 0;

		edgeTrafficRouterLocations.clear();

		for (final TrafficRouterLocation newLoc : locations) {
			edgeTrafficRouterLocations.put(newLoc.getId(), newLoc);

			final List<Node> trafficRouters = newLoc.getTrafficRouters();

			if (trafficRouters != null) {
				count += trafficRouters.size();
			}
		}

		setEdgeTrafficRouterCount(count);
	}

	public boolean hasEdgeTrafficRouters() {
		return !edgeTrafficRouterLocations.isEmpty();
	}

	public void setCacheMap(final Map<String,Cache> map) {
		allCaches = map;
	}

	public Map<String,Cache> getCacheMap() {
		return allCaches;
	}

	public Set<DeliveryServiceMatcher> getDeliveryServiceMatchers(final DeliveryService deliveryService) {
	    return this.deliveryServiceMatchers.stream()
				.filter(deliveryServiceMatcher -> deliveryServiceMatcher.getDeliveryService().getId().equals(deliveryService.getId()))
				.collect(Collectors.toCollection(TreeSet::new));
	}

	public void setDeliveryServiceMatchers(final TreeSet<DeliveryServiceMatcher> matchers) {
		this.deliveryServiceMatchers = matchers;
	}

	/**
	 * Gets the first {@link DeliveryService} that matches the {@link Request}.
	 * 
	 * @param request
	 *            the request to match
	 * @return the DeliveryService that matches the request
	 */
	public DeliveryService getDeliveryService(final Request request) {
		final String requestName = request.getHostname();
		
		// Multi-CDN support: Extract CDN from domain and use CDN-specific config
		if (isMultiCDN()) {
			final String cdnName = extractCDNFromDomain(requestName);
			if (cdnName != null) {
				return getDeliveryServiceForCDN(request, cdnName);
			}
		}
		
		// Single CDN mode - existing logic
		final Map<String, DeliveryService> map = getFQDNToDeliveryServiceMap();
		if (map != null) {
			final DeliveryService ds = map.get(requestName);
			if (ds != null) {
				return ds;
			}
		}
		if (deliveryServiceMatchers == null) {
			return null;
		}

		for (final DeliveryServiceMatcher m : deliveryServiceMatchers) {
			if (m.matches(request)) {
				return m.getDeliveryService();
			}
		}

		return null;
	}

	public DeliveryService getDeliveryService(final String deliveryServiceId) {
		return dsMap.get(deliveryServiceId);
	}

	public List<CacheLocation> filterAvailableCacheLocations(final String deliveryServiceId) {
		final DeliveryService deliveryService = dsMap.get(deliveryServiceId);

		if (deliveryService == null) {
			return null;
		}

		return deliveryService.filterAvailableLocations(getCacheLocations());
	}

	public void setDeliveryServiceMap(final Map<String, DeliveryService> dsMap) {
		this.dsMap = dsMap;
	}

	public Map<String, DeliveryService> getFQDNToDeliveryServiceMap() {
		return fqdnToDeliveryServiceMap;
	}

	public void setFQDNToDeliveryServiceMap(final Map<String, DeliveryService> fqdnToDeliveryServiceMap) {
		this.fqdnToDeliveryServiceMap = fqdnToDeliveryServiceMap;
	}

	public JsonNode getTrafficRouters() {
		return trafficRouters;
	}
	public void setTrafficRouters(final JsonNode o) {
		trafficRouters = o;
	}

	public void setConfig(final JsonNode o) {
		// Check if this is a multi-CDN configuration
		if (o != null && o.has("cdnConfigs")) {
			processMultiCDNConfig(o);
		} else {
			// Single CDN configuration - existing behavior
			config = o;
		}
	}
	public JsonNode getConfig() {
		return config;
	}

	public Map<String, DeliveryService> getDeliveryServices() {
		return this.dsMap;
	}

	public JsonNode getStats() {
		return stats;
	}

	public void setStats(final JsonNode stats) {
		this.stats = stats;
	}

	/**
	 * Process multi-CDN configuration format from Traffic Monitor
	 * Expected format: {"cdnConfigs": [{"cdnName": "...", "crconfig": {...}}, ...]}
	 */
	private void processMultiCDNConfig(final JsonNode multiCDNConfig) {
		final JsonNode cdnConfigsArray = multiCDNConfig.get("cdnConfigs");
		
		if (cdnConfigsArray != null && cdnConfigsArray.isArray()) {
			// Clear existing mappings
			cdnConfigs.clear();
			domainToCDNMapping.clear();
			
			// Process each CDN configuration
			for (final JsonNode cdnConfigNode : cdnConfigsArray) {
				final String cdnName = cdnConfigNode.get(CDN_NAME_FIELD).asText();
				final JsonNode crconfig = cdnConfigNode.get(CRCONFIG_FIELD);
				
				// Store the CRConfig for this CDN
				cdnConfigs.put(cdnName, crconfig);
				
				// Build domain mapping for this CDN
				buildDomainMapping(cdnName, crconfig);
				
				// Set the first CDN as the default config for backward compatibility
				if (config == null && crconfig.has(CONFIG_FIELD)) {
					config = crconfig.get(CONFIG_FIELD);
				}
			}
		}
	}

	/**
	 * Build domain-to-CDN mapping from a CRConfig
	 */
	private void buildDomainMapping(final String cdnName, final JsonNode crconfig) {
		// Add primary domain from config.domain_name
		if (crconfig.has(CONFIG_FIELD) && crconfig.get(CONFIG_FIELD).has(DOMAIN_NAME_FIELD)) {
			final String primaryDomain = crconfig.get(CONFIG_FIELD).get(DOMAIN_NAME_FIELD).asText();
			domainToCDNMapping.put(primaryDomain, cdnName);
		}
		
		// Add domains from delivery services
		if (crconfig.has(DELIVERY_SERVICES_FIELD)) {
			final JsonNode deliveryServices = crconfig.get(DELIVERY_SERVICES_FIELD);
			final Iterator<String> dsNames = deliveryServices.fieldNames();
			
			while (dsNames.hasNext()) {
				final String dsName = dsNames.next();
				final JsonNode ds = deliveryServices.get(dsName);
				
				if (ds.has(DOMAINS_FIELD)) {
					final JsonNode domains = ds.get(DOMAINS_FIELD);
					if (domains.isArray()) {
						for (final JsonNode domainNode : domains) {
							final String domain = domainNode.asText();
							domainToCDNMapping.put(domain, cdnName);
						}
					}
				}
			}
		}
	}

	/**
	 * Extract CDN name from a domain/hostname for multi-CDN routing
	 */
	public String extractCDNFromDomain(final String hostname) {
		if (domainToCDNMapping.isEmpty()) {
			// Single CDN mode - return null to use default behavior
			return null;
		}
		
		// Try exact match first
		final String cdnName = domainToCDNMapping.get(hostname);
		if (cdnName != null) {
			return cdnName;
		}
		
		// Try subdomain matching (e.g., "video.example.com" matches "example.com")
		for (final Map.Entry<String, String> entry : domainToCDNMapping.entrySet()) {
			final String domain = entry.getKey();
			if (hostname.endsWith("." + domain)) {
				return entry.getValue();
			}
		}
		
		// Fallback to first available CDN
		if (!cdnConfigs.isEmpty()) {
			return cdnConfigs.keySet().iterator().next();
		}
		
		return null;
	}

	/**
	 * Get CRConfig for a specific CDN (multi-CDN mode)
	 */
	public JsonNode getCRConfigForCDN(final String cdnName) {
		return cdnConfigs.get(cdnName);
	}

	/**
	 * Check if this is a multi-CDN configuration
	 */
	public boolean isMultiCDN() {
		return !cdnConfigs.isEmpty();
	}

	/**
	 * Get all managed CDN names
	 */
	public Set<String> getManagedCDNs() {
		return cdnConfigs.keySet();
	}

	/**
	 * Get DeliveryService for a specific CDN (multi-CDN mode)
	 */
	@SuppressWarnings("PMD.CyclomaticComplexity")
	private DeliveryService getDeliveryServiceForCDN(final Request request, final String cdnName) {
		final JsonNode cdnConfig = getCRConfigForCDN(cdnName);
		if (cdnConfig == null) {
			return null;
		}
		
		final String requestName = request.getHostname();
		
		// Check delivery services in this CDN's config
		final DeliveryService dsFromConfig = findDeliveryServiceInConfig(cdnConfig, requestName);
		if (dsFromConfig != null) {
			return dsFromConfig;
		}
		
		// Fallback to matcher-based lookup with CDN context
		return findDeliveryServiceInMatchers(request, cdnName);
	}
	
	/**
	 * Find delivery service in CDN configuration
	 */
	private DeliveryService findDeliveryServiceInConfig(final JsonNode cdnConfig, final String requestName) {
		if (!cdnConfig.has(DELIVERY_SERVICES_FIELD)) {
			return null;
		}
		
		final JsonNode deliveryServices = cdnConfig.get(DELIVERY_SERVICES_FIELD);
		final Iterator<String> dsNames = deliveryServices.fieldNames();
		
		while (dsNames.hasNext()) {
			final String dsName = dsNames.next();
			final JsonNode ds = deliveryServices.get(dsName);
			
			if (isDeliveryServiceMatch(ds, requestName)) {
				return dsMap.get(dsName);
			}
		}
		
		return null;
	}
	
	/**
	 * Check if delivery service matches the request
	 */
	private boolean isDeliveryServiceMatch(final JsonNode ds, final String requestName) {
		if (!ds.has(DOMAINS_FIELD)) {
			return false;
		}
		
		final JsonNode domains = ds.get(DOMAINS_FIELD);
		if (!domains.isArray()) {
			return false;
		}
		
		for (final JsonNode domainNode : domains) {
			final String domain = domainNode.asText();
			if (requestName.equals(domain) || requestName.endsWith("." + domain)) {
				return true;
			}
		}
		
		return false;
	}
	
	/**
	 * Find delivery service using matchers
	 */
	private DeliveryService findDeliveryServiceInMatchers(final Request request, final String cdnName) {
		if (deliveryServiceMatchers == null) {
			return null;
		}
		
		for (final DeliveryServiceMatcher m : deliveryServiceMatchers) {
			if (m.matches(request)) {
				final DeliveryService ds = m.getDeliveryService();
				// Verify this DS belongs to the correct CDN
				if (isDeliveryServiceInCDN(ds, cdnName)) {
					return ds;
				}
			}
		}
		
		return null;
	}

	/**
	 * Check if a DeliveryService belongs to a specific CDN
	 */
	private boolean isDeliveryServiceInCDN(final DeliveryService ds, final String cdnName) {
		final JsonNode cdnConfig = getCRConfigForCDN(cdnName);
		if (cdnConfig == null || ds == null) {
			return false;
		}
		
		if (cdnConfig.has(DELIVERY_SERVICES_FIELD)) {
			final JsonNode deliveryServices = cdnConfig.get(DELIVERY_SERVICES_FIELD);
			return deliveryServices.has(ds.getId());
		}
		
		return false;
	}

}
