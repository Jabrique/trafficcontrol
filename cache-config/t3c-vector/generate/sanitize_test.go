package generate

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
	"strings"
	"testing"
)

// Test 1: Normal case -- clean ASCII inputs.
func TestSanitizeFileName_Normal(t *testing.T) {
	result := SanitizeFileName("wowrack", "ds-video-streaming")
	expected := "wowrack__ds-video-streaming.yaml"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// Test 2: Directory traversal and spaces must be replaced with underscores.
// The result must never contain '/' or '..' as path components.
func TestSanitizeFileName_SpecialChars(t *testing.T) {
	result := SanitizeFileName("evil/../../etc", "ds name with spaces")
	// Must not contain raw slash or dot-dot.
	if strings.Contains(result, "/") {
		t.Errorf("filename contains slash: %q", result)
	}
	if strings.Contains(result, "..") {
		t.Errorf("filename contains dot-dot: %q", result)
	}
	// Must end with .yaml.
	if !strings.HasSuffix(result, ".yaml") {
		t.Errorf("filename does not end with .yaml: %q", result)
	}
	// Must contain the double-underscore delimiter.
	if !strings.Contains(result, "__") {
		t.Errorf("filename missing delimiter __: %q", result)
	}
}

// Test 3: Non-ASCII characters (Arabic, CJK, emoji) must be replaced with underscores.
func TestSanitizeFileName_Unicode(t *testing.T) {
	inputs := []struct{ tenant, xmlid string }{
		{"tenant-\u0645\u0635\u0631", "ds-1"},
		{"\u4e2d\u6587tenant", "ds-\u6d4b\u8bd5"},
		{"tenant\U0001F600", "ds-ok"},
	}
	for _, tc := range inputs {
		result := SanitizeFileName(tc.tenant, tc.xmlid)
		for _, r := range result {
			if !isAllowedRune(r) && r != '_' && r != '.' && r != '-' {
				t.Errorf("SanitizeFileName(%q, %q) = %q contains forbidden rune %q",
					tc.tenant, tc.xmlid, result, string(r))
			}
		}
	}
}

// Test (additional): FilterTransformID format.
func TestFilterTransformID(t *testing.T) {
	result := FilterTransformID("wowrack", "ds-video")
	expected := "filter_wowrack__ds-video"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// Test (additional): SinkComponentID format.
func TestSinkComponentID(t *testing.T) {
	result := SinkComponentID("aws_s3", "wowrack", "ds-video")
	expected := "aws_s3_wowrack__ds-video"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
