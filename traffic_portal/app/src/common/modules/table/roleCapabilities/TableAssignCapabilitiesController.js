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

var TableAssignCapabilitiesController = function(role, capabilities, assignedCapabilities, $scope, $uibModalInstance) {

	var selectedCapabilities = [];

	var addAll = function() {
		markVisibleCapabilities(true);
	};

	var removeAll = function() {
		markVisibleCapabilities(false);
	};

	var markVisibleCapabilities = function(selected) {
		var visibleCapabilityNames = $('#assignCapabilitiesTable tr.cap-row').map(
			function() {
				return $(this).attr('id'); // the cap name is being stored as the id on the row
			}).get();
		$scope.selectedCapabilities = _.map(capabilities, function(c) {
			if (visibleCapabilityNames.includes(c.name)) {
				c['selected'] = selected;
			}
			return c;
		});
		updateSelectedCount();
	};

	var updateSelectedCount = function() {
		selectedCapabilities = _.filter($scope.selectedCapabilities, function(c) { return c['selected'] == true; } );
		$('div.selected-count').html('<b>' + selectedCapabilities.length + ' capabilities selected</b>');
	};

	$scope.role = role;

	// Reset selectedCapabilities completely
	$scope.selectedCapabilities = [];
	
	// Create fresh capabilities array with proper state
	$scope.selectedCapabilities = _.map(capabilities, function(c) {
		// Create a fresh copy of the capability object
		var freshCapability = {
			name: c.name,
			selected: false  // Start with false for all
		};
		
		// Check if this capability is assigned
		var isAssigned = _.find(assignedCapabilities, function(assignedCap) {
			return assignedCap == c.name;
		});
		
		if (isAssigned) {
			freshCapability.selected = true;
		}
		
		return freshCapability;
	});

	$scope.selectAll = function($event) {
		var checkbox = $event.target;
		if (checkbox.checked) {
			addAll();
		} else {
			removeAll();
		}
	};

	$scope.onChange = function() {
		updateSelectedCount();
	};

	$scope.submit = function() {
		var selectedCapabilityNames = _.pluck(selectedCapabilities, 'name');
		// Clean up DataTable before closing
		if ($.fn.DataTable.isDataTable('#assignCapabilitiesTable')) {
			$('#assignCapabilitiesTable').DataTable().destroy();
		}
		$uibModalInstance.close(selectedCapabilityNames);
	};

	$scope.cancel = function () {
		// Clean up DataTable before dismissing
		if ($.fn.DataTable.isDataTable('#assignCapabilitiesTable')) {
			$('#assignCapabilitiesTable').DataTable().destroy();
		}
		$uibModalInstance.dismiss('cancel');
	};

	angular.element(document).ready(function () {
		// Clear any existing DataTable instance first
		if ($.fn.DataTable.isDataTable('#assignCapabilitiesTable')) {
			$('#assignCapabilitiesTable').DataTable().destroy();
		}
		
		// Force update selectedCapabilities based on assignedCapabilities
		$scope.selectedCapabilities = _.map(capabilities, function(c) {
			var freshCapability = {
				name: c.name,
				selected: false
			};
			
			var isAssigned = _.find(assignedCapabilities, function(assignedCap) {
				return assignedCap == c.name;
			});
			
			if (isAssigned) {
				freshCapability.selected = true;
			}
			
			return freshCapability;
		});
		
		var assignCapabilitiesTable = $('#assignCapabilitiesTable').dataTable({
			"scrollY": "60vh",
			"paging": false,
			"order": [[ 1, 'asc' ]],
			"dom": '<"selected-count">frtip',
			"columnDefs": [
				{ 'orderable': false, 'targets': 0 },
				{ "width": "5%", "targets": 0 }
			],
			"stateSave": false,
			"destroy": true
		});
		assignCapabilitiesTable.on( 'search.dt', function () {
			$("#selectAllCB").removeAttr("checked"); // uncheck the all box when filtering
		} );
		
		// Force update checkboxes after DataTable is created
		$scope.$apply(function() {
			$scope.selectedCapabilities.forEach(function(capability) {
				var checkbox = $('#assignCapabilitiesTable tr[id="' + capability.name + '"] input[type="checkbox"]');
				checkbox.prop('checked', capability.selected);
			});
		});
		
		updateSelectedCount();
	});

};

TableAssignCapabilitiesController.$inject = ['role', 'capabilities', 'assignedCapabilities', '$scope', '$uibModalInstance'];
module.exports = TableAssignCapabilitiesController;
