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

import "strings"

// SanitizeFileName converts a tenant name and DS XMLID into a safe filesystem
// filename. The result is guaranteed to contain only [a-zA-Z0-9_-] and uses
// double underscore as the tenant-DS delimiter. Returns a .yaml file name.
//
// Example: SanitizeFileName("wowrack", "ds-video") -> "wowrack__ds-video.yaml"
// Example: SanitizeFileName("evil/../etc", "ds name") -> "evil_.._.etc__ds_name.yaml"
func SanitizeFileName(tenant, xmlID string) string {
	return sanitizeSegment(tenant) + "__" + sanitizeSegment(xmlID) + ".yaml"
}

// sanitizeSegment replaces any character that is not alphanumeric, a hyphen,
// or an underscore with an underscore. This prevents directory traversal and
// filesystem special characters while keeping names readable.
func sanitizeSegment(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if isAllowedRune(r) {
			buf.WriteRune(r)
		} else {
			buf.WriteRune('_')
		}
	}
	return buf.String()
}

// isAllowedRune reports whether r is safe to use in a filesystem filename.
func isAllowedRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' ||
		r == '_'
}

// FilterTransformID returns the Vector component ID for the per-tenant filter transform.
// Format: "filter_<sanitized_tenant>__<sanitized_xmlid>"
func FilterTransformID(tenant, xmlID string) string {
	return "filter_" + sanitizeSegment(tenant) + "__" + sanitizeSegment(xmlID)
}

// SinkComponentID returns the Vector component ID for a specific sink.
// Format: "<sinkType>_<sanitized_tenant>__<sanitized_xmlid>"
// Example: "aws_s3_wowrack__ds-video"
func SinkComponentID(sinkType, tenant, xmlID string) string {
	return sanitizeSegment(sinkType) + "_" + sanitizeSegment(tenant) + "__" + sanitizeSegment(xmlID)
}
