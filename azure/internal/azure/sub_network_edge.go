// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// sub_network_edge.go — the network resources that sit at the edge: load
// balancers, application gateways, firewalls, NAT gateways, private endpoints
// and flow logs.

// loadBalancerSub walks the subscription's load balancers.
//
// IT CARRIES EACH BALANCER'S FRONTEND ADDRESSES FORWARD as a walk fact, which
// is what resolver 2 uses to turn a DNS record's raw address into a route.
type loadBalancerSub struct{ subBase }

func (s *loadBalancerSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewLoadBalancersClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("load balancers client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armnetwork.LoadBalancersClientListAllResponse) error {
		for _, lb := range page.Value {
			if lb == nil || lb.ID == nil {
				continue
			}
			r, err := loadBalancerResource(lb)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, loadBalancerEdges(lb)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing load balancers: %w", err)
	}
	return out, nil
}

func loadBalancerResource(lb *armnetwork.LoadBalancer) (resource, error) {
	content, err := marshalContent(lb)
	if err != nil {
		return resource{}, fmt.Errorf("projecting load balancer %s: %w", ptr(lb.ID), err)
	}
	r := resource{
		id:           ptr(lb.ID),
		name:         ptr(lb.Name),
		resourceType: rtLoadBalancer,
		region:       ptr(lb.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if lb.SKU != nil && lb.SKU.Name != nil {
		r.metadata["skuName"] = string(*lb.SKU.Name)
	}
	r.lbFrontendIPs = frontendAddresses(lb)
	return r, nil
}

// frontendAddresses returns the balancer's frontend private addresses, which
// are what a DNS A record inside the network would point at. A malformed value
// is dropped rather than carried: the index resolver 2 builds is keyed by
// address, and a key that is not an address can never be hit.
func frontendAddresses(lb *armnetwork.LoadBalancer) []string {
	if lb.Properties == nil {
		return nil
	}
	var out []string
	for _, fip := range lb.Properties.FrontendIPConfigurations {
		if fip == nil || fip.Properties == nil {
			continue
		}
		addr := ptr(fip.Properties.PrivateIPAddress)
		if addr == "" || net.ParseIP(addr) == nil {
			continue
		}
		out = append(out, addr)
	}
	return out
}

func loadBalancerEdges(lb *armnetwork.LoadBalancer) []edge {
	if lb.Properties == nil {
		return nil
	}
	id := ptr(lb.ID)
	var out []edge
	for _, fip := range lb.Properties.FrontendIPConfigurations {
		if fip == nil || fip.Properties == nil || fip.Properties.Subnet == nil {
			continue
		}
		if subnetID := ptr(fip.Properties.Subnet.ID); subnetID != "" {
			out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
		}
	}
	for _, pool := range lb.Properties.BackendAddressPools {
		if pool == nil || pool.Properties == nil {
			continue
		}
		out = append(out, backendPoolEdges(id, ptr(pool.Name), pool.Properties.LoadBalancerBackendAddresses)...)
	}
	return out
}

// backendPoolEdges draws what a balancer sends traffic to, and the subnets its
// backend addresses sit in.
//
// THE TARGET IS THE INTERFACE CONFIGURATION when the pool names one, and the
// network otherwise: a backend address configured by interface names a real
// machine, while one configured by address names only the network it is in.
func backendPoolEdges(lbID, poolName string, addrs []*armnetwork.LoadBalancerBackendAddress) []edge {
	var out []edge
	for _, addr := range addrs {
		if addr == nil || addr.Properties == nil {
			continue
		}
		md := map[string]string{}
		if poolName != "" {
			md["pool_name"] = poolName
		}
		p := addr.Properties
		switch {
		case p.NetworkInterfaceIPConfiguration != nil && ptr(p.NetworkInterfaceIPConfiguration.ID) != "":
			out = append(out, edge{from: lbID, to: ptr(p.NetworkInterfaceIPConfiguration.ID), relation: edgeTargets, metadata: md})
		case p.VirtualNetwork != nil && ptr(p.VirtualNetwork.ID) != "":
			out = append(out, edge{from: lbID, to: ptr(p.VirtualNetwork.ID), relation: edgeTargets, metadata: md})
		}
		if p.Subnet != nil && ptr(p.Subnet.ID) != "" {
			out = append(out, edge{from: lbID, to: ptr(p.Subnet.ID), relation: edgeUsesSubnet})
		}
	}
	return out
}

// appGatewaySub walks the subscription's application gateways.
type appGatewaySub struct{ subBase }

func (s *appGatewaySub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewApplicationGatewaysClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("application gateways client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armnetwork.ApplicationGatewaysClientListAllResponse) error {
		for _, gw := range page.Value {
			if gw == nil || gw.ID == nil {
				continue
			}
			r, err := appGatewayResource(gw)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, appGatewayEdges(gw)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing application gateways: %w", err)
	}
	return out, nil
}

func appGatewayResource(gw *armnetwork.ApplicationGateway) (resource, error) {
	content, err := marshalContent(gw)
	if err != nil {
		return resource{}, fmt.Errorf("projecting application gateway %s: %w", ptr(gw.ID), err)
	}
	r := resource{
		id:           ptr(gw.ID),
		name:         ptr(gw.Name),
		resourceType: rtAppGateway,
		region:       ptr(gw.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if gw.Properties != nil && gw.Properties.SKU != nil && gw.Properties.SKU.Name != nil {
		r.metadata["skuName"] = string(*gw.Properties.SKU.Name)
	}
	return r, nil
}

// appGatewayEdges draws the gateway's subnet, its backends, the firewall policy
// guarding it and the certificates it serves.
//
// THE POLICY EDGE RUNS POLICY TO GATEWAY, not gateway to policy: the policy is
// what acts on the gateway, and every other guarding relationship in this
// collector runs from the guard to the guarded.
func appGatewayEdges(gw *armnetwork.ApplicationGateway) []edge {
	if gw.Properties == nil {
		return nil
	}
	id := ptr(gw.ID)
	var out []edge
	for _, cfg := range gw.Properties.GatewayIPConfigurations {
		if cfg == nil || cfg.Properties == nil || cfg.Properties.Subnet == nil {
			continue
		}
		if subnetID := ptr(cfg.Properties.Subnet.ID); subnetID != "" {
			out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
		}
	}
	for _, pool := range gw.Properties.BackendAddressPools {
		if pool == nil || pool.Properties == nil {
			continue
		}
		for _, cfg := range pool.Properties.BackendIPConfigurations {
			if cfg == nil || ptr(cfg.ID) == "" {
				continue
			}
			out = append(out, edge{from: id, to: ptr(cfg.ID), relation: edgeTargets})
		}
	}
	if gw.Properties.FirewallPolicy != nil && ptr(gw.Properties.FirewallPolicy.ID) != "" {
		out = append(out, edge{from: ptr(gw.Properties.FirewallPolicy.ID), to: id, relation: edgeProtects})
	}
	for _, cert := range gw.Properties.SSLCertificates {
		if cert == nil || cert.Properties == nil {
			continue
		}
		if secret := ptr(cert.Properties.KeyVaultSecretID); secret != "" {
			out = append(out, edge{from: id, to: secret, relation: edgeUsesCert})
		}
	}
	return out
}

// firewallSub walks the subscription's Azure Firewalls.
type firewallSub struct{ subBase }

func (s *firewallSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewAzureFirewallsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("firewalls client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armnetwork.AzureFirewallsClientListAllResponse) error {
		for _, fw := range page.Value {
			if fw == nil || fw.ID == nil {
				continue
			}
			r, err := firewallResource(fw)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, firewallEdges(fw)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing firewalls: %w", err)
	}
	return out, nil
}

func firewallResource(fw *armnetwork.AzureFirewall) (resource, error) {
	content, err := marshalContent(fw)
	if err != nil {
		return resource{}, fmt.Errorf("projecting firewall %s: %w", ptr(fw.ID), err)
	}
	r := resource{
		id:           ptr(fw.ID),
		name:         ptr(fw.Name),
		resourceType: rtFirewall,
		region:       ptr(fw.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := fw.Properties; p != nil {
		if p.SKU != nil && p.SKU.Name != nil {
			r.metadata["skuName"] = string(*p.SKU.Name)
		}
		if p.ThreatIntelMode != nil {
			r.metadata["threatIntelMode"] = string(*p.ThreatIntelMode)
		}
	}
	return r, nil
}

// firewallEdges draws the firewall's subnets and what its address-translation
// rules point at.
//
// ONLY A TRANSLATED TARGET THAT IS AN ARM ID BECOMES AN EDGE. A rule
// translating to a bare address or hostname names something this graph has no
// node for, and an edge to a hostname string would be a dangling endpoint
// nothing can resolve.
func firewallEdges(fw *armnetwork.AzureFirewall) []edge {
	if fw.Properties == nil {
		return nil
	}
	id := ptr(fw.ID)
	var out []edge
	seen := map[string]bool{}
	addSubnet := func(subnetID string) {
		for _, e := range subnetEdges(id, subnetID) {
			if seen[e.to] {
				continue
			}
			seen[e.to] = true
			out = append(out, e)
		}
	}
	for _, cfg := range fw.Properties.IPConfigurations {
		if cfg == nil || cfg.Properties == nil || cfg.Properties.Subnet == nil {
			continue
		}
		addSubnet(ptr(cfg.Properties.Subnet.ID))
	}
	if mgmt := fw.Properties.ManagementIPConfiguration; mgmt != nil && mgmt.Properties != nil && mgmt.Properties.Subnet != nil {
		addSubnet(ptr(mgmt.Properties.Subnet.ID))
	}
	for _, coll := range fw.Properties.NatRuleCollections {
		if coll == nil || coll.Properties == nil {
			continue
		}
		for _, rule := range coll.Properties.Rules {
			target := natRuleTarget(rule)
			if !strings.HasPrefix(target, "/") {
				continue
			}
			out = append(out, edge{from: id, to: target, relation: edgeProtects})
		}
	}
	return out
}

func natRuleTarget(rule *armnetwork.AzureFirewallNatRule) string {
	if rule == nil {
		return ""
	}
	if v := ptr(rule.TranslatedFqdn); v != "" {
		return v
	}
	return ptr(rule.TranslatedAddress)
}
