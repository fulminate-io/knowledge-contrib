// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// sub_network_perimeter.go — the remaining edge-of-network resources: NAT
// gateways, private endpoints and flow logs. They are split from their siblings
// in sub_network_edge.go by file size alone; nothing distinguishes them.

// natGatewaySub walks the subscription's NAT gateways.
type natGatewaySub struct{ subBase }

func (s *natGatewaySub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewNatGatewaysClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("nat gateways client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armnetwork.NatGatewaysClientListAllResponse) error {
		for _, ng := range page.Value {
			if ng == nil || ng.ID == nil {
				continue
			}
			r, err := natGatewayResource(ng)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, natGatewayEdges(ng)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing nat gateways: %w", err)
	}
	return out, nil
}

func natGatewayResource(ng *armnetwork.NatGateway) (resource, error) {
	content, err := marshalContent(ng)
	if err != nil {
		return resource{}, fmt.Errorf("projecting nat gateway %s: %w", ptr(ng.ID), err)
	}
	r := resource{
		id:           ptr(ng.ID),
		name:         ptr(ng.Name),
		resourceType: rtNATGateway,
		region:       ptr(ng.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if ng.SKU != nil && ng.SKU.Name != nil {
		r.metadata["skuName"] = string(*ng.SKU.Name)
	}
	if p := ng.Properties; p != nil {
		if p.IdleTimeoutInMinutes != nil {
			r.metadata["idleTimeoutInMinutes"] = strconv.Itoa(int(*p.IdleTimeoutInMinutes))
		}
		if p.ProvisioningState != nil {
			r.metadata["provisioningState"] = string(*p.ProvisioningState)
		}
	}
	return r, nil
}

// natGatewayEdges draws the subnets the gateway serves. The list is a read-only
// back-reference Azure fills in from each subnet's own setting, so it is the
// gateway's view of a relationship configured on the other side.
func natGatewayEdges(ng *armnetwork.NatGateway) []edge {
	if ng.Properties == nil {
		return nil
	}
	id := ptr(ng.ID)
	var out []edge
	seen := map[string]bool{}
	for _, sub := range ng.Properties.Subnets {
		if sub == nil {
			continue
		}
		for _, e := range subnetEdges(id, ptr(sub.ID)) {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			out = append(out, e)
		}
	}
	return out
}

// privateEndpointSub walks the subscription's private endpoints.
type privateEndpointSub struct{ subBase }

func (s *privateEndpointSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewPrivateEndpointsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("private endpoints client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil), func(page armnetwork.PrivateEndpointsClientListBySubscriptionResponse) error {
		for _, pe := range page.Value {
			if pe == nil || pe.ID == nil {
				continue
			}
			r, err := privateEndpointResource(pe)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, privateEndpointEdges(pe)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing private endpoints: %w", err)
	}
	return out, nil
}

func privateEndpointResource(pe *armnetwork.PrivateEndpoint) (resource, error) {
	content, err := marshalContent(pe)
	if err != nil {
		return resource{}, fmt.Errorf("projecting private endpoint %s: %w", ptr(pe.ID), err)
	}
	return resource{
		id:           ptr(pe.ID),
		name:         ptr(pe.Name),
		resourceType: rtPrivateEndpoint,
		region:       ptr(pe.Location),
		content:      content,
		metadata:     map[string]string{},
	}, nil
}

// privateEndpointEdges draws the subnet the endpoint occupies and the service
// it fronts. BOTH connection lists are read: an endpoint approved by the
// service owner and one awaiting manual approval front the same service, and
// reading only the automatic list would miss every endpoint into another
// subscription.
func privateEndpointEdges(pe *armnetwork.PrivateEndpoint) []edge {
	if pe.Properties == nil {
		return nil
	}
	id := ptr(pe.ID)
	var out []edge
	if pe.Properties.Subnet != nil {
		if subnetID := ptr(pe.Properties.Subnet.ID); subnetID != "" {
			out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
		}
	}
	links := append(append([]*armnetwork.PrivateLinkServiceConnection(nil),
		pe.Properties.PrivateLinkServiceConnections...),
		pe.Properties.ManualPrivateLinkServiceConnections...)
	for _, conn := range links {
		if conn == nil || conn.Properties == nil {
			continue
		}
		if target := ptr(conn.Properties.PrivateLinkServiceID); target != "" {
			out = append(out, edge{from: id, to: target, relation: edgeTargets})
		}
	}
	return out
}

// flowLogSub walks the subscription's flow logs, through the network watchers
// that own them.
//
// WATCHERS ARE NOT EMITTED AS NODES. Azure provisions one per region
// automatically, they carry no relationship an operator configured, and a node
// per region would be noise in every graph.
type flowLogSub struct{ subBase }

func (s *flowLogSub) Collect(ctx context.Context) (subResult, error) {
	watchers, err := armnetwork.NewWatchersClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("network watchers client: %w", err)
	}
	flowLogs, err := armnetwork.NewFlowLogsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("flow logs client: %w", err)
	}

	var out subResult
	err = drain(ctx, watchers.NewListAllPager(nil), func(page armnetwork.WatchersClientListAllResponse) error {
		for _, w := range page.Value {
			if w == nil || w.ID == nil || w.Name == nil {
				continue
			}
			rg := armResourceGroup(*w.ID)
			if rg == "" {
				continue
			}
			err := drain(ctx, flowLogs.NewListPager(rg, *w.Name, nil),
				func(fp armnetwork.FlowLogsClientListResponse) error {
					for _, fl := range fp.Value {
						if fl == nil || fl.ID == nil {
							continue
						}
						r, err := flowLogResource(fl)
						if err != nil {
							return err
						}
						out.resources = append(out.resources, r)
						out.edges = append(out.edges, flowLogEdges(fl)...)
					}
					return nil
				})
			if err != nil {
				return fmt.Errorf("listing flow logs of watcher %s: %w", *w.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing network watchers: %w", err)
	}
	return out, nil
}

func flowLogResource(fl *armnetwork.FlowLog) (resource, error) {
	content, err := marshalContent(fl)
	if err != nil {
		return resource{}, fmt.Errorf("projecting flow log %s: %w", ptr(fl.ID), err)
	}
	r := resource{
		id:           ptr(fl.ID),
		name:         ptr(fl.Name),
		resourceType: rtFlowLog,
		region:       ptr(fl.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := fl.Properties; p != nil {
		if p.Enabled != nil {
			r.metadata["enabled"] = strconv.FormatBool(*p.Enabled)
		}
		if p.RetentionPolicy != nil && p.RetentionPolicy.Days != nil {
			r.metadata["retentionDays"] = strconv.Itoa(int(*p.RetentionPolicy.Days))
		}
		if p.ProvisioningState != nil {
			r.metadata["provisioningState"] = string(*p.ProvisioningState)
		}
	}
	return r, nil
}

// flowLogEdges draws what the flow log watches and where it writes.
func flowLogEdges(fl *armnetwork.FlowLog) []edge {
	if fl.Properties == nil {
		return nil
	}
	id := ptr(fl.ID)
	var out []edge
	if target := ptr(fl.Properties.TargetResourceID); target != "" {
		out = append(out, edge{from: id, to: target, relation: edgeMonitors})
	}
	if storage := ptr(fl.Properties.StorageID); storage != "" {
		out = append(out, edge{from: id, to: storage, relation: edgeSinksTo})
	}
	if ta := fl.Properties.FlowAnalyticsConfiguration; ta != nil && ta.NetworkWatcherFlowAnalyticsConfiguration != nil {
		if ws := ptr(ta.NetworkWatcherFlowAnalyticsConfiguration.WorkspaceResourceID); ws != "" {
			out = append(out, edge{from: id, to: ws, relation: edgeSinksTo})
		}
	}
	return out
}
