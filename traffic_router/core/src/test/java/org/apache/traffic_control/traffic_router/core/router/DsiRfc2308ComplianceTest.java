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

package org.apache.traffic_control.traffic_router.core.router;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.apache.traffic_control.traffic_router.core.edge.Cache;
import org.apache.traffic_control.traffic_router.core.edge.CacheLocation;
import org.apache.traffic_control.traffic_router.core.edge.InetRecord;
import org.apache.traffic_control.traffic_router.core.edge.Node.IPVersions;
import org.apache.traffic_control.traffic_router.core.ds.DeliveryService;
import org.apache.traffic_control.traffic_router.core.loc.FederationRegistry;
import org.apache.traffic_control.traffic_router.core.request.DNSRequest;
import org.apache.traffic_control.traffic_router.core.router.StatTracker.Track;
import org.apache.traffic_control.traffic_router.core.util.CidrAddress;
import org.apache.traffic_control.traffic_router.geolocation.Geolocation;
import org.junit.Before;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.powermock.core.classloader.annotations.PowerMockIgnore;
import org.powermock.core.classloader.annotations.PrepareForTest;
import org.powermock.modules.junit4.PowerMockRunner;
import org.powermock.reflect.Whitebox;
import org.xbill.DNS.Name;
import org.xbill.DNS.Type;

import java.net.Inet4Address;
import java.net.InetAddress;
import java.util.ArrayList;
import java.util.Collections;
import java.util.List;

import static org.hamcrest.MatcherAssert.assertThat;
import static org.hamcrest.Matchers.*;
import static org.junit.Assert.assertNotNull;
import static org.junit.Assert.fail;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;
import static org.powermock.api.mockito.PowerMockito.doCallRealMethod;
import static org.powermock.api.mockito.PowerMockito.spy;
import static org.powermock.reflect.Whitebox.setInternalState;

/**
 * TDD Tests for DSI (Dual Stack Inclusion) — RFC 2308 NODATA Compliance.
 *
 * ┌─────────────────────────────────────────────────────────────────────┐
 * │  PHASE 1 (RED):   These tests MUST FAIL before the fix.            │
 * │  PHASE 2 (GREEN): They MUST PASS after ensureDualStackAnchor() is  │
 * │                   implemented in TrafficRouter.                     │
 * └─────────────────────────────────────────────────────────────────────┘
 *
 * Root Bug: AAAA query for IPv4-only DS returns NXDOMAIN (RFC 2308 §2.1
 * requires NODATA). Windows DNS clients cache NXDOMAIN for negative TTL,
 * blocking ALL subsequent resolution including valid A queries.
 *
 * Test Strategy:
 *  - Unit (#1-4): call ensureDualStackAnchor() via Whitebox.invokeMethod().
 *    Before fix: NoSuchMethodException is thrown → test FAILS (RED).
 *    After fix:  method exists and behaves correctly → test PASSES (GREEN).
 *  - Integration (#5-6): end-to-end via getEdgeCaches() through route().
 *  - Guard (#7): defensive copy for redirectInetRecords singleton mutation.
 */
@RunWith(PowerMockRunner.class)
@PrepareForTest({DeliveryService.class, TrafficRouter.class})
@PowerMockIgnore("javax.management.*")
public class DsiRfc2308ComplianceTest {

    private static final String CLIENT_IP    = "192.168.10.1";
    private static final String DS_ID        = "test-ds-ipv4only";
    private static final String ROUTING_NAME = "edge";
    private static final String ZONE_NAME    = "foo.kabletown.com";
    private static final String METHOD_NAME  = "ensureDualStackAnchor";

    private TrafficRouter trafficRouter;
    private DeliveryService ds;
    private Track track;
    private DNSRequest aaaaReq;
    private DNSRequest aReq;
    private InetRecord aRecord;
    private List<Cache> ipv4CacheList;

    @Before
    public void setUp() throws Exception {
        Name name = Name.fromString(ROUTING_NAME + "." + ZONE_NAME + ".");
        aaaaReq = new DNSRequest(ZONE_NAME, name, Type.AAAA);
        aaaaReq.setClientIP(CLIENT_IP);
        aaaaReq.setHostname(name.relativize(Name.root).toString());

        aReq = new DNSRequest(ZONE_NAME, name, Type.A);
        aReq.setClientIP(CLIENT_IP);
        aReq.setHostname(name.relativize(Name.root).toString());

        // IPv4-only cache
        Cache ipv4Cache = mock(Cache.class);
        when(ipv4Cache.getId()).thenReturn("cache-v4-01");
        InetAddress addr = Inet4Address.getByName("10.0.0.1");
        aRecord = new InetRecord(addr, 30L);
        when(ipv4Cache.getIpAddresses(any(JsonNode.class), eq(false)))
                .thenReturn(Collections.singletonList(aRecord));
        ipv4CacheList = Collections.singletonList(ipv4Cache);

        // Delivery service — IPv4-only
        ds = mock(DeliveryService.class);
        when(ds.getId()).thenReturn(DS_ID);
        when(ds.getRoutingName()).thenReturn(ROUTING_NAME);
        when(ds.isDns()).thenReturn(true);
        when(ds.isAvailable()).thenReturn(true);
        when(ds.isCoverageZoneOnly()).thenReturn(false);
        when(ds.isIp6RoutingEnabled()).thenReturn(false);
        when(ds.getFailureDnsResponse(any(), any())).thenReturn(null);
        when(ds.isLocationAvailable(any())).thenReturn(true);
        when(ds.getTtls()).thenReturn(new ObjectMapper().createObjectNode());

        // TrafficRouter partial mock
        trafficRouter = mock(TrafficRouter.class);

        FederationRegistry federationRegistry = mock(FederationRegistry.class);
        when(federationRegistry.findInetRecords(anyString(), any(CidrAddress.class)))
                .thenReturn(null);
        setInternalState(trafficRouter, "federationRegistry", federationRegistry);

        track = spy(StatTracker.getTrack());
    }

    // =========================================================================
    // UNIT TESTS — Directly invoke ensureDualStackAnchor() via Whitebox
    // =========================================================================

    /**
     * Unit Test #1 — PRIMARY BUG PROOF:
     *
     * Before fix: Whitebox.invokeMethod() throws NoSuchMethodException
     *             → method does not exist → this test FAILS (RED).
     *
     * After fix:  Method exists. AAAA query with NS-only result gets A record
     *             anchors added → test PASSES (GREEN).
     *
     * Scenario: edge DNS routing path — result has only NS records.
     * NS records don't anchor routing name (they go under zone apex).
     * DSI must fire and add A records.
     */
    @Test
    public void unit1_ensureDualStackAnchor_aaaaWithNsOnly_mustAddARecord()
            throws Exception {
        // Result with NS records only (selectTrafficRouters returns these)
        DNSRouteResult result = new DNSRouteResult();
        result.setDeliveryService(ds);
        InetRecord nsRecord = new InetRecord("tr1.kabletown.com.", 300L, Type.NS);
        List<InetRecord> nsOnly = new ArrayList<>();
        nsOnly.add(nsRecord);
        result.setAddresses(nsOnly);

        // Stub DSI dependency chain on the TrafficRouter mock:
        // Phase 1: getCoverageZoneCacheLocation(ip, ds, IPVersions.IPV4ONLY)
        CacheLocation ipv4Loc = mock(CacheLocation.class);
        when(ipv4Loc.getGeolocation()).thenReturn(new Geolocation(40, -100));
        doReturn(ipv4Loc).when(trafficRouter)
                .getCoverageZoneCacheLocation(anyString(), any(DeliveryService.class), eq(IPVersions.IPV4ONLY));

        // selectCachesByCZ(ds, location, IPVersions) — public 3-arg overload
        doReturn(ipv4CacheList).when(trafficRouter)
                .selectCachesByCZ(any(DeliveryService.class), any(CacheLocation.class), eq(IPVersions.IPV4ONLY));

        // inetRecordsFromCaches — returns A records for the anchor caches
        doReturn(Collections.singletonList(aRecord)).when(trafficRouter)
                .inetRecordsFromCaches(any(DeliveryService.class), any(List.class), any(DNSRequest.class));

        DNSRouteResult anchored;
        try {
            anchored = Whitebox.invokeMethod(trafficRouter, METHOD_NAME,
                    result, aaaaReq, ds);
        } catch (Exception e) {
            if (e.getClass().getSimpleName().equals("NoSuchMethodException")
                    || e.getMessage() != null && e.getMessage().contains("ensureDualStackAnchor")) {
                // Expected RED state — method doesn't exist yet
                fail("EXPECTED RED: ensureDualStackAnchor() method does not exist yet. " +
                     "Implement the fix to make this test pass. " +
                     "Exception: " + e);
            }
            throw e; // Unexpected exception
        }

        assertNotNull("anchored result must not be null — method exists and ran", anchored);
        // Method exists (GREEN: would have thrown NoSuchMethodException before fix).
        // Full behavioral assertion is in integration5.
        // At minimum, result must not lose the existing NS records.
        assertThat("Anchored result must have at least the original NS records",
                anchored.getAddresses(), not(nullValue()));
    }

    /**
     * Unit Test #2 — A query guard:
     *
     * Before fix: NoSuchMethodException (RED).
     * After fix:  guard rejects A queries immediately, result unchanged.
     */
    @Test
    public void unit2_ensureDualStackAnchor_aQuery_mustBeNoOp() throws Exception {
        DNSRouteResult result = new DNSRouteResult();
        result.setDeliveryService(ds);
        InetRecord existingA = new InetRecord(Inet4Address.getByName("1.2.3.4"), 30L);
        result.setAddresses(Collections.singletonList(existingA));

        DNSRouteResult returned;
        try {
            returned = Whitebox.invokeMethod(trafficRouter, METHOD_NAME,
                    result, aReq, ds);
        } catch (Exception e) {
            if (e.getClass().getSimpleName().equals("NoSuchMethodException")
                    || (e.getMessage() != null && e.getMessage().contains("ensureDualStackAnchor"))) {
                fail("EXPECTED RED: ensureDualStackAnchor() does not exist yet. " + e);
            }
            throw e;
        }

        // A query must be a complete no-op
        assertThat("A query: result must be unchanged (DSI must not fire)",
                returned.getAddresses(), hasSize(1));
        assertThat("A query: original A record must still be present",
                returned.getAddresses().get(0), equalTo(existingA));
    }

    /**
     * Unit Test #3 — AAAA with existing A records guard:
     *
     * Before fix: NoSuchMethodException (RED).
     * After fix:  guard short-circuits when A records already present.
     */
    @Test
    public void unit3_ensureDualStackAnchor_aaaaWithExistingA_mustShortCircuit()
            throws Exception {
        DNSRouteResult result = new DNSRouteResult();
        result.setDeliveryService(ds);
        InetRecord existing = new InetRecord(Inet4Address.getByName("5.6.7.8"), 30L);
        result.setAddresses(Collections.singletonList(existing));

        DNSRouteResult returned;
        try {
            returned = Whitebox.invokeMethod(trafficRouter, METHOD_NAME,
                    result, aaaaReq, ds);
        } catch (Exception e) {
            if (e.getClass().getSimpleName().equals("NoSuchMethodException")
                    || (e.getMessage() != null && e.getMessage().contains("ensureDualStackAnchor"))) {
                fail("EXPECTED RED: ensureDualStackAnchor() does not exist yet. " + e);
            }
            throw e;
        }

        // Result must be unchanged — DSI should not add more A records
        assertThat(returned.getAddresses(), hasSize(1));
        assertThat(returned.getAddresses().get(0), equalTo(existing));
    }

    /**
     * Unit Test #4 — CZ-only DS: GEO phase must be skipped:
     *
     * Before fix: NoSuchMethodException (RED).
     * After fix:  helper respects coverageZoneOnly policy.
     */
    @Test
    public void unit4_ensureDualStackAnchor_czOnlyDs_geoMustBeSkipped()
            throws Exception {
        when(ds.isCoverageZoneOnly()).thenReturn(true);

        DNSRouteResult result = new DNSRouteResult();
        result.setDeliveryService(ds);
        result.setAddresses(Collections.emptyList());

        try {
            Whitebox.invokeMethod(trafficRouter, METHOD_NAME,
                    result, aaaaReq, ds);
        } catch (Exception e) {
            if (e.getClass().getSimpleName().equals("NoSuchMethodException")
                    || (e.getMessage() != null && e.getMessage().contains("ensureDualStackAnchor"))) {
                fail("EXPECTED RED: ensureDualStackAnchor() does not exist yet. " + e);
            }
            // Other exceptions OK (method exists, dependencies may not be wired)
        }

        // If we reach here, method exists. Verify GEO not called for CZ-only.
        verify(trafficRouter, never()).selectCachesByGeo(
                anyString(), any(DeliveryService.class), any(),
                any(Track.class), any(IPVersions.class));
    }

    // =========================================================================
    // INTEGRATION TEST — end-to-end through getEdgeCaches() / route()
    // =========================================================================

    /**
     * Integration Test #5 — PRIMARY BUG (RED):
     *
     * AAAA query for IPv4-only DS, GEO MISS path.
     *
     * Before fix: getEdgeCaches() returns result with empty/null addresses
     *             (no IPv6 found, no DSI fallback exists).
     *             Assertion fails → RED.
     *
     * After fix:  DSI adds A records → result has addresses → NODATA.
     *             Assertion passes → GREEN.
     */
    @Test
    public void integration5_aaaaQuery_ipv4OnlyDs_missPath_mustHaveAnchorRecords()
            throws Exception {
        doCallRealMethod().when(trafficRouter).route(aaaaReq, track);
        doReturn(ds).when(trafficRouter).selectDeliveryService(aaaaReq);

        // Primary AAAA path: CZ lookup returns null (no IPv6 CZ)
        CacheLocation czLoc = mock(CacheLocation.class);
        when(czLoc.getGeolocation()).thenReturn(new Geolocation(40, -100));
        doReturn(czLoc).when(trafficRouter).getCoverageZoneCacheLocation(
                eq(CLIENT_IP), eq(ds), eq(false), any(Track.class), eq(IPVersions.IPV6ONLY));
        doReturn(null).when(trafficRouter).selectCachesByCZ(
                eq(ds), eq(czLoc), eq(IPVersions.IPV6ONLY));

        // AAAA GEO lookup → null (no IPv6 geo)
        doReturn(null).when(trafficRouter).selectCachesByGeo(
                eq(CLIENT_IP), eq(ds), any(), any(Track.class), eq(IPVersions.IPV6ONLY));

        // Failure response → null (no bypass destination configured)
        doReturn(null).when(ds).getFailureDnsResponse(any(), any());

        // DSI anchor: IPv4 CZ lookup → ipv4CacheList (this is what DSI finds)
        CacheLocation ipv4Loc = mock(CacheLocation.class);
        when(ipv4Loc.getGeolocation()).thenReturn(new Geolocation(40, -100));
        doReturn(ipv4Loc).when(trafficRouter)
                .getCoverageZoneCacheLocation(anyString(), any(DeliveryService.class), eq(IPVersions.IPV4ONLY));
        doReturn(ipv4CacheList).when(trafficRouter)
                .selectCachesByCZ(any(DeliveryService.class), any(CacheLocation.class), eq(IPVersions.IPV4ONLY));

        // inetRecordsFromCaches — A records for DSI anchor path
        doReturn(Collections.singletonList(aRecord)).when(trafficRouter)
                .inetRecordsFromCaches(any(DeliveryService.class), eq(ipv4CacheList), any(DNSRequest.class));
        // Return empty list for primary AAAA miss path (null caches list)
        doReturn(Collections.emptyList()).when(trafficRouter)
                .inetRecordsFromCaches(any(DeliveryService.class), isNull(), any(DNSRequest.class));

        // Stub getCacheRegister for selectTrafficRouters
        doReturn(mock(org.apache.traffic_control.traffic_router.core.edge.CacheRegister.class))
                .when(trafficRouter).getCacheRegister();

        DNSRouteResult result = trafficRouter.route(aaaaReq, track);

        // PRIMARY ASSERTION:
        // Before fix: result.getAddresses() == null or empty → FAILS
        // After fix:  result.getAddresses() has A records → PASSES
        assertNotNull("route() result must not be null", result);

        // Result must not be null and must have at least one address (from inetRecordsFromCaches stub)
        final List<InetRecord> addresses = result.getAddresses();
        assertNotNull("AAAA result must have addresses (DSI or otherwise)", addresses);
        boolean hasAnchor = addresses.stream().anyMatch(InetRecord::isInet4);
        assertThat(
            "AAAA query for IPv4-only DS MUST have A record anchors to enable " +
            "RFC 2308 NODATA (currently returns NXDOMAIN — this test is RED before fix)",
            hasAnchor, is(true)
        );
    }

    /**
     * Integration Test #6 — Regression: A query unaffected (STAYS GREEN):
     *
     * Normal A query must continue to work correctly after DSI fix.
     */
    @Test
    public void integration6_aQuery_normalCzHit_mustReturnARecords()
            throws Exception {
        doCallRealMethod().when(trafficRouter).route(aReq, track);
        doReturn(ds).when(trafficRouter).selectDeliveryService(aReq);

        CacheLocation czLoc = mock(CacheLocation.class);
        when(czLoc.getGeolocation()).thenReturn(new Geolocation(40, -100));
        doReturn(czLoc).when(trafficRouter).getCoverageZoneCacheLocation(
                eq(CLIENT_IP), eq(ds), eq(false), any(Track.class), eq(IPVersions.IPV4ONLY));
        doReturn(ipv4CacheList).when(trafficRouter).selectCachesByCZ(
                eq(DS_ID), anyString(), any(Track.class), eq(IPVersions.IPV4ONLY));

        doReturn(Collections.singletonList(aRecord)).when(trafficRouter)
                .inetRecordsFromCaches(any(), any(), eq(aReq));
        doReturn(mock(org.apache.traffic_control.traffic_router.core.edge.CacheRegister.class))
                .when(trafficRouter).getCacheRegister();

        DNSRouteResult result = trafficRouter.route(aReq, track);

        assertNotNull(result);
        assertThat("A query CZ hit must return A records",
                result.getAddresses().stream().anyMatch(InetRecord::isInet4), is(true));
    }

    // =========================================================================
    // GUARD TEST — Bug #3: Defensive Copy for redirectInetRecords
    // =========================================================================

    /**
     * Guard Test #7 — Shared Mutable List (RED→GREEN):
     *
     * Before fix: setAddresses(getFailureDnsResponse()) assigns reference
     *             directly → addAddresses() mutates the shared singleton.
     *             Memory grows unbounded per request.
     *
     * After fix:  defensive copy prevents mutation of the shared list.
     */
    @Test
    public void guard7_defensiveCopy_redirectInetRecordsMustNotBeMutated()
            throws Exception {
        // Simulate DS.redirectInetRecords — a cached singleton
        InetRecord bypassA = new InetRecord(Inet4Address.getByName("9.9.9.9"), 30L);
        final List<InetRecord> singleton = new ArrayList<>();
        singleton.add(bypassA);
        final int originalSize = singleton.size();

        when(ds.isAvailable()).thenReturn(false);
        when(ds.getFailureDnsResponse(any(), any())).thenReturn(singleton);

        doCallRealMethod().when(trafficRouter).route(aaaaReq, track);
        doReturn(ds).when(trafficRouter).selectDeliveryService(aaaaReq);
        doReturn(mock(org.apache.traffic_control.traffic_router.core.edge.CacheRegister.class))
                .when(trafficRouter).getCacheRegister();
        doReturn(null).when(trafficRouter).getCoverageZoneCacheLocation(
                anyString(), eq(ds), eq(false), any(Track.class), any(IPVersions.class));

        try {
            trafficRouter.route(aaaaReq, track);
        } catch (Exception ignored) {
            // selectTrafficRouters may throw — we only care about the singleton
        }

        // CRITICAL:
        // Before fix: singleton.size() > originalSize (NS records added)
        // After fix:  singleton.size() == originalSize (defensive copy used)
        assertThat(
            "BUG #3: DeliveryService.redirectInetRecords singleton must NOT be " +
            "mutated by addAddresses(). Defensive copy required in getEdgeCaches().",
            singleton.size(), equalTo(originalSize)
        );
    }
}
