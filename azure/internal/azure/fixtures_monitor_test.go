// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/applicationinsights/armapplicationinsights"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/monitor/armmonitor"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"
)

// fixtures_monitor_test.go — hand-built monitoring responses.

const (
	componentID  = rgID + "/providers/Microsoft.Insights/components/ai1"
	diagID       = subID + "/providers/Microsoft.Insights/diagnosticSettings/activity"
	alertID      = rgID + "/providers/Microsoft.Insights/metricAlerts/cpu-high"
	ehAuthRuleID = ehNamespaceID + "/authorizationRules/RootManageSharedAccessKey"
)

func monitoringFixtures() []fixture {
	return []fixture{
		{name: "log analytics workspace", build: func(t *testing.T) subResult {
			return subResult{resources: []resource{fx{t}.res(logAnalyticsWorkspaceResource(logAnalyticsWorkspace()))}}
		}},
		{name: "application insights component", build: func(t *testing.T) subResult {
			comp := insightsComponent()
			return subResult{
				resources: []resource{fx{t}.res(componentResource(comp))},
				edges:     []edge{{from: componentID, to: workspaceID, relation: edgeSinksTo}},
			}
		}},
		{name: "diagnostic setting", build: func(t *testing.T) subResult {
			ds := diagnosticSetting()
			return subResult{
				resources: []resource{fx{t}.res(diagnosticSettingResource(ds))},
				edges:     diagnosticSettingEdges(ds),
			}
		}},
		{name: "metric alert", build: func(t *testing.T) subResult {
			alert := metricAlert()
			return subResult{
				resources: []resource{fx{t}.res(metricAlertResource(alert))},
				edges:     metricAlertEdges(alert),
			}
		}},
	}
}

func logAnalyticsWorkspace() *armoperationalinsights.Workspace {
	return &armoperationalinsights.Workspace{
		ID:       new(workspaceID),
		Name:     new("law1"),
		Location: new("westeurope"),
		Properties: &armoperationalinsights.WorkspaceProperties{
			RetentionInDays: to.Ptr[int32](30),
		},
	}
}

func insightsComponent() *armapplicationinsights.Component {
	return &armapplicationinsights.Component{
		ID:       new(componentID),
		Name:     new("ai1"),
		Location: new("westeurope"),
		Kind:     new("web"),
		Properties: &armapplicationinsights.ComponentProperties{
			ApplicationType:     to.Ptr(armapplicationinsights.ApplicationTypeWeb),
			RetentionInDays:     to.Ptr[int32](90),
			WorkspaceResourceID: new(workspaceID),
			InstrumentationKey:  new("00000000-0000-0000-0000-000000000000"),
			ConnectionString:    new("InstrumentationKey=00000000-0000-0000-0000-000000000000"),
		},
	}
}

// diagnosticSetting routes to THREE destinations at once, which is the shape a
// real activity-log setting usually has.
func diagnosticSetting() *armmonitor.DiagnosticSettingsResource {
	return &armmonitor.DiagnosticSettingsResource{
		ID:   new(diagID),
		Name: new("activity"),
		Properties: &armmonitor.DiagnosticSettings{
			StorageAccountID:            new(storageID),
			WorkspaceID:                 new(workspaceID),
			EventHubAuthorizationRuleID: new(ehAuthRuleID),
			Logs: []*armmonitor.LogSettings{
				{Category: new("Administrative"), Enabled: new(true)},
				{Category: new("Security"), Enabled: new(false)},
			},
			Metrics: []*armmonitor.MetricSettings{
				{Category: new("AllMetrics"), Enabled: new(false)},
			},
		},
	}
}

func metricAlert() *armmonitor.MetricAlertResource {
	return &armmonitor.MetricAlertResource{
		ID:       new(alertID),
		Name:     new("cpu-high"),
		Location: new("global"),
		Properties: &armmonitor.MetricAlertProperties{
			Severity: to.Ptr[int32](2),
			Enabled:  new(true),
			Scopes:   []*string{new(vmID), new(vmssID)},
		},
	}
}

// TestComponentResource_DropsTheTelemetryCredentials. Both the instrumentation
// key and the connection string are credentials for writing telemetry INTO the
// component, and neither says anything about the shape of the subscription.
func TestComponentResource_DropsTheTelemetryCredentials(t *testing.T) {
	r := fx{t}.res(componentResource(insightsComponent()))
	body := string(r.content)
	for _, forbidden := range []string{"instrumentationKey", "InstrumentationKey", "connectionString"} {
		if containsSubstring(body, forbidden) {
			t.Errorf("the stored body carries %q, which is a telemetry credential", forbidden)
		}
	}
	// The known positive for that zero: the field the projection DOES keep is
	// in the body, so the absences above are the projection working rather
	// than an empty body.
	if !containsSubstring(body, "workspaceResourceId") {
		t.Error("the stored body carries no workspace reference, so the absences above prove nothing")
	}
}

// TestDiagnosticSettingEdges_DrawEveryDestination. A setting commonly routes to
// several at once, and a walk reading only the first would report a partial
// answer as complete.
func TestDiagnosticSettingEdges_DrawEveryDestination(t *testing.T) {
	edges := diagnosticSettingEdges(diagnosticSetting())
	for _, want := range []string{storageID, workspaceID, ehAuthRuleID} {
		if _, ok := edgeBetween(edges, diagID, want, edgeSinksTo); !ok {
			t.Errorf("no sink edge to %s", want)
		}
	}
	if len(edges) != 3 {
		t.Errorf("expected three sink edges, got %d", len(edges))
	}

	// The negative: a setting routing nowhere draws nothing.
	empty := &armmonitor.DiagnosticSettingsResource{
		ID: new(diagID), Properties: &armmonitor.DiagnosticSettings{},
	}
	if got := diagnosticSettingEdges(empty); len(got) != 0 {
		t.Errorf("a setting with no destination drew %d edges", len(got))
	}
}

// TestDiagnosticSettingResource_ReportsWhetherAnythingIsEnabled. A setting with
// every category disabled routes nothing, and the flags are what tell a reader
// that without decoding the whole body.
func TestDiagnosticSettingResource_ReportsWhetherAnythingIsEnabled(t *testing.T) {
	r := fx{t}.res(diagnosticSettingResource(diagnosticSetting()))
	if r.metadata["logsEnabled"] != "true" {
		t.Errorf("logsEnabled is %q with one category enabled", r.metadata["logsEnabled"])
	}
	if r.metadata["metricsEnabled"] != "false" {
		t.Errorf("metricsEnabled is %q with every metric category disabled", r.metadata["metricsEnabled"])
	}
}

// TestMetricAlertEdges_WatchEveryScope. One alert watching two resources is
// two relationships.
func TestMetricAlertEdges_WatchEveryScope(t *testing.T) {
	edges := metricAlertEdges(metricAlert())
	for _, want := range []string{vmID, vmssID} {
		if _, ok := edgeBetween(edges, alertID, want, edgeMonitors); !ok {
			t.Errorf("no monitoring edge to %s", want)
		}
	}
	scopeless := metricAlert()
	scopeless.Properties.Scopes = nil
	if got := metricAlertEdges(scopeless); len(got) != 0 {
		t.Errorf("an alert with no scope drew %d edges", len(got))
	}
}
