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
 * @param {*} $scope
 * @param {import("../../../service/utils/FormUtils")} formUtils
 * @param {import("../../../service/utils/LocationUtils")} locationUtils
 * @param {import("angular").IControllerService} $uibModal
 * @param {import("../../../api/CapabilityService")} capabilityService
 */
var FormAPITokenController = function($scope, formUtils, locationUtils, $uibModal, capabilityService) {

	$scope.apiToken = $scope.apiToken || {};
	$scope.createdSecret = null;

	$scope.navigateToPath = function(path, unsavedChanges) {
		locationUtils.navigateToPath(path, unsavedChanges);
	};

	$scope.hasError = formUtils.hasError;
	$scope.hasPropertyError = formUtils.hasPropertyError;

	/**
	 * Opens the capability picker modal and merges the chosen capabilities
	 * into the scopedPermissionsStr text field.
	 */
	$scope.openCapabilityBrowser = function() {
		// Parse currently entered permissions so the modal pre-selects them.
		var current = parseCSV($scope.apiToken.scopedPermissionsStr || '');

		capabilityService.getCapabilities().then(function(allCaps) {
			var withSelection = allCaps.map(function(cap) {
				return { name: cap.name, selected: current.includes(cap.name) };
			});

			$uibModal.open({
				templateUrl: 'common/modules/table/roleCapabilities/table.assignCapabilities.tpl.html',
				controller: 'TableAssignCapabilitiesController',
				size: 'lg',
				resolve: {
					role: function() { return { name: 'this token' }; },
					capabilities: function() { return allCaps; },
					assignedCapabilities: function() { return current; }
				}
			}).result.then(function(selectedCapabilities) {
				$scope.apiToken.scopedPermissionsStr = selectedCapabilities.join(', ');
			});
		});
	};

	/**
	 * Parses a comma-separated string of permissions/CIDRs into a clean array,
	 * stripping whitespace and empty entries.
	 *
	 * @param {string|undefined} str
	 * @returns {string[]}
	 */
	function parseCSV(str) {
		if (!str || str.trim() === '') {
			return [];
		}
		return str.split(',').map(function(s) { return s.trim(); }).filter(Boolean);
	}

	/**
	 * Builds the API request payload from the form model, converting
	 * comma-separated strings to arrays and omitting empty optional fields.
	 *
	 * @param {Object} formToken
	 * @returns {Object} payload ready for the API
	 */
	/**
	 * Converts expiryDays (user input) into an ISO-8601 expiresAt timestamp.
	 * Backend requires expiresAt directly — does not accept expiryDays.
	 * @param {number|undefined} days  Number of days from now. Defaults to 90.
	 * @returns {string} ISO-8601 UTC string
	 */
	function daysToExpiresAt(days) {
		var d = new Date();
		d.setDate(d.getDate() + (days > 0 ? parseInt(days, 10) : 90));
		return d.toISOString();
	}

	function buildPayload(formToken) {
		var payload = {
			name: formToken.name,
			// Backend requires expiresAt as a full ISO timestamp.
			expiresAt: daysToExpiresAt(formToken.expiryDays)
		};

		var scoped = parseCSV(formToken.scopedPermissionsStr);
		if (scoped.length > 0) {
			payload.scopedPermissions = scoped;
		}

		var cidrs = parseCSV(formToken.allowedCidrsStr);
		if (cidrs.length > 0) {
			payload.allowedCidrs = cidrs;
		}

		return payload;
	}

	$scope.save = function(formToken) {
		var payload = buildPayload(formToken);
		$scope.confirmSave(payload);
	};

};

FormAPITokenController.$inject = ['$scope', 'formUtils', 'locationUtils', '$uibModal', 'capabilityService'];
module.exports = FormAPITokenController;
