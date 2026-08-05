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

	/**
	 * Fetches all capabilities from Traffic Ops (GET /api/3.0/capabilities).
	 * The /api/3.0 path is intentional — this endpoint was not ported to later API versions.
	 * Custom capabilities (API-TOKEN:*, API-IP-RULE:*) are available here once the
	 * corresponding DB migrations have been applied.
	 *
	 * @param {Object} [queryParams] - Optional query parameters (e.g. {name: 'FOO:READ'}).
	 * @returns {Promise<Array<{name: string}>>}
	 */
	this.getCapabilities = function(queryParams) {
		return $http.get('/api/3.0/capabilities', {params: queryParams}).then(
			function(result) {
				return result.data.response;
			},
			function(err) {
				messageModel.setMessages(err.data.alerts, false);
				throw err;
			}
		);
	};
};

CapabilityService.$inject = ['$http', 'messageModel', 'ENV'];
module.exports = CapabilityService;
