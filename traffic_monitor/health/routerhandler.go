package health

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
	"fmt"
	"io"
	"time"

	"github.com/apache/trafficcontrol/v8/lib/go-log"
)

// RouterHandler implements the handler.Handler interface for Traffic Router health polls.
type RouterHandler struct {
	resultChan chan RouterResult
}

// NewRouterHandler creates a new RouterHandler and its result channel.
func NewRouterHandler() RouterHandler {
	return RouterHandler{resultChan: make(chan RouterResult)}
}

// ResultChan returns the channel on which router health results are sent.
func (h RouterHandler) ResultChan() <-chan RouterResult {
	return h.resultChan
}

// Handle processes the raw HTTP poll response for a Traffic Router.
// It parses the /crs/health JSON response to extract health stats,
// then sends a RouterResult to the result channel.
func (h RouterHandler) Handle(id string, r io.Reader, format string, reqTime time.Duration, reqEnd time.Time, err error, pollID uint64, usingIPv4 bool, pollCtx interface{}, pollFinished chan<- uint64) {
	result := RouterResult{
		ID:           id,
		RequestTime:  reqTime,
		UsingIPv4:    usingIPv4,
		PollID:       pollID,
		PollFinished: pollFinished,
		Time:         reqEnd,
		HealthStats:  make(map[string]float64),
	}

	if err != nil {
		result.Error = err
		result.Available = false
		h.resultChan <- result
		return
	}

	if r == nil {
		result.Available = false
		result.Error = fmt.Errorf("nil response body from router %s", id)
		h.resultChan <- result
		return
	}

	result.HealthStats["queryTime"] = float64(reqTime / time.Millisecond)
	result.Available = true

	// Parse optional health stats from /crs/health JSON response
	var healthResponse map[string]interface{}
	if decodeErr := json.NewDecoder(r).Decode(&healthResponse); decodeErr != nil {
		log.Warnf("router %s health response decode error (treating as healthy with queryTime only): %v", id, decodeErr)
		h.resultChan <- result
		return
	}

	// Extract system-level stats if present
	if system, ok := healthResponse["system"].(map[string]interface{}); ok {
		extractFloat(system, "loadAvg", result.HealthStats)
		extractFloat(system, "cpuUsage", result.HealthStats)
		extractFloat(system, "memoryUsagePercent", result.HealthStats)
	}

	// Extract request rate if present
	extractFloat(healthResponse, "requestRate", result.HealthStats)

	h.resultChan <- result
}

// extractFloat safely extracts a float64 value from a map and puts it into the target map.
func extractFloat(src map[string]interface{}, key string, dst map[string]float64) {
	if val, ok := src[key]; ok {
		switch v := val.(type) {
		case float64:
			dst[key] = v
		case json.Number:
			if f, err := v.Float64(); err == nil {
				dst[key] = f
			}
		}
	}
}
