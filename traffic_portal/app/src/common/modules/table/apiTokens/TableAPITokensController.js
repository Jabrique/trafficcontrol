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
 * @param {*} apiTokens
 * @param {*} $scope
 * @param {*} $state
 * @param {import("../../../service/utils/angular.ui.bootstrap").IModalService} $uibModal
 * @param {import("../../../service/utils/LocationUtils")} locationUtils
 * @param {import("../../../api/APITokenService")} apiTokenService
 * @param {import("../../../models/MessageModel")} messageModel
 */
var TableAPITokensController = function(apiTokens, $scope, $state, $uibModal, locationUtils, apiTokenService, messageModel) {

	/** @type {CGC.GridSettings} */
	$scope.gridOptions = {
		refreshable: true
	};

	/** @type {import("../agGrid/CommonGridController").CGC.ColumnDefinition} */
	$scope.columns = [
		{
			headerName: 'Name',
			field: 'name',
			hide: false
		},
		{
			headerName: 'Public ID',
			field: 'publicId',
			hide: false
		},
		{
			headerName: 'Scoped Permissions',
			field: 'scopedPermissions',
			hide: false,
			valueFormatter: function(params) {
				return Array.isArray(params.value) ? params.value.join(', ') : (params.value || '(all)');
			}
		},
		{
			headerName: 'Allowed CIDRs',
			field: 'allowedCidrs',
			hide: false,
			valueFormatter: function(params) {
				return Array.isArray(params.value) ? params.value.join(', ') : (params.value || '(any)');
			}
		},
		{
			headerName: 'Expires At (UTC)',
			field: 'expiresAt',
			hide: false,
			filter: 'agDateColumnFilter'
		},
		{
			headerName: 'Last Used At (UTC)',
			field: 'lastUsedAt',
			hide: false,
			filter: 'agDateColumnFilter'
		},
		{
			headerName: 'Created At (UTC)',
			field: 'createdAt',
			hide: false,
			filter: 'agDateColumnFilter'
		}
	];

	$scope.apiTokens = (apiTokens || []).map(function(t) {
		t.expiresAt = t.expiresAt ? new Date(t.expiresAt) : null;
		t.lastUsedAt = t.lastUsedAt ? new Date(t.lastUsedAt) : null;
		t.createdAt = t.createdAt ? new Date(t.createdAt) : null;
		return t;
	});

	/** @type {import("../agGrid/CommonGridController").CGC.DropDownOption[]} */
	$scope.dropDownOptions = [{
		name: 'createAPITokenMenuItem',
		onClick: function() {
			$scope.createAPIToken();
		},
		text: 'Create API Token',
		type: 1
	}];

	/** @type {import("../agGrid/CommonGridController").CGC.ContextMenuOption[]} */
	$scope.contextMenuOptions = [{
		onClick: function(token) {
			$scope.confirmRevokeToken(token);
		},
		text: 'Revoke API Token',
		type: 1
	}];

	$scope.createAPIToken = function() {
		locationUtils.navigateToPath('/user/api-tokens/new');
	};

	$scope.refresh = function() {
		$state.reload();
	};

	$scope.confirmRevokeToken = function(token) {
		var params = {
			title: 'Revoke API Token?',
			message: 'Are you sure you want to revoke the token <strong>' + token.name + '</strong>?<br><br>' +
				'Any scripts or integrations using this token will immediately lose access.'
		};
		var modalInstance = $uibModal.open({
			templateUrl: 'common/modules/dialog/confirm/dialog.confirm.tpl.html',
			controller: 'DialogConfirmController',
			size: 'md',
			resolve: {
				params: function() {
					return params;
				}
			}
		});
		modalInstance.result.then(function() {
			apiTokenService.deleteAPIToken(token.id)
				.then(function(result) {
					messageModel.setMessages(result.data.alerts, false);
					$scope.refresh();
				});
		});
	};

};

TableAPITokensController.$inject = ['apiTokens', '$scope', '$state', '$uibModal', 'locationUtils', 'apiTokenService', 'messageModel'];
module.exports = TableAPITokensController;
