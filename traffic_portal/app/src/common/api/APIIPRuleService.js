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

var APIIPRuleService = function($http, ENV, messageModel) {

	this.getAPIIPRules = function(queryParams) {
		return $http.get(ENV.api.unstable + 'api_ip_rules', { params: queryParams }).then(
			function(result) {
				// API returns null response when there are no rules; normalise to []
				return result.data.response || [];
			},
			function(err) {
				messageModel.setMessages(err.data.alerts, false);
				throw err;
			}
		);
	};

	this.getAPIIPRule = function(id) {
		return $http.get(ENV.api.unstable + 'api_ip_rules/' + id).then(
			function(result) {
				return result.data.response;
			},
			function(err) {
				messageModel.setMessages(err.data.alerts, false);
				throw err;
			}
		);
	};

	this.createAPIIPRule = function(rule) {
		return $http.post(ENV.api.unstable + 'api_ip_rules', rule).then(
			function(result) {
				return result;
			},
			function(err) {
				messageModel.setMessages(err.data.alerts, false);
				throw err;
			}
		);
	};

	this.updateAPIIPRule = function(rule) {
		return $http.put(ENV.api.unstable + 'api_ip_rules/' + rule.id, rule).then(
			function(result) {
				return result;
			},
			function(err) {
				messageModel.setMessages(err.data.alerts, false);
				throw err;
			}
		);
	};

	this.deleteAPIIPRule = function(id) {
		return $http.delete(ENV.api.unstable + 'api_ip_rules/' + id).then(
			function(result) {
				return result;
			},
			function(err) {
				messageModel.setMessages(err.data.alerts, false);
				throw err;
			}
		);
	};

};

APIIPRuleService.$inject = ['$http', 'ENV', 'messageModel'];
module.exports = APIIPRuleService;
