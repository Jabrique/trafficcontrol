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
		var visibleCapabilityNames = $('#assignCapabilitiesTable tr.cap-row:visible').map(
			function() {
				return $(this).attr('id'); // the cap name is being stored as the id on the row
			}).get();
		$scope.selectedCapabilities = _.map($scope.selectedCapabilities, function(c) {
			if (visibleCapabilityNames.includes(c.name)) {
				c.selected = selected;
			}
			return c;
		});
		// Force update the checkboxes in the DOM
		$scope.selectedCapabilities.forEach(function(capability) {
			if (visibleCapabilityNames.includes(capability.name)) {
				var checkbox = $('#assignCapabilitiesTable tr[id="' + capability.name + '"] input[type="checkbox"]');
				checkbox.prop('checked', capability.selected);
			}
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
		// Sync checkbox states from DOM back to scope before submitting
		$scope.selectedCapabilities.forEach(function(capability) {
			var checkbox = $('#assignCapabilitiesTable tr[id="' + capability.name + '"] input[type="checkbox"]');
			if (checkbox.length > 0) {
				capability.selected = checkbox.prop('checked');
			}
		});
		
		// Update selectedCapabilities with the latest state before submitting
		updateSelectedCount();
		// Get the names from ALL selected capabilities (not just visible ones)
		var selectedCapabilityNames = _.pluck(_.filter($scope.selectedCapabilities, function(c) { 
			return c.selected === true; 
		}), 'name');
		
		// Clean up event handlers and DataTable before closing
		$('#assignCapabilitiesTable').off('click', 'tbody tr.cap-row');
		if ($.fn.DataTable.isDataTable('#assignCapabilitiesTable')) {
			$('#assignCapabilitiesTable').DataTable().destroy();
		}
		$uibModalInstance.close(selectedCapabilityNames);
	};

	$scope.cancel = function () {
		// Clean up event handlers and DataTable before dismissing
		$('#assignCapabilitiesTable').off('click', 'tbody tr.cap-row');
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
		
		// Add event delegation for row clicks to handle filtered results
		$('#assignCapabilitiesTable').on('click', 'tbody tr.cap-row', function(e) {
			// Don't trigger if clicking on checkbox directly
			if ($(e.target).is('input[type="checkbox"]')) {
				return;
			}
			
			var capabilityName = $(this).data('capability-name');
			var capability = _.find($scope.selectedCapabilities, function(c) {
				return c.name === capabilityName;
			});
			
			if (capability) {
				$scope.$apply(function() {
					capability.selected = !capability.selected;
					// Update the corresponding checkbox
					var checkbox = $(e.currentTarget).find('input[type="checkbox"]');
					checkbox.prop('checked', capability.selected);
					updateSelectedCount();
				});
			}
		});
		
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
