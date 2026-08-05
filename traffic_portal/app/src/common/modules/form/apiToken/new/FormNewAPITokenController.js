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
 * @param {import("../../../../api/APITokenService")} apiTokenService
 * @param {import("../../../../models/MessageModel")} messageModel
 */
var FormNewAPITokenController = function($scope, $controller, locationUtils, apiTokenService, messageModel) {

	// Inherit common form logic from FormAPITokenController.
	angular.extend(this, $controller('FormAPITokenController', { $scope: $scope }));

	$scope.settings = {
		isNew: true,
		saveLabel: 'Create Token'
	};

	/**
	 * Called by the base controller's save() after building the payload.
	 * On success, displays the plaintext secret in the form (once-only display).
	 *
	 * @param {Object} payload
	 */
	$scope.confirmSave = function(payload) {
		apiTokenService.createAPIToken(payload).then(
			function(result) {
				// The response includes the plaintext secret only at creation time.
				var secret = result.data && result.data.response && result.data.response.secret;
				if (secret) {
					$scope.createdSecret = secret;
					messageModel.setMessages(result.data.alerts, false);
				} else {
					messageModel.setMessages(result.data.alerts, true);
					locationUtils.navigateToPath('/user/api-tokens');
				}
			}
		);
	};

};

FormNewAPITokenController.$inject = ['$scope', '$controller', 'locationUtils', 'apiTokenService', 'messageModel'];
module.exports = FormNewAPITokenController;
