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

/**
 * Dialog for browsing and selecting an API endpoint pattern.
 * Loads live endpoint list from /api/5.0/api_capabilities.
 * User picks one → modal resolves with the selected endpoint object.
 *
 * @param {import("angular").IScope} $scope
 * @param {*} $uibModalInstance
 * @param {import("../../../api/EndpointService")} endpointService
 */
var DialogSelectEndpointController = function($scope, $uibModalInstance, endpointService) {

	/** @type {Array<{route: string, version: string, methods: string[]}>} */
	$scope.endpoints = [];

	/** @type {string} */
	$scope.filter = '';

	/** @type {boolean} */
	$scope.loading = true;

	/** @type {string|null} */
	$scope.error = null;

	// Load endpoints on open.
	endpointService.getEndpoints().then(function(data) {
		// api_capabilities returns [{httpMethod, route, version, ...}]
		// Deduplicate by route — keep all methods for the same route together.
		var routeMap = {};
		(data || []).forEach(function(ep) {
			var key = ep.route || ep.path || ep.endpoint || ep.httpRoute || '';
			if (!key) return;
			if (!routeMap[key]) {
				routeMap[key] = { route: key, methods: [] };
			}
			if (ep.httpMethod && !routeMap[key].methods.includes(ep.httpMethod)) {
				routeMap[key].methods.push(ep.httpMethod);
			}
		});
		$scope.endpoints = Object.values(routeMap).sort(function(a, b) {
			return a.route.localeCompare(b.route);
		});
	}).catch(function() {
		$scope.error = 'Failed to load endpoint list.';
	}).finally(function() {
		$scope.loading = false;
	});

	/**
	 * Returns endpoints filtered by the search string.
	 * @returns {Array}
	 */
	$scope.filteredEndpoints = function() {
		if (!$scope.filter) return $scope.endpoints;
		var q = $scope.filter.toLowerCase();
		return $scope.endpoints.filter(function(ep) {
			return ep.route.toLowerCase().includes(q);
		});
	};

	/**
	 * Selects an endpoint and closes the modal with it.
	 * @param {{route: string, methods: string[]}} endpoint
	 */
	$scope.select = function(endpoint) {
		$uibModalInstance.close(endpoint);
	};

	$scope.cancel = function() {
		$uibModalInstance.dismiss('cancel');
	};

};

DialogSelectEndpointController.$inject = ['$scope', '$uibModalInstance', 'endpointService'];
module.exports = DialogSelectEndpointController;
