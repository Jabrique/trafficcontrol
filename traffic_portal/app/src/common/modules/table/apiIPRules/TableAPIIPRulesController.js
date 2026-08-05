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
 * @param {*} apiIPRules
 * @param {*} $scope
 * @param {*} $state
 * @param {import("../../../service/utils/angular.ui.bootstrap").IModalService} $uibModal
 * @param {import("../../../service/utils/LocationUtils")} locationUtils
 * @param {import("../../../api/APIIPRuleService")} apiIPRuleService
 * @param {import("../../../models/MessageModel")} messageModel
 */
var TableAPIIPRulesController = function(apiIPRules, $scope, $state, $uibModal, locationUtils, apiIPRuleService, messageModel) {

	/** @type {CGC.GridSettings} */
	$scope.gridOptions = {
		refreshable: true
	};

	/** @type {import("../agGrid/CommonGridController").CGC.ColumnDefinition} */
	$scope.columns = [
		{
			headerName: 'Priority',
			field: 'priority',
			hide: false,
			filter: 'agNumberColumnFilter',
			width: 90
		},
		{
			headerName: 'Name',
			field: 'name',
			hide: false
		},
		{
			headerName: 'Endpoint Regex',
			field: 'endpointRegex',
			hide: false
		},
		{
			headerName: 'HTTP Methods',
			field: 'httpMethods',
			hide: false,
			valueFormatter: function(params) {
				return Array.isArray(params.value) && params.value.length > 0 ? params.value.join(', ') : '(all)';
			}
		},
		{
			headerName: 'Allowed CIDRs',
			field: 'allowedCidrs',
			hide: false,
			valueFormatter: function(params) {
				return Array.isArray(params.value) && params.value.length > 0 ? params.value.join(', ') : '(any)';
			}
		},
		{
			headerName: 'Denied CIDRs',
			field: 'deniedCidrs',
			hide: false,
			valueFormatter: function(params) {
				return Array.isArray(params.value) && params.value.length > 0 ? params.value.join(', ') : '(none)';
			}
		},
		{
			headerName: 'Applies To Token',
			field: 'appliesToApiToken',
			hide: false,
			valueFormatter: function(params) {
				return params.value ? 'Yes' : 'No';
			}
		},
		{
			headerName: 'Applies To Session',
			field: 'appliesToSession',
			hide: false,
			valueFormatter: function(params) {
				return params.value ? 'Yes' : 'No';
			}
		},
		{
			headerName: 'Active',
			field: 'active',
			hide: false,
			valueFormatter: function(params) {
				return params.value ? 'Yes' : 'No';
			}
		}
	];

	$scope.apiIPRules = apiIPRules;

	/** @type {import("../agGrid/CommonGridController").CGC.DropDownOption[]} */
	$scope.dropDownOptions = [
		{
			name: 'createAPIIPRuleMenuItem',
			onClick: function() {
				$scope.createAPIIPRule();
			},
			text: 'Create IP Rule',
			type: 1
		},
		{
			name: 'testIPRuleMenuItem',
			onClick: function() {
				$scope.openEvaluateDialog();
			},
			text: 'Test IP Rule',
			type: 1
		}
	];

	/** @type {import("../agGrid/CommonGridController").CGC.ContextMenuOption[]} */
	$scope.contextMenuOptions = [
		{
			onClick: function(rule) {
				$scope.editAPIIPRule(rule);
			},
			text: 'Edit IP Rule',
			type: 1
		},
		{
			onClick: function(rule) {
				$scope.confirmDeleteRule(rule);
			},
			text: 'Delete IP Rule',
			type: 1
		}
	];

	$scope.createAPIIPRule = function() {
		locationUtils.navigateToPath('/api-ip-rules/new');
	};

	/**
	 * Opens the "Test IP Rule" evaluation dialog.
	 * Allows admins to test any IP+method+path against the live rule set.
	 */
	$scope.openEvaluateDialog = function() {
		$uibModal.open({
			templateUrl: 'common/modules/dialog/evaluateIPRule/dialog.evaluateIPRule.tpl.html',
			controller: 'DialogEvaluateIPRuleController',
			size: 'md'
		});
	};

	$scope.editAPIIPRule = function(rule) {
		locationUtils.navigateToPath('/api-ip-rules/edit/' + rule.id);
	};

	$scope.refresh = function() {
		$state.reload();
	};

	$scope.confirmDeleteRule = function(rule) {
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
				params: function() {
					return params;
				}
			}
		});
		modalInstance.result.then(function() {
			apiIPRuleService.deleteAPIIPRule(rule.id)
				.then(function(result) {
					messageModel.setMessages(result.data.alerts, false);
					$scope.refresh();
				});
		});
	};

};

TableAPIIPRulesController.$inject = ['apiIPRules', '$scope', '$state', '$uibModal', 'locationUtils', 'apiIPRuleService', 'messageModel'];
module.exports = TableAPIIPRulesController;
