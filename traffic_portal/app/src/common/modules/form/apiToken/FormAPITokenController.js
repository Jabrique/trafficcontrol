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
 */
var FormAPITokenController = function($scope, formUtils, locationUtils) {

	$scope.apiToken = $scope.apiToken || {};
	$scope.createdSecret = null;

	$scope.navigateToPath = function(path, unsavedChanges) {
		locationUtils.navigateToPath(path, unsavedChanges);
	};

	$scope.hasError = formUtils.hasError;
	$scope.hasPropertyError = formUtils.hasPropertyError;

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
	 * Converts expiryDays (user input) into an ISO-8601 expiresAt timestamp
	 * for the backend. Backend requires expiresAt directly — it does not
	 * accept expiryDays.
	 *
	 * @param {number|undefined} days  Number of days from now. Defaults to 90.
	 * @returns {string} ISO-8601 UTC string (e.g. "2026-11-03T04:41:00.000Z")
	 */
	function daysToExpiresAt(days) {
		var d = new Date();
		d.setDate(d.getDate() + (days > 0 ? days : 90));
		return d.toISOString();
	}

	/**
	 * Builds the API request payload from the form model.
	 * Converts expiryDays → expiresAt and CSV strings → arrays.
	 *
	 * @param {Object} formToken
	 * @returns {Object} payload ready for the API
	 */
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

FormAPITokenController.$inject = ['$scope', 'formUtils', 'locationUtils'];
module.exports = FormAPITokenController;
