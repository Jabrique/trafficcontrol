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
 * @param {*} apiIPRule
 * @param {*} $scope
 * @param {import("../../../service/utils/FormUtils")} formUtils
 * @param {import("../../../service/utils/LocationUtils")} locationUtils
 */
var FormAPIIPRuleController = function(apiIPRule, $scope, formUtils, locationUtils) {

	$scope.apiIPRule = apiIPRule || {};

	// Populate CSV string representations for the editable text fields.
	$scope.apiIPRule.httpMethodsStr = Array.isArray(apiIPRule.httpMethods)
		? apiIPRule.httpMethods.join(', ')
		: '';
	$scope.apiIPRule.allowedCidrsStr = Array.isArray(apiIPRule.allowedCidrs)
		? apiIPRule.allowedCidrs.join(', ')
		: '';
	$scope.apiIPRule.deniedCidrsStr = Array.isArray(apiIPRule.deniedCidrs)
		? apiIPRule.deniedCidrs.join(', ')
		: '';

	$scope.navigateToPath = function(path, unsavedChanges) {
		locationUtils.navigateToPath(path, unsavedChanges);
	};

	$scope.hasError = formUtils.hasError;
	$scope.hasPropertyError = formUtils.hasPropertyError;

	/**
	 * Parses a comma-separated string into a cleaned array.
	 *
	 * @param {string|undefined} str
	 * @returns {string[]}
	 */
	function parseCSV(str) {
		if (!str || str.trim() === '') {
			return [];
		}
		return str.split(',').map(function(s) { return s.trim().toUpperCase(); }).filter(Boolean);
	}

	/**
	 * Builds the API payload from the form model, converting CSV strings
	 * back into arrays and normalising HTTP methods to uppercase.
	 *
	 * @param {Object} formRule
	 * @returns {Object}
	 */
	function buildPayload(formRule) {
		return {
			name: formRule.name,
			priority: formRule.priority || 100,
			endpointRegex: formRule.endpointRegex,
			httpMethods: parseCSV(formRule.httpMethodsStr),
			allowedCidrs: parseCSV(formRule.allowedCidrsStr),
			// deniedCidrs: don't uppercase CIDRs
			deniedCidrs: (formRule.deniedCidrsStr || '').split(',')
				.map(function(s) { return s.trim(); }).filter(Boolean),
			appliesToApiToken: !!formRule.appliesToApiToken,
			appliesToSession: !!formRule.appliesToSession,
			active: !!formRule.active
		};
	}

	$scope.save = function(formRule) {
		var payload = buildPayload(formRule);
		$scope.confirmSave(payload);
	};

};

FormAPIIPRuleController.$inject = ['apiIPRule', '$scope', 'formUtils', 'locationUtils'];
module.exports = FormAPIIPRuleController;
