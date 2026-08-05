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
 * Controller for the "Test IP Rule" dialog.
 * Accepts {ip, method, path, authType} and calls the evaluate endpoint.
 *
 * @param {import("angular").IScope} $scope
 * @param {import("angular").IControllerService} $uibModalInstance
 * @param {import("../../../api/APIIPRuleService")} apiIPRuleService
 */
var DialogEvaluateIPRuleController = function($scope, $uibModalInstance, apiIPRuleService) {

	/** @type {string[]} All HTTP methods the UI supports selecting */
	$scope.httpMethods = ['GET', 'POST', 'PUT', 'DELETE', 'PATCH', 'HEAD', 'OPTIONS'];

	$scope.form = {
		ip: '',
		method: 'GET',
		path: '',
		authType: 'token'
	};

	/** @type {{allowed: boolean, matchedRule: string, message: string}|null} */
	$scope.result = null;

	/** @type {boolean} */
	$scope.loading = false;

	/** @type {string|null} */
	$scope.error = null;

	/**
	 * Runs the evaluation against the live IP rule set.
	 * Calls POST /api/5.0/api_ip_rules/evaluate and populates $scope.result.
	 */
	$scope.evaluate = function() {
		if (!$scope.form.ip || !$scope.form.method || !$scope.form.path) {
			$scope.error = 'IP address, HTTP method, and path are required.';
			return;
		}

		$scope.error = null;
		$scope.result = null;
		$scope.loading = true;

		apiIPRuleService.evaluateIPRule({
			ip: $scope.form.ip.trim(),
			method: $scope.form.method,
			path: $scope.form.path.trim(),
			authType: $scope.form.authType
		}).then(function(res) {
			$scope.result = res;
		}).catch(function(err) {
			$scope.error = 'Evaluation failed. Check console for details.';
		}).finally(function() {
			$scope.loading = false;
		});
	};

	$scope.cancel = function() {
		$uibModalInstance.dismiss('cancel');
	};

};

DialogEvaluateIPRuleController.$inject = ['$scope', '$uibModalInstance', 'apiIPRuleService'];
module.exports = DialogEvaluateIPRuleController;
