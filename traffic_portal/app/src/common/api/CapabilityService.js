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

var CapabilityService = function($http, messageModel, ENV) {

	// Complete list of all capabilities/permissions from database
	// Extracted from actual role_capability table data
	var commonCapabilities = [
		// Core permissions
		{name: "ALL"},
		
		// ACME
		{name: "ACME:READ"},
		{name: "ACME:CREATE"},
		{name: "ACME:UPDATE"},
		{name: "ACME:DELETE"},
		
		// ASN
		{name: "ASN:READ"},
		{name: "ASN:CREATE"},
		{name: "ASN:UPDATE"},
		{name: "ASN:DELETE"},
		
		// ASYNC-STATUS
		{name: "ASYNC-STATUS:READ"},
		
		// CACHE-GROUP
		{name: "CACHE-GROUP:READ"},
		{name: "CACHE-GROUP:CREATE"},
		{name: "CACHE-GROUP:UPDATE"},
		{name: "CACHE-GROUP:DELETE"},
		
		// CAPABILITY
		{name: "CAPABILITY:READ"},
		
		// DBDUMP
		{name: "DBDUMP:READ"},
		
		// CDN
		{name: "CDN:READ"},
		{name: "CDN:CREATE"},
		{name: "CDN:UPDATE"},
		{name: "CDN:DELETE"},
		
		// CDN-LOCK
		{name: "CDN-LOCK:CREATE"},
		{name: "CDN-LOCK:DELETE"},
		
		// CDN-SNAPSHOT
		{name: "CDN-SNAPSHOT:READ"},
		{name: "CDN-SNAPSHOT:CREATE"},
		
		// CDNI-ADMIN
		{name: "CDNI-ADMIN:READ"},
		
		// CDNI-CAPACITY
		{name: "CDNI-CAPACITY:READ"},
		
		// COORDINATE
		{name: "COORDINATE:READ"},
		{name: "COORDINATE:CREATE"},
		{name: "COORDINATE:UPDATE"},
		{name: "COORDINATE:DELETE"},
		
		// DELIVERY-SERVICE
		{name: "DELIVERY-SERVICE:READ"},
		{name: "DELIVERY-SERVICE:CREATE"},
		{name: "DELIVERY-SERVICE:UPDATE"},
		{name: "DELIVERY-SERVICE:DELETE"},
		
		// DELIVERY-SERVICE-SAFE
		{name: "DELIVERY-SERVICE-SAFE:UPDATE"},
		
		// DIVISION
		{name: "DIVISION:READ"},
		{name: "DIVISION:CREATE"},
		{name: "DIVISION:UPDATE"},
		{name: "DIVISION:DELETE"},
		
		// DNS-SEC
		{name: "DNS-SEC:READ"},
		{name: "DNS-SEC:CREATE"},
		{name: "DNS-SEC:UPDATE"},
		{name: "DNS-SEC:DELETE"},
		
		// DS-REQUEST
		{name: "DS-REQUEST:READ"},
		{name: "DS-REQUEST:CREATE"},
		{name: "DS-REQUEST:UPDATE"},
		{name: "DS-REQUEST:DELETE"},
		
		// DS-SECURITY-KEY
		{name: "DS-SECURITY-KEY:READ"},
		
		// FEDERATION
		{name: "FEDERATION:READ"},
		{name: "FEDERATION:CREATE"},
		{name: "FEDERATION:UPDATE"},
		{name: "FEDERATION:DELETE"},
		
		// FEDERATION-RESOLVER
		{name: "FEDERATION-RESOLVER:READ"},
		{name: "FEDERATION-RESOLVER:CREATE"},
		{name: "FEDERATION-RESOLVER:DELETE"},
		
		// ISO
		{name: "ISO:READ"},
		{name: "ISO:GENERATE"},
		
		// JOB
		{name: "JOB:READ"},
		{name: "JOB:CREATE"},
		{name: "JOB:UPDATE"},
		{name: "JOB:DELETE"},
		
		// LOG
		{name: "LOG:READ"},
		
		// MONITOR-CONFIG
		{name: "MONITOR-CONFIG:READ"},
		
		// ORIGIN
		{name: "ORIGIN:READ"},
		{name: "ORIGIN:CREATE"},
		{name: "ORIGIN:UPDATE"},
		{name: "ORIGIN:DELETE"},
		
		// PARAMETER
		{name: "PARAMETER:READ"},
		{name: "PARAMETER:CREATE"},
		{name: "PARAMETER:UPDATE"},
		{name: "PARAMETER:DELETE"},
		
		// PHYSICAL-LOCATION
		{name: "PHYSICAL-LOCATION:READ"},
		{name: "PHYSICAL-LOCATION:CREATE"},
		{name: "PHYSICAL-LOCATION:UPDATE"},
		{name: "PHYSICAL-LOCATION:DELETE"},
		
		// PLUGIN
		{name: "PLUGIN:READ"},
		
		// PROFILE
		{name: "PROFILE:READ"},
		{name: "PROFILE:CREATE"},
		{name: "PROFILE:UPDATE"},
		{name: "PROFILE:DELETE"},
		
		// REGION
		{name: "REGION:READ"},
		{name: "REGION:CREATE"},
		{name: "REGION:UPDATE"},
		{name: "REGION:DELETE"},
		
		// ROLE
		{name: "ROLE:READ"},
		{name: "ROLE:CREATE"},
		{name: "ROLE:UPDATE"},
		{name: "ROLE:DELETE"},
		
		// SECURE-SERVER
		{name: "SECURE-SERVER:READ"},
		
		// SERVER
		{name: "SERVER:READ"},
		{name: "SERVER:CREATE"},
		{name: "SERVER:UPDATE"},
		{name: "SERVER:DELETE"},
		{name: "SERVER:QUEUE"},
		
		// SERVER-CAPABILITY
		{name: "SERVER-CAPABILITY:READ"},
		{name: "SERVER-CAPABILITY:CREATE"},
		{name: "SERVER-CAPABILITY:UPDATE"},
		{name: "SERVER-CAPABILITY:DELETE"},
		
		// SERVER-CHECK
		{name: "SERVER-CHECK:READ"},
		{name: "SERVER-CHECK:CREATE"},
		{name: "SERVER-CHECK:DELETE"},
		
		// SERVICE-CATEGORY
		{name: "SERVICE-CATEGORY:READ"},
		{name: "SERVICE-CATEGORY:CREATE"},
		{name: "SERVICE-CATEGORY:UPDATE"},
		{name: "SERVICE-CATEGORY:DELETE"},
		
		// SSL-KEY-EXPIRATION
		{name: "SSL-KEY-EXPIRATION:READ"},
		
		// STATIC-DN
		{name: "STATIC-DN:READ"},
		{name: "STATIC-DN:CREATE"},
		{name: "STATIC-DN:UPDATE"},
		{name: "STATIC-DN:DELETE"},
		
		// STAT
		{name: "STAT:READ"},
		{name: "STAT:CREATE"},
		
		// STATUS
		{name: "STATUS:READ"},
		{name: "STATUS:CREATE"},
		{name: "STATUS:UPDATE"},
		{name: "STATUS:DELETE"},
		
		// STEERING
		{name: "STEERING:READ"},
		{name: "STEERING:CREATE"},
		{name: "STEERING:UPDATE"},
		{name: "STEERING:DELETE"},
		
		// TENANT
		{name: "TENANT:READ"},
		{name: "TENANT:CREATE"},
		{name: "TENANT:UPDATE"},
		{name: "TENANT:DELETE"},
		
		// TOPOLOGY
		{name: "TOPOLOGY:READ"},
		{name: "TOPOLOGY:CREATE"},
		{name: "TOPOLOGY:UPDATE"},
		{name: "TOPOLOGY:DELETE"},
		
		// TRAFFIC-VAULT
		{name: "TRAFFIC-VAULT:READ"},
		
		// TYPE
		{name: "TYPE:READ"},
		{name: "TYPE:CREATE"},
		{name: "TYPE:UPDATE"},
		{name: "TYPE:DELETE"},
		
		// USER
		{name: "USER:READ"},
		{name: "USER:CREATE"},
		{name: "USER:UPDATE"},
		{name: "USER:DELETE"}
	];

	this.getCapabilities = function(queryParams) {
		// Use hardcoded list for better performance and reliability
		// This is the recommended approach for Traffic Control permissions
		return Promise.resolve(commonCapabilities);
	};
};

CapabilityService.$inject = ['$http', 'messageModel', 'ENV'];
module.exports = CapabilityService;
