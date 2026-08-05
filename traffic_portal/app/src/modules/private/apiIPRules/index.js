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

module.exports = angular.module('trafficPortal.private.apiIPRules', [])
	.config(function($stateProvider, $urlRouterProvider) {
		$stateProvider
			.state('trafficPortal.private.apiIPRules', {
				url: 'api-ip-rules',
				abstract: true,
				views: {
					privateContent: {
						template: '<div ui-view="apiIPRulesContent"></div>'
					}
				}
			})
			.state('trafficPortal.private.apiIPRules.list', {
				url: '',
				views: {
					apiIPRulesContent: {
						templateUrl: 'common/modules/table/apiIPRules/table.apiIPRules.tpl.html',
						controller: 'TableAPIIPRulesController',
						resolve: {
							apiIPRules: function(apiIPRuleService) {
								return apiIPRuleService.getAPIIPRules().catch(function() { return []; });
							}
						}
					}
				}
			})
			.state('trafficPortal.private.apiIPRules.new', {
				url: '/new',
				views: {
					apiIPRulesContent: {
						templateUrl: 'common/modules/form/apiIPRule/form.apiIPRule.tpl.html',
						controller: 'FormNewAPIIPRuleController'
					}
				}
			})
			.state('trafficPortal.private.apiIPRules.edit', {
				url: '/edit/:id',
				views: {
					apiIPRulesContent: {
						templateUrl: 'common/modules/form/apiIPRule/form.apiIPRule.tpl.html',
						controller: 'FormEditAPIIPRuleController',
						resolve: {
							apiIPRule: function($stateParams, apiIPRuleService) {
								return apiIPRuleService.getAPIIPRule($stateParams.id);
							}
						}
					}
				}
			})
		;
		$urlRouterProvider.otherwise('/');
	});
