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
 * "AS IS" BASIS, WITHOUT WARRANTIES OR nCONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

import (
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
	"github.com/apache/trafficcontrol/v8/lib/go-tc"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/cache"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/config"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/health"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/peer"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/threadsafe"
	"github.com/apache/trafficcontrol/v8/traffic_monitor/todata"
)

// StartHealthResultManager starts the goroutine which listens for health results.
// Note this polls the brief stat endpoint from ATS Astats, not the full stats.
// This poll should be quicker and less computationally expensive for ATS, but
// doesn't include all stat data needed for e.g. delivery service calculations.4
// Returns the last health durations, events, the local cache statuses, and the health result history.
func StartHealthResultManager(
	cacheHealthChan <-chan cache.Result,
	toData todata.TODataThreadsafe,
	localStates peer.CRStatesThreadsafe,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
	fetchCount threadsafe.Uint,
	cfg config.Config,
	events health.ThreadsafeEvents,
	localCacheStatus threadsafe.CacheAvailableStatus,
	cachesChanged <-chan struct{},
	combineStates func(),
) (threadsafe.DurationMap, threadsafe.ResultHistory, threadsafe.UnpolledCaches) {
	lastHealthDurations := threadsafe.NewDurationMap()
	healthHistory := threadsafe.NewResultHistory()
	healthUnpolledCaches := threadsafe.NewUnpolledCaches()
	go healthResultManagerListen(
		cacheHealthChan,
		toData,
		localStates,
		lastHealthDurations,
		healthHistory,
		monitorConfig,
		fetchCount,
		events,
		localCacheStatus,
		cfg,
		healthUnpolledCaches,
		cachesChanged,
		combineStates,
	)
	return lastHealthDurations, healthHistory, healthUnpolledCaches
}

func healthResultManagerListen(
	cacheHealthChan <-chan cache.Result,
	toData todata.TODataThreadsafe,
	localStates peer.CRStatesThreadsafe,
	lastHealthDurations threadsafe.DurationMap,
	healthHistory threadsafe.ResultHistory,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
	fetchCount threadsafe.Uint,
	events health.ThreadsafeEvents,
	localCacheStatus threadsafe.CacheAvailableStatus,
	cfg config.Config,
	healthUnpolledCaches threadsafe.UnpolledCaches,
	cachesChanged <-chan struct{},
	combineStates func(),
) {
	haveCachesChanged := func() bool {
		select {
		case <-cachesChanged:
			return true
		default:
			return false
		}
	}

	lastHealthEndTimes := map[tc.CacheName]time.Time{}
	// This reads at least 1 value from the cacheHealthChan. Then, we loop, and try to read from the channel some more. If there's nothing to read, we hit `default` and process. If there is stuff to read, we read it, then inner-loop trying to read more. If we're continuously reading and the channel is never empty, and we hit the tick time, process anyway even though the channel isn't empty, to prevent never processing (starvation).
	var ticker *time.Ticker

	process := func(results []cache.Result) {
		if haveCachesChanged() {
			healthUnpolledCaches.SetNewCaches(getNewCaches(localStates, monitorConfig))
		}
		processHealthResult(
			toData,
			localStates,
			lastHealthDurations,
			healthUnpolledCaches,
			monitorConfig,
			fetchCount,
			events,
			localCacheStatus,
			lastHealthEndTimes,
			healthHistory,
			results,
			cfg,
			combineStates,
		)
	}

	for {
		var results []cache.Result
		results = append(results, <-cacheHealthChan)
		if ticker != nil {
			ticker.Stop()
		}
		ticker = time.NewTicker(cfg.HealthFlushInterval)
	innerLoop:
		for {
			select {
			case <-ticker.C:
				log.Infof("Health Result Manager flushing queued results\n")
				process(results)
				break innerLoop
			default:
				select {
				case r := <-cacheHealthChan:
					results = append(results, r)
				default:
					process(results)
					break innerLoop
				}
			}
		}
	}
}

// processHealthResult processes the given health results, adding their stats to the CacheAvailableStatus. Note this is NOT threadsafe, because it non-atomically gets CacheAvailableStatuses, Events, LastHealthDurations and later updates them. This MUST NOT be called from multiple threads.
func processHealthResult(
	toData todata.TODataThreadsafe,
	localStates peer.CRStatesThreadsafe,
	lastHealthDurationsThreadsafe threadsafe.DurationMap,
	healthUnpolledCaches threadsafe.UnpolledCaches,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
	fetchCount threadsafe.Uint,
	events health.ThreadsafeEvents,
	localCacheStatusThreadsafe threadsafe.CacheAvailableStatus,
	lastHealthEndTimes map[tc.CacheName]time.Time,
	healthHistory threadsafe.ResultHistory,
	results []cache.Result,
	cfg config.Config,
	combineStates func(),
) {
	if len(results) == 0 {
		return
	}
	defer func() {
		for _, r := range results {
			log.Debugf("poll %v %v finish\n", r.PollID, time.Now())
			r.PollFinished <- r.PollID
		}
	}()

	toDataCopy := toData.Get() // create a copy, so the same data used for all processing of this cache health result
	monitorConfigCopy := monitorConfig.Get()
	healthHistoryCopy := healthHistory.Get().Copy()
	for i, healthResult := range results {
		fetchCount.Inc()
		var prevResult cache.Result
		healthResultHistory := healthHistoryCopy[tc.CacheName(healthResult.ID)]
		if len(healthResultHistory) != 0 {
			prevResult = healthResultHistory[len(healthResultHistory)-1]
		}

		if healthResult.Error == nil {
			health.GetVitals(&healthResult, &prevResult, &monitorConfigCopy)
			results[i] = healthResult
		}

		maxHistory := uint64(monitorConfigCopy.Profile[monitorConfigCopy.TrafficServer[string(healthResult.ID)].Profile].Parameters.HistoryCount)
		if maxHistory < 1 {
			log.Infof("processHealthResult got history count %v for %v, setting to 1\n", maxHistory, healthResult.ID)
			maxHistory = 1
		}

		healthHistoryCopy[tc.CacheName(healthResult.ID)] = pruneHistory(append([]cache.Result{healthResult}, healthHistoryCopy[tc.CacheName(healthResult.ID)]...), maxHistory)
	}

	pollerName := "health"
	statResultHistoryNil := (*threadsafe.ResultStatHistory)(nil) // health poller doesn't have stats
	health.CalcAvailability(results, pollerName, statResultHistoryNil, monitorConfigCopy, toDataCopy, localCacheStatusThreadsafe, localStates, events, cfg.CachePollingProtocol)
	combineStates()

	healthHistory.Set(healthHistoryCopy)

	lastHealthDurations := threadsafe.CopyDurationMap(lastHealthDurationsThreadsafe.Get())
	for _, healthResult := range results {
		if lastHealthStart, ok := lastHealthEndTimes[tc.CacheName(healthResult.ID)]; ok {
			d := time.Since(lastHealthStart)
			lastHealthDurations[tc.CacheName(healthResult.ID)] = d
		}
		lastHealthEndTimes[tc.CacheName(healthResult.ID)] = time.Now()
	}
	lastHealthDurationsThreadsafe.Set(lastHealthDurations)
	healthUnpolledCaches.SetHealthPolled(results)
}

// StartRouterHealthResultManager starts a goroutine that listens for router
// health poll results, evaluates health, updates local states, and triggers
// state combination. This is a simplified version of the cache health result
// manager — no vitals, no stat history, no delivery service processing.
func StartRouterHealthResultManager(
	routerHealthChan <-chan health.RouterResult,
	localStates peer.CRStatesThreadsafe,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
	events health.ThreadsafeEvents,
	combineStates func(),
) {
	go routerHealthResultManagerListen(
		routerHealthChan,
		localStates,
		monitorConfig,
		events,
		combineStates,
	)
}

func routerHealthResultManagerListen(
	routerHealthChan <-chan health.RouterResult,
	localStates peer.CRStatesThreadsafe,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
	events health.ThreadsafeEvents,
	combineStates func(),
) {
	for result := range routerHealthChan {
		processRouterHealthResult(result, localStates, monitorConfig, events, combineStates)
	}
}

func processRouterHealthResult(
	result health.RouterResult,
	localStates peer.CRStatesThreadsafe,
	monitorConfig threadsafe.TrafficMonitorConfigMap,
	events health.ThreadsafeEvents,
	combineStates func(),
) {
	defer func() {
		log.Debugf("router poll %v %v finish", result.PollID, time.Now())
		result.PollFinished <- result.PollID
	}()

	routerName := tc.RouterName(result.ID)
	monitorConfigCopy := monitorConfig.Get()

	routerConfig, ok := monitorConfigCopy.TrafficRouter[result.ID]
	if !ok {
		log.Warnf("router health result for unknown router %s — skipping", result.ID)
		return
	}

	result.Status = tc.CacheStatusFromString(routerConfig.ServerStatus)

	profile, profileOk := monitorConfigCopy.Profile[routerConfig.Profile]
	if !profileOk {
		log.Errorf("router %s profile %s not found in config — treating as unavailable", result.ID, routerConfig.Profile)
		localStates.SetRouter(routerName, tc.IsAvailable{IsAvailable: false, DirectlyPolled: true})
		combineStates()
		return
	}

	available, why := health.EvalRouterHealth(result, profile)

	isAvailable := tc.IsAvailable{
		IsAvailable:    available,
		DirectlyPolled: true,
		Status:         why,
		LastPoll:       result.Time,
	}
	if result.UsingIPv4 {
		isAvailable.Ipv4Available = available
		if existing, exists := localStates.GetRouter(routerName); exists {
			isAvailable.Ipv6Available = existing.Ipv6Available
		}
	} else {
		isAvailable.Ipv6Available = available
		if existing, exists := localStates.GetRouter(routerName); exists {
			isAvailable.Ipv4Available = existing.Ipv4Available
		}
	}
	isAvailable.IsAvailable = isAvailable.Ipv4Available || isAvailable.Ipv6Available
	if result.Status == tc.CacheStatusAdminDown {
		isAvailable.IsAvailable = false
	}

	prevAvail, prevExists := localStates.GetRouter(routerName)
	if !prevExists || prevAvail.IsAvailable != isAvailable.IsAvailable {
		availStr := "available"
		if !isAvailable.IsAvailable {
			availStr = "unavailable"
		}
		events.Add(health.Event{
			Time:        health.Time(result.Time),
			Description: why,
			Name:        result.ID,
			Hostname:    result.ID,
			Type:        "ROUTER",
			Available:   isAvailable.IsAvailable,
		})
		log.Infof("router %s now %s: %s", result.ID, availStr, why)
	}

	localStates.SetRouter(routerName, isAvailable)
	combineStates()
}
