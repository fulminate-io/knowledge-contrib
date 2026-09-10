// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/applicationinsights/armapplicationinsights"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/monitor/armmonitor"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/operationalinsights/armoperationalinsights"
)

// sub_monitor.go — where telemetry lands and what watches it.

// monitoringSub walks the subscription's Log Analytics workspaces, Application
// Insights components and subscription-level diagnostic settings.
type monitoringSub struct{ subBase }

func (s *monitoringSub) Collect(ctx context.Context) (subResult, error) {
	var out subResult
	if err := s.collectWorkspaces(ctx, &out); err != nil {
		return out, err
	}
	if err := s.collectComponents(ctx, &out); err != nil {
		return out, err
	}
	if err := s.collectDiagnosticSettings(ctx, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (s *monitoringSub) collectWorkspaces(ctx context.Context, out *subResult) error {
	client, err := armoperationalinsights.NewWorkspacesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return fmt.Errorf("log analytics workspaces client: %w", err)
	}
	err = drain(ctx, client.NewListPager(nil), func(page armoperationalinsights.WorkspacesClientListResponse) error {
		for _, ws := range page.Value {
			if ws == nil || ws.ID == nil {
				continue
			}
			r, err := logAnalyticsWorkspaceResource(ws)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing log analytics workspaces: %w", err)
	}
	return nil
}

// componentContent is the curated projection stored on an Application Insights
// node.
//
// THE INSTRUMENTATION KEY AND CONNECTION STRING ARE DELIBERATELY ABSENT. Both
// are credentials for writing telemetry into the component, and neither says
// anything about the shape of the subscription.
type componentContent struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Location   string              `json:"location,omitempty"`
	Kind       string              `json:"kind,omitempty"`
	Properties componentProperties `json:"properties"`
}

type componentProperties struct {
	ApplicationType     string `json:"applicationType,omitempty"`
	RetentionInDays     *int32 `json:"retentionInDays,omitempty"`
	WorkspaceResourceID string `json:"workspaceResourceId,omitempty"`
}

func (s *monitoringSub) collectComponents(ctx context.Context, out *subResult) error {
	client, err := armapplicationinsights.NewComponentsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return fmt.Errorf("application insights client: %w", err)
	}
	err = drain(ctx, client.NewListPager(nil), func(page armapplicationinsights.ComponentsClientListResponse) error {
		for _, comp := range page.Value {
			if comp == nil || comp.ID == nil {
				continue
			}
			r, err := componentResource(comp)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			if comp.Properties != nil {
				if ws := ptr(comp.Properties.WorkspaceResourceID); ws != "" {
					out.edges = append(out.edges, edge{from: ptr(comp.ID), to: ws, relation: edgeSinksTo})
				}
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing application insights components: %w", err)
	}
	return nil
}

func componentResource(comp *armapplicationinsights.Component) (resource, error) {
	proj := componentContent{
		ID:       ptr(comp.ID),
		Name:     ptr(comp.Name),
		Location: ptr(comp.Location),
		Kind:     ptr(comp.Kind),
	}
	if p := comp.Properties; p != nil {
		if p.ApplicationType != nil {
			proj.Properties.ApplicationType = string(*p.ApplicationType)
		}
		if p.RetentionInDays != nil {
			v := *p.RetentionInDays
			proj.Properties.RetentionInDays = &v
		}
		proj.Properties.WorkspaceResourceID = ptr(p.WorkspaceResourceID)
	}
	content, err := marshalContent(proj)
	if err != nil {
		return resource{}, fmt.Errorf("projecting application insights component %s: %w", ptr(comp.ID), err)
	}
	r := resource{
		id:           proj.ID,
		name:         proj.Name,
		resourceType: rtAppInsights,
		region:       proj.Location,
		content:      content,
		metadata:     map[string]string{},
	}
	setIfNotEmpty(r.metadata, "kind", proj.Kind)
	setIfNotEmpty(r.metadata, "applicationType", proj.Properties.ApplicationType)
	if proj.Properties.RetentionInDays != nil {
		r.metadata["retentionInDays"] = strconv.Itoa(int(*proj.Properties.RetentionInDays))
	}
	return r, nil
}

// collectDiagnosticSettings reads the settings routing the subscription's
// activity log.
//
// PER-RESOURCE SETTINGS ARE NOT COLLECTED, and the reason is cost: reading them
// takes one call per resource in the subscription, which for a large one is
// thousands of calls for a relationship that is nearly always configured once
// at the subscription.
func (s *monitoringSub) collectDiagnosticSettings(ctx context.Context, out *subResult) error {
	client, err := armmonitor.NewDiagnosticSettingsClient(s.cred, s.clientOptions())
	if err != nil {
		return fmt.Errorf("diagnostic settings client: %w", err)
	}
	scope := "/subscriptions/" + s.subscriptionID
	err = drain(ctx, client.NewListPager(scope, nil), func(page armmonitor.DiagnosticSettingsClientListResponse) error {
		for _, ds := range page.Value {
			if ds == nil || ds.ID == nil {
				continue
			}
			r, err := diagnosticSettingResource(ds)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, diagnosticSettingEdges(ds)...)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing the subscription's diagnostic settings: %w", err)
	}
	return nil
}

func diagnosticSettingResource(ds *armmonitor.DiagnosticSettingsResource) (resource, error) {
	content, err := marshalContent(ds)
	if err != nil {
		return resource{}, fmt.Errorf("projecting diagnostic setting %s: %w", ptr(ds.ID), err)
	}
	r := resource{
		id:           ptr(ds.ID),
		name:         ptr(ds.Name),
		resourceType: rtDiagnosticSetting,
		content:      content,
		metadata:     map[string]string{},
	}
	if p := ds.Properties; p != nil {
		setIfNotEmpty(r.metadata, "logAnalyticsDestinationType", ptr(p.LogAnalyticsDestinationType))
		r.metadata["logsEnabled"] = strconv.FormatBool(anyLogEnabled(p.Logs))
		r.metadata["metricsEnabled"] = strconv.FormatBool(anyMetricEnabled(p.Metrics))
	}
	return r, nil
}

// diagnosticSettingEdges draws every destination the setting routes to. A
// setting commonly has more than one.
func diagnosticSettingEdges(ds *armmonitor.DiagnosticSettingsResource) []edge {
	if ds.Properties == nil {
		return nil
	}
	id := ptr(ds.ID)
	var out []edge
	for _, target := range []string{
		ptr(ds.Properties.StorageAccountID),
		ptr(ds.Properties.WorkspaceID),
		ptr(ds.Properties.EventHubAuthorizationRuleID),
		ptr(ds.Properties.MarketplacePartnerID),
	} {
		if target == "" {
			continue
		}
		out = append(out, edge{from: id, to: target, relation: edgeSinksTo})
	}
	return out
}

func anyLogEnabled(logs []*armmonitor.LogSettings) bool {
	for _, l := range logs {
		if l != nil && l.Enabled != nil && *l.Enabled {
			return true
		}
	}
	return false
}

func anyMetricEnabled(metrics []*armmonitor.MetricSettings) bool {
	for _, m := range metrics {
		if m != nil && m.Enabled != nil && *m.Enabled {
			return true
		}
	}
	return false
}

// metricAlertSub walks the subscription's metric alerts.
type metricAlertSub struct{ subBase }

func (s *metricAlertSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armmonitor.NewMetricAlertsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("metric alerts client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil), func(page armmonitor.MetricAlertsClientListBySubscriptionResponse) error {
		for _, alert := range page.Value {
			if alert == nil || alert.ID == nil {
				continue
			}
			r, err := metricAlertResource(alert)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, metricAlertEdges(alert)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing metric alerts: %w", err)
	}
	return out, nil
}

func metricAlertResource(alert *armmonitor.MetricAlertResource) (resource, error) {
	content, err := marshalContent(alert)
	if err != nil {
		return resource{}, fmt.Errorf("projecting metric alert %s: %w", ptr(alert.ID), err)
	}
	r := resource{
		id:           ptr(alert.ID),
		name:         ptr(alert.Name),
		resourceType: rtMetricAlert,
		region:       ptr(alert.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := alert.Properties; p != nil {
		if p.Severity != nil {
			r.metadata["severity"] = strconv.Itoa(int(*p.Severity))
		}
		if p.Enabled != nil {
			r.metadata["enabled"] = strconv.FormatBool(*p.Enabled)
		}
	}
	return r, nil
}

// metricAlertEdges draws what the alert watches. An alert's scope is a list, so
// one alert can watch several resources.
func metricAlertEdges(alert *armmonitor.MetricAlertResource) []edge {
	if alert.Properties == nil {
		return nil
	}
	id := ptr(alert.ID)
	var out []edge
	for _, scope := range derefStrings(alert.Properties.Scopes) {
		out = append(out, edge{from: id, to: scope, relation: edgeMonitors})
	}
	return out
}

// logAnalyticsWorkspaceResource converts one Log Analytics workspace.
func logAnalyticsWorkspaceResource(ws *armoperationalinsights.Workspace) (resource, error) {
	r, err := entityResource(ptr(ws.ID), ptr(ws.Name), rtLogAnalytics, ptr(ws.Location), ws)
	if err != nil {
		return resource{}, err
	}
	if p := ws.Properties; p != nil {
		if p.RetentionInDays != nil {
			r.metadata["retentionInDays"] = strconv.Itoa(int(*p.RetentionInDays))
		}
		if p.ProvisioningState != nil {
			r.metadata["provisioningState"] = string(*p.ProvisioningState)
		}
	}
	return r, nil
}
