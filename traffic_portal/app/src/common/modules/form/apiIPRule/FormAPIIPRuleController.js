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
 * @param {import("angular").IControllerService} $uibModal
 * @param {import("../../../api/EndpointService")} endpointService
 */
var FormAPIIPRuleController = function(apiIPRule, $scope, formUtils, locationUtils, $uibModal, endpointService) {

	$scope.apiIPRule = apiIPRule || {};

	/** All supported HTTP methods, shown as toggle buttons in the form. */
	$scope.allHttpMethods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];

	/**
	 * Tracks which methods are selected as a plain object for ng-model binding:
	 * { GET: true, POST: false, … }
	 */
	$scope.selectedMethods = {};

	// Populate selectedMethods from existing data (edit mode).
	var existingMethods = Array.isArray(apiIPRule.httpMethods) ? apiIPRule.httpMethods : [];
	$scope.allHttpMethods.forEach(function(m) {
		$scope.selectedMethods[m] = existingMethods.includes(m);
	});

	// Populate CSV string representations for CIDR fields.
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
	 * Opens a browseable list of known API endpoint patterns from api_capabilities.
	 * User picks one → pattern auto-filled into the endpointRegex field.
	 */
	$scope.openEndpointBrowser = function() {
		$uibModal.open({
			templateUrl: 'common/modules/dialog/selectEndpoint/dialog.selectEndpoint.tpl.html',
			controller: 'DialogSelectEndpointController',
			size: 'lg',
			resolve: {
				endpointService: function() { return endpointService; }
			}
		}).result.then(function(selected) {
			if (selected && selected.route) {
				// Escape route pattern for use as a Go regexp.
				// Replace {param} path params with [^/]+ wildcard.
				var pattern = selected.route
					.replace(/\{[^}]+\}/g, '[^/]+')
					.replace(/\./g, '\\.');
				$scope.apiIPRule.endpointRegex = pattern;
			}
		});
	};

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
		return str.split(',').map(function(s) { return s.trim(); }).filter(Boolean);
	}

	/**
	 * Builds the API payload from the form model.
	 * Converts selectedMethods toggle object → string array,
	 * and CIDR CSV strings → arrays.
	 *
	 * @param {Object} formRule
	 * @returns {Object}
	 */
	function buildPayload(formRule) {
		// Collect checked methods in canonical order.
		var methods = $scope.allHttpMethods.filter(function(m) {
			return !!$scope.selectedMethods[m];
		});

		return {
			name: formRule.name,
			description: formRule.description || '',
			priority: formRule.priority || 100,
			endpointRegex: formRule.endpointRegex,
			httpMethods: methods,
			allowedCidrs: parseCSV(formRule.allowedCidrsStr),
			deniedCidrs: parseCSV(formRule.deniedCidrsStr),
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

FormAPIIPRuleController.$inject = ['apiIPRule', '$scope', 'formUtils', 'locationUtils', '$uibModal', 'endpointService'];
module.exports = FormAPIIPRuleController;
