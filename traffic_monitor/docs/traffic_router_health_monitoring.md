# Traffic Router Health Monitoring by Traffic Monitor

## Overview

This feature extends Apache Traffic Control's Traffic Monitor (TM) to actively monitor the health of Traffic Router (TR) servers, in addition to the existing cache server monitoring. Previously, TM only monitored cache servers (EDGE/MID), leaving Traffic Routers unmonitored. This meant a down TR with edge routing enabled could still receive traffic because its status was never automatically updated.

### Problem

When edge DNS/HTTP routing is enabled (`edge.dns.routing` / `edge.http.routing` in CRConfig), Traffic Router routes clients to edge Traffic Router instances. If an edge TR goes down but no system updates its status, clients are directed to an unresponsive TR.

### Solution

Traffic Monitor now polls TR health endpoints, evaluates their health against configurable thresholds, combines local observations with peer TM observations using the optimistic health protocol, and publishes router availability in CRStates. Traffic Router can then consume this data to skip unavailable edge TRs.

---

## Architecture

### Data Flow

```
Traffic Ops                    Traffic Monitor                 Peer TM
     │                              │                            │
     │  /api/.../monitoring         │                            │
     │  (includes TrafficRouters[]) │                            │
     │─────────────────────────────►│                            │
     │                              │                            │
     │                              │  Poll each TR:             │
     │                              │  GET /crs/health           │
     │                              │  ◄─── TR responds ───►     │
     │                              │                            │
     │                              │  EvalRouterHealth()        │
     │                              │  → localStates.SetRouter() │
     │                              │                            │
     │                              │  combineRouterState()      │
     │                              │  ◄── peer CRStates ────────│
     │                              │  (optimistic protocol)     │
     │                              │                            │
     │                              │  /publish/CrStates         │
     │                              │  includes "routers" field  │
     │                              │                            │
                                    │                            │
Traffic Router ◄────────────────────│                            │
  Reads CRStates                    │                            │
  Filters unavailable edge TRs      │                            │
```

### Components Modified

| Component | Language | Changes |
|-----------|----------|---------|
| `lib/go-tc` | Go | Added `RouterName` type, `Routers` field to `CRStates`, `TrafficRouter` struct |
| Traffic Ops | Go | Extended monitoring API to return full router data (hostname, IP, FQDN, interfaces) |
| Traffic Monitor | Go | Router health polling, evaluation, state combination, API endpoints, UI |
| Traffic Router | Java | `/crs/health` endpoint, consume router states from CRStates, filter unavailable edge TRs |

---

## Configuration

### Traffic Router Health Endpoint

Traffic Router exposes a `/crs/health` endpoint (port 3333) that returns system health metrics:

```json
{
  "healthy": true,
  "uptime": 86400000,
  "requestRate": 150.5,
  "system": {
    "loadAvg": 1.2,
    "cpuUsage": 0.35,
    "memoryUsagePercent": 0.55,
    "memoryUsed": 2147483648,
    "memoryAvailable": 4294967296
  },
  "threads": {
    "active": 25,
    "max": 200,
    "queueDepth": 0
  }
}
```

### Health Thresholds

Configure via Traffic Ops profile parameters (same mechanism as cache thresholds).
All thresholds are **optional** — if none are configured, the router is considered healthy as long as it responds to the health poll (connectivity/liveness check).

| Parameter | Example | Description |
|-----------|---------|-------------|
| `health_threshold.queryTime` | `>0 <1000` | Maximum response time in milliseconds |
| `health_threshold.loadAvg` | `>0 <25.0` | Maximum system load average. Note: returns -1 on platforms that do not support `getSystemLoadAverage()` (non-Linux); avoid setting a `>0` lower bound if TR may run on such platforms. |
| `health_threshold.cpuUsage` | `>0 <0.90` | Maximum CPU usage as a **fraction (0.0–1.0)**, NOT percentage. E.g., 0.90 = 90% CPU. Uses Java `getProcessCpuLoad()`. |
| `health_threshold.memoryUsagePercent` | `>0 <0.85` | Maximum JVM heap memory usage as a **fraction (0.0–1.0)**, NOT percentage. E.g., 0.85 = 85% heap used. |
| `health_threshold.requestRate` | Per deployment | Maximum requests per second (set based on capacity). |

### Polling Configuration

| Parameter | Default | Description |
|-----------|---------|-------------|
| `health.polling.url` | `http://${hostname}:${port}/crs/health` | URL to poll for health |
| `health.connection.timeout` | `5000ms` | Connection timeout for health polls |
| Polling interval | Same as cache polling (~6s) | Configurable via TM config |

---

## API Endpoints

### Traffic Monitor

#### `GET /publish/CrStates`

Now includes a `"routers"` field in the response:

```json
{
  "caches": {
    "edge-01": {
      "isAvailable": true,
      "ipv4Available": true,
      "ipv6Available": true
    }
  },
  "deliveryServices": {
    "ds-01": {
      "isAvailable": true,
      "disabledLocations": []
    }
  },
  "routers": {
    "tr-edge-01": {
      "isAvailable": true,
      "ipv4Available": true,
      "ipv6Available": true,
      "status": "REPORTED - available"
    }
  }
}
```

**Backward Compatibility**: The `"routers"` field uses `omitempty` — it only appears when routers are being monitored. Old consumers that don't expect this field will silently ignore it (standard JSON parsing behavior).

#### `GET /api/router-statuses`

Returns router health status summary:

```json
{
  "tr-edge-01": {
    "status": "REPORTED",
    "ipv4_available": true,
    "ipv6_available": true,
    "combined_available": true,
    "type": "CCR",
    "cachegroup": "CDN_TR_Group",
    "fqdn": "tr-edge-01.cdn.example.com"
  }
}
```

### Traffic Router

#### `GET /crs/health`

Returns system health metrics (see Configuration section above).

#### `GET /crs/edge-router-states`

Returns the availability state of all known edge Traffic Routers **as seen by this TR instance**. Use this to verify that the health filter is working correctly.

```json
{
  "totalCount": 3,
  "includedInRoutingCount": 2,
  "excludedFromRoutingCount": 1,
  "edgeRouters": [
    {
      "id": "tr-edge-01",
      "fqdn": "tr-edge-01.cdn.example.com",
      "monitorHasReported": true,
      "available": true,
      "ipv4Available": true,
      "ipv6Available": true,
      "includedInRouting": true,
      "reason": "Traffic Monitor reports healthy"
    },
    {
      "id": "tr-edge-02",
      "fqdn": "tr-edge-02.cdn.example.com",
      "monitorHasReported": true,
      "available": false,
      "ipv4Available": false,
      "ipv6Available": false,
      "includedInRouting": false,
      "reason": "Traffic Monitor reports unavailable"
    },
    {
      "id": "tr-edge-03",
      "fqdn": "tr-edge-03.cdn.example.com",
      "monitorHasReported": false,
      "available": false,
      "includedInRouting": true,
      "reason": "fail-open: Traffic Monitor has not reported state yet"
    }
  ]
}
```

**Fields:**

| Field | Description |
|-------|-------------|
| `monitorHasReported` | `true` if TM has ever sent a health state for this TR. `false` during startup or when using an older TM without router monitoring. |
| `available` | Raw `isAvailable` value as reported by TM. Meaningless if `monitorHasReported=false`. |
| `ipv4Available` | IPv4-specific availability. Only present when `monitorHasReported=true`. |
| `ipv6Available` | IPv6-specific availability. Only present when `monitorHasReported=true`. |
| `includedInRouting` | Whether this TR is actually included in routing decisions. When `monitorHasReported=false`, this is always `true` (fail-open). |
| `reason` | Human-readable explanation for the `includedInRouting` value. |

**Cross-referencing with TM:** Compare this output against `GET /api/router-statuses` on Traffic Monitor. Both should reflect the same availability state once TM has reported.

---

## Health Evaluation Logic

### Router vs Cache Comparison

| Feature | Cache Monitoring | Router Monitoring |
|---------|-----------------|-------------------|
| Response format | ATS stats (complex) | Simple JSON |
| Vitals computation | Bandwidth, LoadAvg, KbpsOut | Not needed |
| Interface-level checks | Per-interface bandwidth | Not needed |
| Threshold checks | queryTime, loadavg, bandwidth | queryTime, loadAvg, cpuUsage, memoryUsagePercent |
| Protocol handling | IPv4/IPv6 optimistic | IPv4/IPv6 optimistic (identical) |
| State combination | Optimistic health protocol | Optimistic health protocol (identical) |
| Peer quorum | Shared CRStates | Shared CRStates (automatic) |

### Health Evaluation Flow

1. **Admin Status Check**: `ONLINE` → always available; `ADMIN_DOWN`/`OFFLINE` → always unavailable
2. **Connection Error Check**: If poll failed (timeout, refused, etc.) → unavailable
3. **Threshold Evaluation**: Check each configured threshold against health stats
4. **Protocol Availability**: Track IPv4 and IPv6 availability separately; optimistically available if either protocol works

### Optimistic Health Protocol (State Combination)

Identical to cache monitoring:

1. If locally healthy on **both** IPv4 and IPv6 → **available** (no peer check needed)
2. If no peers online → use local state only
3. If **any** peer reports the router as available → **optimistic override** → mark available
4. If locally down AND no peers report available → **unavailable**

This prevents false-negative scenarios where a TR is healthy but one TM can't reach it due to network partitioning.

---

## Traffic Router Integration

### Consuming Router States

Traffic Router reads the `"routers"` field from CRStates and updates edge TR availability:

```java
// In TrafficRouter.setState()
JsonNode routerStates = states.get("routers");
if (routerStates != null) {
    setRouterStates(routerStates);
}
```

### Edge TR Filtering

When edge routing is enabled, `selectTrafficRoutersLocalized()` now filters unavailable edge TRs using the same fail-open pattern as cache routing:

```java
// Fail-open: include TR if TM hasn't reported yet (!hasAuthority)
// OR if TM reports it as available
if (!tr.hasAuthority() || tr.isAvailable()) {
    trafficRouters.add(tr);
}
```

This ensures clients are only routed to healthy, available edge TRs — while preserving fail-open behavior during startup or when TM has not yet reported state.

---

## Files Changed

### New Files

| File | Description |
|------|-------------|
| `traffic_monitor/health/router.go` | `RouterResult` struct, `EvalRouterHealth()` |
| `traffic_monitor/health/routerhandler.go` | `RouterHandler` — parses `/crs/health` JSON |
| `traffic_monitor/datareq/routerstate.go` | `/api/router-statuses` endpoint |
| `traffic_router/.../HealthController.java` | `/crs/health` Spring endpoint |
| `traffic_router/.../EdgeRouterStateController.java` | `/crs/edge-router-states` observability endpoint |

### Modified Files

| File | Change |
|------|--------|
| `lib/go-tc/crstates.go` | Added `Routers` field to `CRStates`, updated `NewCRStates`/`Copy`/`CopyRouters` |
| `lib/go-tc/enum.go` | Added `RouterName` type |
| `lib/go-tc/traffic_monitor.go` | Added `TrafficRouter` struct and config mapping |
| `traffic_ops/.../monitoring.go` | Extended `Router` struct with full server data |
| `traffic_monitor/peer/crstates.go` | Added `GetRouter`/`SetRouter`/`AddRouter`/`DeleteRouter`/`GetRouters` |
| `traffic_monitor/manager/statecombiner.go` | Added `combineRouterState()`, `pruneCombinedRouters()` |
| `traffic_monitor/manager/monitorconfig.go` | Router config processing loop |
| `traffic_monitor/manager/health.go` | `StartRouterHealthResultManager()`, `processRouterHealthResult()` |
| `traffic_monitor/manager/manager.go` | Wired router health poller, handler, result manager |
| `traffic_monitor/datareq/crstate.go` | Preserve `Routers` in filtered CRStates |
| `traffic_monitor/datareq/datareq.go` | Registered `/api/router-statuses` endpoint |
| `traffic_monitor/static/index.html` | Added "Router States" tab |
| `traffic_monitor/static/script.js` | Added `getRouterStates()` for UI updates |
| `traffic_router/.../TrafficRouter.java` | Consume router states, filter unavailable edge TRs with fail-open |
| `traffic_router/.../DataExporter.java` | Added `getEdgeRouterStates()` for `/crs/edge-router-states` |

---

## Testing

### Unit Tests

| Test File | Tests | Coverage |
|-----------|-------|----------|
| `lib/go-tc/crstates_test.go` | 6 tests | CRStates Router field, serialization, Copy |
| `traffic_monitor/health/router_test.go` | 7 tests | EvalRouterHealth for all status types and thresholds |
| `traffic_monitor/health/routerhandler_test.go` | 7 tests | RouterHandler JSON parsing, error handling |
| `traffic_monitor/peer/peer_test.go` | 3 tests | Router CRUD in CRStatesThreadsafe |
| `traffic_monitor/manager/statecombiner_test.go` | 4 tests | Router state combination, optimistic override, pruning |

### Integration Tests

| Test | Description |
|------|-------------|
| `TestRouterPipelineHealthyPollToCombinedState` | Full pipeline: parse → eval → combine → serialize |
| `TestRouterPipelineUnhealthyThresholdExceeded` | Threshold violation → unavailable through pipeline |
| `TestRouterPipelineOptimisticOverrideFromPeer` | Local down + peer up → optimistic available |
| `TestRouterPipelineMultiProtocolAvailability` | Dual IPv4/IPv6 protocol tracking |
| `TestRouterPipelineCRStatesSerializationBackwardCompat` | Old/new CRStates JSON compatibility |
| `TestRouterPipelineFullCombineCrStates` | `combineCrStates()` processes caches + routers + DS together |
| `TestRouterPipelineAdminDownBlocksAvailability` | ADMIN_DOWN always unavailable |
| `TestRouterPipelinePruneStaleRouters` | Removed routers get pruned from combined states |

### Running Tests

```bash
# All router-related tests
go test -mod=mod -v -run 'Router' ./traffic_monitor/... ./lib/go-tc/

# Integration tests only
go test -mod=mod -v -run 'TestRouterPipeline' ./traffic_monitor/manager/

# Full test suite
go test -mod=mod ./traffic_monitor/... ./lib/go-tc/
```

---

## Deployment Guide

### Prerequisites

1. Traffic Router must expose `/crs/health` endpoint (included in this change via `HealthController.java`)
2. Traffic Ops monitoring API returns full router data (included in this change)

### Steps

1. **Deploy Traffic Router** with the new `HealthController.java` — exposes `/crs/health` on port 3333
2. **Deploy Traffic Ops** with updated monitoring API — returns `TrafficRouters[]` with full data
3. **Deploy Traffic Monitor** with router monitoring — starts polling TRs automatically
4. **Verify** via TM UI: check "Router States" tab shows TR health
5. **Verify** via API: `GET /publish/CrStates` includes `"routers"` field
6. **Verify** filter is working: `GET http://<tr-host>:3333/crs/edge-router-states` — check `includedInRouting` and `monitorHasReported` fields
7. **(Optional) Deploy Traffic Router** with CRStates consumption — filters unavailable edge TRs

### Rollback

- All changes are backward compatible
- Removing any component reverts to previous behavior (no router monitoring)
- `CRStates.Routers` uses `omitempty` — absent if no routers are monitored

---

## Troubleshooting

### Router Not Appearing in CRStates

1. Check TR profile has `health.polling.url` parameter configured
2. Check TR server status is `REPORTED` or `ONLINE` in Traffic Ops
3. Check TM logs for router polling errors
4. Verify `/crs/health` is accessible: `curl http://<tr-host>:3333/crs/health`

### Router Shows Unavailable

1. Check health endpoint response: `curl http://<tr-host>:3333/crs/health`
2. Verify thresholds in TR profile — are they reasonable for the environment?
3. Check TM events log for threshold violation details
4. If using edge routing, check peer TM states for optimistic override

### CRStates Missing Routers Field

1. Ensure TM version includes router monitoring code
2. Ensure TO monitoring API returns `TrafficRouters` (check `/api/.../monitoring` response)
3. Check TM config processing logs — look for "router" entries
