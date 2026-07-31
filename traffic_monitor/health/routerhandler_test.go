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
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestRouterHandlerResultChan(t *testing.T) {
	h := NewRouterHandler()
	if h.ResultChan() == nil {
		t.Fatal("expected non-nil result channel")
	}
}

func TestRouterHandlerHandleWithError(t *testing.T) {
	h := NewRouterHandler()
	pollFinished := make(chan uint64, 1)

	go h.Handle("tr-01", nil, "health", 0, time.Now(), fmt.Errorf("connection refused"), 1, true, nil, pollFinished)

	result := <-h.ResultChan()
	if result.Error == nil {
		t.Fatal("expected error in result")
	}
	if result.Available {
		t.Fatal("expected unavailable when error")
	}
	if result.ID != "tr-01" {
		t.Fatalf("expected ID 'tr-01', got '%s'", result.ID)
	}
}

func TestRouterHandlerHandleWithNilReader(t *testing.T) {
	h := NewRouterHandler()
	pollFinished := make(chan uint64, 1)

	go h.Handle("tr-02", nil, "health", 100*time.Millisecond, time.Now(), nil, 2, false, nil, pollFinished)

	result := <-h.ResultChan()
	if result.Error == nil {
		t.Fatal("expected error for nil reader")
	}
	if result.Available {
		t.Fatal("expected unavailable when nil reader")
	}
}

func TestRouterHandlerHandleValidJSON(t *testing.T) {
	h := NewRouterHandler()
	pollFinished := make(chan uint64, 1)

	body := `{
		"healthy": true,
		"requestRate": 150.5,
		"system": {
			"loadAvg": 2.5,
			"cpuUsage": 0.45,
			"memoryUsagePercent": 0.70
		}
	}`

	go h.Handle("tr-03", strings.NewReader(body), "health", 250*time.Millisecond, time.Now(), nil, 3, true, nil, pollFinished)

	result := <-h.ResultChan()
	if result.Error != nil {
		t.Fatalf("unexpected error: %v", result.Error)
	}
	if !result.Available {
		t.Fatal("expected available")
	}
	if result.HealthStats["queryTime"] != 250 {
		t.Fatalf("expected queryTime 250, got %v", result.HealthStats["queryTime"])
	}
	if result.HealthStats["loadAvg"] != 2.5 {
		t.Fatalf("expected loadAvg 2.5, got %v", result.HealthStats["loadAvg"])
	}
	if result.HealthStats["cpuUsage"] != 0.45 {
		t.Fatalf("expected cpuUsage 0.45, got %v", result.HealthStats["cpuUsage"])
	}
	if result.HealthStats["memoryUsagePercent"] != 0.70 {
		t.Fatalf("expected memoryUsagePercent 0.70, got %v", result.HealthStats["memoryUsagePercent"])
	}
	if result.HealthStats["requestRate"] != 150.5 {
		t.Fatalf("expected requestRate 150.5, got %v", result.HealthStats["requestRate"])
	}
}

func TestRouterHandlerHandleInvalidJSON(t *testing.T) {
	h := NewRouterHandler()
	pollFinished := make(chan uint64, 1)

	go h.Handle("tr-04", strings.NewReader("not json"), "health", 50*time.Millisecond, time.Now(), nil, 4, true, nil, pollFinished)

	result := <-h.ResultChan()
	// Invalid JSON should still return available=true (just queryTime)
	if !result.Available {
		t.Fatal("expected available even with invalid JSON — queryTime is still set")
	}
	if result.HealthStats["queryTime"] != 50 {
		t.Fatalf("expected queryTime 50, got %v", result.HealthStats["queryTime"])
	}
}

func TestRouterHandlerHandleEmptyJSON(t *testing.T) {
	h := NewRouterHandler()
	pollFinished := make(chan uint64, 1)

	go h.Handle("tr-05", strings.NewReader("{}"), "health", 30*time.Millisecond, time.Now(), nil, 5, false, nil, pollFinished)

	result := <-h.ResultChan()
	if !result.Available {
		t.Fatal("expected available for empty JSON response")
	}
	if !result.UsingIPv4 {
		// expected: usingIPv4 was set to false
	}
	if result.UsingIPv4 {
		t.Fatal("expected usingIPv4=false")
	}
	if result.HealthStats["queryTime"] != 30 {
		t.Fatalf("expected queryTime 30, got %v", result.HealthStats["queryTime"])
	}
}

func TestExtractFloat(t *testing.T) {
	src := map[string]interface{}{
		"loadAvg":  2.5,
		"strField": "not a number",
	}
	dst := make(map[string]float64)

	extractFloat(src, "loadAvg", dst)
	if dst["loadAvg"] != 2.5 {
		t.Fatalf("expected 2.5, got %v", dst["loadAvg"])
	}

	extractFloat(src, "strField", dst)
	if _, ok := dst["strField"]; ok {
		t.Fatal("string field should not have been extracted as float")
	}

	extractFloat(src, "missing", dst)
	if _, ok := dst["missing"]; ok {
		t.Fatal("missing field should not be in dst")
	}
}
