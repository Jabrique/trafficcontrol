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
 * @param {import("angular").IControllerService} $controller
 * @param {import("../../../../service/utils/angular.ui.bootstrap").IModalService} $uibModal
 * @param {import("../../../../service/utils/LocationUtils")} locationUtils
 * @param {import("../../../../api/APIIPRuleService")} apiIPRuleService
 * @param {import("../../../../models/MessageModel")} messageModel
 */
var FormEditAPIIPRuleController = function(apiIPRule, $scope, $controller, $uibModal, locationUtils, apiIPRuleService, messageModel) {

	angular.extend(this, $controller('FormAPIIPRuleController', { apiIPRule: apiIPRule, $scope: $scope }));

	$scope.settings = {
		isNew: false,
		saveLabel: 'Update Rule'
	};

	$scope.confirmSave = function(payload) {
		payload.id = apiIPRule.id;
		apiIPRuleService.updateAPIIPRule(payload).then(function(result) {
			messageModel.setMessages(result.data.alerts, false);
			$scope.$broadcast('asnChanged');
		});
	};

	$scope.confirmDelete = function(rule) {
		var params = {
			title: 'Delete IP Rule?',
			message: 'Are you sure you want to delete the IP rule <strong>' + rule.name + '</strong>?<br><br>' +
				'This may immediately affect API access for matched requests.'
		};
		var modalInstance = $uibModal.open({
			templateUrl: 'common/modules/dialog/confirm/dialog.confirm.tpl.html',
			controller: 'DialogConfirmController',
			size: 'md',
			resolve: {
				params: function() { return params; }
			}
		});
		modalInstance.result.then(function() {
			apiIPRuleService.deleteAPIIPRule(rule.id).then(function(result) {
				messageModel.setMessages(result.data.alerts, true);
				locationUtils.navigateToPath('/api-ip-rules');
			});
		});
	};

};

FormEditAPIIPRuleController.$inject = ['apiIPRule', '$scope', '$controller', '$uibModal', 'locationUtils', 'apiIPRuleService', 'messageModel'];
module.exports = FormEditAPIIPRuleController;
