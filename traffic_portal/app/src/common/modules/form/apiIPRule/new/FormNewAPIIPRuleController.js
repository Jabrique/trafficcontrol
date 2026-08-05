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
 * @param {import("angular").IControllerService} $controller
 * @param {import("../../../../service/utils/LocationUtils")} locationUtils
 * @param {import("../../../../api/APIIPRuleService")} apiIPRuleService
 * @param {import("../../../../models/MessageModel")} messageModel
 */
var FormNewAPIIPRuleController = function($scope, $controller, locationUtils, apiIPRuleService, messageModel) {

	var emptyRule = {
		active: true,
		appliesToApiToken: true,
		appliesToSession: false,
		priority: 100
	};

	angular.extend(this, $controller('FormAPIIPRuleController', { apiIPRule: emptyRule, $scope: $scope }));

	$scope.settings = {
		isNew: true,
		saveLabel: 'Create Rule'
	};

	$scope.confirmSave = function(payload) {
		apiIPRuleService.createAPIIPRule(payload).then(function(result) {
			messageModel.setMessages(result.data.alerts, true);
			locationUtils.navigateToPath('/api-ip-rules');
		});
	};

};

FormNewAPIIPRuleController.$inject = ['$scope', '$controller', 'locationUtils', 'apiIPRuleService', 'messageModel'];
module.exports = FormNewAPIIPRuleController;
