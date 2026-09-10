// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// fixtures_network_test.go — hand-built network responses.

const (
	nsgID        = rgID + "/providers/Microsoft.Network/networkSecurityGroups/nsg1"
	lbID         = rgID + "/providers/Microsoft.Network/loadBalancers/lb1"
	appGwID      = rgID + "/providers/Microsoft.Network/applicationGateways/gw1"
	firewallID   = rgID + "/providers/Microsoft.Network/azureFirewalls/fw1"
	natGwID      = rgID + "/providers/Microsoft.Network/natGateways/nat1"
	peID         = rgID + "/providers/Microsoft.Network/privateEndpoints/pe1"
	flowLogID    = rgID + "/providers/Microsoft.Network/networkWatchers/nw1/flowLogs/fl1"
	wafPolicyID  = rgID + "/providers/Microsoft.Network/firewallPolicies/waf1"
	peeringID    = vnetID + "/virtualNetworkPeerings/peer1"
	remoteVNetID = subID + "/resourceGroups/other/providers/Microsoft.Network/virtualNetworks/vnet2"
	workspaceID  = rgID + "/providers/Microsoft.OperationalInsights/workspaces/law1"
	storageID    = rgID + "/providers/Microsoft.Storage/storageAccounts/store1"
	kvSecretID   = "https://vault1.vault.azure.net/secrets/tls"
)

func networkFixtures() []fixture {
	return []fixture{
		{name: "virtual network and its subnets", build: func(t *testing.T) subResult {
			vnet := virtualNetwork()
			subnets, edges, err := subnetResources(vnet)
			if err != nil {
				t.Fatalf("subnetResources: %v", err)
			}
			return subResult{
				resources: append([]resource{fx{t}.res(vnetResource(vnet))}, subnets...),
				edges:     edges,
			}
		}},
		{name: "security group and its rules", build: func(t *testing.T) subResult {
			nsg := securityGroup()
			rules, edges, err := nsgRuleResources(nsg)
			if err != nil {
				t.Fatalf("nsgRuleResources: %v", err)
			}
			return subResult{
				resources: append([]resource{fx{t}.res(nsgResource(nsg))}, rules...),
				edges:     edges,
			}
		}},
		{name: "network peering", build: func(t *testing.T) subResult {
			peering := networkPeering()
			vnet := virtualNetwork()
			return subResult{
				resources: []resource{fx{t}.res(peeringResource(peering, vnet))},
				edges:     peeringEdges(peering, vnetID),
			}
		}},
		{name: "load balancer", build: func(t *testing.T) subResult {
			lb := loadBalancer()
			return subResult{
				resources: []resource{fx{t}.res(loadBalancerResource(lb))},
				edges:     loadBalancerEdges(lb),
			}
		}},
		{name: "application gateway", build: func(t *testing.T) subResult {
			gw := applicationGateway()
			return subResult{
				resources: []resource{fx{t}.res(appGatewayResource(gw))},
				edges:     appGatewayEdges(gw),
			}
		}},
		{name: "firewall", build: func(t *testing.T) subResult {
			fw := azureFirewall()
			return subResult{
				resources: []resource{fx{t}.res(firewallResource(fw))},
				edges:     firewallEdges(fw),
			}
		}},
		{name: "nat gateway", build: func(t *testing.T) subResult {
			ng := natGateway()
			return subResult{
				resources: []resource{fx{t}.res(natGatewayResource(ng))},
				edges:     natGatewayEdges(ng),
			}
		}},
		{name: "private endpoint", build: func(t *testing.T) subResult {
			pe := privateEndpoint()
			return subResult{
				resources: []resource{fx{t}.res(privateEndpointResource(pe))},
				edges:     privateEndpointEdges(pe),
			}
		}},
		{name: "flow log", build: func(t *testing.T) subResult {
			fl := flowLog()
			return subResult{
				resources: []resource{fx{t}.res(flowLogResource(fl))},
				edges:     flowLogEdges(fl),
			}
		}},
	}
}

func virtualNetwork() *armnetwork.VirtualNetwork {
	return &armnetwork.VirtualNetwork{
		ID:       new(vnetID),
		Name:     new("vnet1"),
		Location: new("westeurope"),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{AddressPrefixes: []*string{new("10.0.0.0/16")}},
			Subnets: []*armnetwork.Subnet{{
				ID:   new(subnetID),
				Name: new("default"),
				Properties: &armnetwork.SubnetPropertiesFormat{
					AddressPrefix:        new("10.0.1.0/24"),
					NetworkSecurityGroup: &armnetwork.SecurityGroup{ID: new(nsgID)},
				},
			}},
		},
	}
}

// securityGroup carries one Allow rule, one Deny rule and one rule naming only
// an application security group: the three cases resolver 1 discriminates on.
func securityGroup() *armnetwork.SecurityGroup {
	return &armnetwork.SecurityGroup{
		ID:       new(nsgID),
		Name:     new("nsg1"),
		Location: new("westeurope"),
		Properties: &armnetwork.SecurityGroupPropertiesFormat{
			SecurityRules: []*armnetwork.SecurityRule{
				{
					Name: new("allow-https-in"),
					Properties: &armnetwork.SecurityRulePropertiesFormat{
						Access:                   to.Ptr(armnetwork.SecurityRuleAccessAllow),
						Direction:                to.Ptr(armnetwork.SecurityRuleDirectionInbound),
						Protocol:                 to.Ptr(armnetwork.SecurityRuleProtocolTCP),
						Priority:                 to.Ptr[int32](100),
						SourceAddressPrefix:      new("203.0.113.0/24"),
						DestinationPortRange:     new("443"),
						DestinationAddressPrefix: new("*"),
					},
				},
				{
					Name: new("deny-all-in"),
					Properties: &armnetwork.SecurityRulePropertiesFormat{
						Access:              to.Ptr(armnetwork.SecurityRuleAccessDeny),
						Direction:           to.Ptr(armnetwork.SecurityRuleDirectionInbound),
						Protocol:            to.Ptr(armnetwork.SecurityRuleProtocolAsterisk),
						Priority:            to.Ptr[int32](4000),
						SourceAddressPrefix: new("*"),
					},
				},
				{
					Name: new("allow-asg-only"),
					Properties: &armnetwork.SecurityRulePropertiesFormat{
						Access:    to.Ptr(armnetwork.SecurityRuleAccessAllow),
						Direction: to.Ptr(armnetwork.SecurityRuleDirectionInbound),
						Protocol:  to.Ptr(armnetwork.SecurityRuleProtocolTCP),
						Priority:  to.Ptr[int32](110),
						SourceApplicationSecurityGroups: []*armnetwork.ApplicationSecurityGroup{
							{ID: new(rgID + "/providers/Microsoft.Network/applicationSecurityGroups/asg1")},
						},
					},
				},
				{
					Name: new("allow-egress-anywhere"),
					Properties: &armnetwork.SecurityRulePropertiesFormat{
						Access:                   to.Ptr(armnetwork.SecurityRuleAccessAllow),
						Direction:                to.Ptr(armnetwork.SecurityRuleDirectionOutbound),
						Protocol:                 to.Ptr(armnetwork.SecurityRuleProtocolAsterisk),
						Priority:                 to.Ptr[int32](120),
						DestinationAddressPrefix: new("*"),
					},
				},
			},
			DefaultSecurityRules: []*armnetwork.SecurityRule{{
				Name: new("AllowVnetInBound"),
				Properties: &armnetwork.SecurityRulePropertiesFormat{
					Access:              to.Ptr(armnetwork.SecurityRuleAccessAllow),
					Direction:           to.Ptr(armnetwork.SecurityRuleDirectionInbound),
					SourceAddressPrefix: new("VirtualNetwork"),
				},
			}},
		},
	}
}

func networkPeering() *armnetwork.VirtualNetworkPeering {
	return &armnetwork.VirtualNetworkPeering{
		ID:   new(peeringID),
		Name: new("peer1"),
		Properties: &armnetwork.VirtualNetworkPeeringPropertiesFormat{
			RemoteVirtualNetwork: &armnetwork.SubResource{ID: new(remoteVNetID)},
		},
	}
}

func loadBalancer() *armnetwork.LoadBalancer {
	return &armnetwork.LoadBalancer{
		ID:       new(lbID),
		Name:     new("lb1"),
		Location: new("westeurope"),
		SKU:      &armnetwork.LoadBalancerSKU{Name: to.Ptr(armnetwork.LoadBalancerSKUNameStandard)},
		Properties: &armnetwork.LoadBalancerPropertiesFormat{
			FrontendIPConfigurations: []*armnetwork.FrontendIPConfiguration{{
				Name: new("front"),
				Properties: &armnetwork.FrontendIPConfigurationPropertiesFormat{
					PrivateIPAddress: new("10.0.1.4"),
					Subnet:           &armnetwork.Subnet{ID: new(subnetID)},
				},
			}},
			BackendAddressPools: []*armnetwork.BackendAddressPool{{
				Name: new("pool"),
				Properties: &armnetwork.BackendAddressPoolPropertiesFormat{
					LoadBalancerBackendAddresses: []*armnetwork.LoadBalancerBackendAddress{{
						Properties: &armnetwork.LoadBalancerBackendAddressPropertiesFormat{
							NetworkInterfaceIPConfiguration: &armnetwork.SubResource{ID: new(nicID + "/ipConfigurations/ipconfig1")},
							Subnet:                          &armnetwork.SubResource{ID: new(subnetID)},
						},
					}},
				},
			}},
		},
	}
}

func applicationGateway() *armnetwork.ApplicationGateway {
	return &armnetwork.ApplicationGateway{
		ID:       new(appGwID),
		Name:     new("gw1"),
		Location: new("westeurope"),
		Properties: &armnetwork.ApplicationGatewayPropertiesFormat{
			SKU: &armnetwork.ApplicationGatewaySKU{Name: to.Ptr(armnetwork.ApplicationGatewaySKUNameWAFV2)},
			GatewayIPConfigurations: []*armnetwork.ApplicationGatewayIPConfiguration{{
				Properties: &armnetwork.ApplicationGatewayIPConfigurationPropertiesFormat{
					Subnet: &armnetwork.SubResource{ID: new(subnetID)},
				},
			}},
			BackendAddressPools: []*armnetwork.ApplicationGatewayBackendAddressPool{{
				Properties: &armnetwork.ApplicationGatewayBackendAddressPoolPropertiesFormat{
					BackendIPConfigurations: []*armnetwork.InterfaceIPConfiguration{
						{ID: new(nicID + "/ipConfigurations/ipconfig1")},
					},
				},
			}},
			FirewallPolicy: &armnetwork.SubResource{ID: new(wafPolicyID)},
			SSLCertificates: []*armnetwork.ApplicationGatewaySSLCertificate{{
				Properties: &armnetwork.ApplicationGatewaySSLCertificatePropertiesFormat{
					KeyVaultSecretID: new(kvSecretID),
				},
			}},
		},
	}
}

func azureFirewall() *armnetwork.AzureFirewall {
	return &armnetwork.AzureFirewall{
		ID:       new(firewallID),
		Name:     new("fw1"),
		Location: new("westeurope"),
		Properties: &armnetwork.AzureFirewallPropertiesFormat{
			SKU:             &armnetwork.AzureFirewallSKU{Name: to.Ptr(armnetwork.AzureFirewallSKUNameAZFWVnet)},
			ThreatIntelMode: to.Ptr(armnetwork.AzureFirewallThreatIntelModeAlert),
			IPConfigurations: []*armnetwork.AzureFirewallIPConfiguration{{
				Properties: &armnetwork.AzureFirewallIPConfigurationPropertiesFormat{
					Subnet: &armnetwork.SubResource{ID: new(vnetID + "/subnets/AzureFirewallSubnet")},
				},
			}},
			NatRuleCollections: []*armnetwork.AzureFirewallNatRuleCollection{{
				Properties: &armnetwork.AzureFirewallNatRuleCollectionProperties{
					Rules: []*armnetwork.AzureFirewallNatRule{
						{Name: new("dnat-to-vm"), TranslatedFqdn: new(vmID)},
						// The negative arm, in the same fixture: a rule
						// translating to a bare address names nothing this
						// graph has a node for.
						{Name: new("dnat-to-address"), TranslatedAddress: new("10.0.1.9")},
					},
				},
			}},
		},
	}
}

func natGateway() *armnetwork.NatGateway {
	return &armnetwork.NatGateway{
		ID:       new(natGwID),
		Name:     new("nat1"),
		Location: new("westeurope"),
		SKU:      &armnetwork.NatGatewaySKU{Name: to.Ptr(armnetwork.NatGatewaySKUNameStandard)},
		Properties: &armnetwork.NatGatewayPropertiesFormat{
			IdleTimeoutInMinutes: to.Ptr[int32](4),
			Subnets:              []*armnetwork.SubResource{{ID: new(subnetID)}},
		},
	}
}

func privateEndpoint() *armnetwork.PrivateEndpoint {
	return &armnetwork.PrivateEndpoint{
		ID:       new(peID),
		Name:     new("pe1"),
		Location: new("westeurope"),
		Properties: &armnetwork.PrivateEndpointProperties{
			Subnet: &armnetwork.Subnet{ID: new(subnetID)},
			PrivateLinkServiceConnections: []*armnetwork.PrivateLinkServiceConnection{{
				Properties: &armnetwork.PrivateLinkServiceConnectionProperties{
					PrivateLinkServiceID: new(storageID),
				},
			}},
			ManualPrivateLinkServiceConnections: []*armnetwork.PrivateLinkServiceConnection{{
				Properties: &armnetwork.PrivateLinkServiceConnectionProperties{
					PrivateLinkServiceID: new(rgID + "/providers/Microsoft.Sql/servers/sql1"),
				},
			}},
		},
	}
}

func flowLog() *armnetwork.FlowLog {
	return &armnetwork.FlowLog{
		ID:       new(flowLogID),
		Name:     new("fl1"),
		Location: new("westeurope"),
		Properties: &armnetwork.FlowLogPropertiesFormat{
			Enabled:          new(true),
			TargetResourceID: new(nsgID),
			StorageID:        new(storageID),
			RetentionPolicy:  &armnetwork.RetentionPolicyParameters{Days: to.Ptr[int32](30)},
			FlowAnalyticsConfiguration: &armnetwork.TrafficAnalyticsProperties{
				NetworkWatcherFlowAnalyticsConfiguration: &armnetwork.TrafficAnalyticsConfigurationProperties{
					WorkspaceResourceID: new(workspaceID),
				},
			},
		},
	}
}

// TestSubnetResources_DrawTheirNetworkAndTheirSecurityGroup pins both subnet
// relationships and their directions.
func TestSubnetResources_DrawTheirNetworkAndTheirSecurityGroup(t *testing.T) {
	subnets, edges, err := subnetResources(virtualNetwork())
	if err != nil {
		t.Fatalf("subnetResources: %v", err)
	}
	if len(subnets) != 1 {
		t.Fatalf("expected one subnet, got %d", len(subnets))
	}
	if subnets[0].region != "westeurope" {
		t.Errorf("the subnet did not inherit its network's region: %q", subnets[0].region)
	}
	if _, ok := edgeBetween(edges, subnetID, vnetID, edgeUsesNetwork); !ok {
		t.Error("no membership edge from the subnet to its network")
	}
	if _, ok := edgeBetween(edges, subnetID, nsgID, edgeUsesSecurityGroup); !ok {
		t.Error("no edge from the subnet to the security group applied to it")
	}

	// The negative, through the same path: a subnet with no group applied
	// draws only the membership edge.
	plain := virtualNetwork()
	plain.Properties.Subnets[0].Properties.NetworkSecurityGroup = nil
	_, plainEdges, err := subnetResources(plain)
	if err != nil {
		t.Fatalf("subnetResources: %v", err)
	}
	if relationsOf(plainEdges)[edgeUsesSecurityGroup] != 0 {
		t.Error("a subnet with no security group drew one anyway")
	}
}

// TestNSGRuleResources_EmitDefaultRulesAsNodesAndExcludeThemFromReachability.
// The two are separate obligations and this is the only place the difference is
// visible: a rule node is a record of configuration, while the reachability
// facts resolver 1 draws would be identical on every group in the subscription
// if the defaults were admitted.
func TestNSGRuleResources_EmitDefaultRulesAsNodesAndExcludeThemFromReachability(t *testing.T) {
	nsg := securityGroup()
	rules, edges, err := nsgRuleResources(nsg)
	if err != nil {
		t.Fatalf("nsgRuleResources: %v", err)
	}
	names := map[string]bool{}
	for _, r := range rules {
		names[r.name] = true
		if _, ok := edgeBetween(edges, nsgID, r.id, edgeContains); !ok {
			t.Errorf("the group does not contain its rule %s", r.name)
		}
	}
	if !names["allow-https-in"] {
		t.Error("an operator's own rule is not a node")
	}
	if !names["AllowVnetInBound"] {
		t.Error("a default rule is not a node, so the graph cannot show what the group actually permits")
	}

	carried := fx{t}.res(nsgResource(nsg))
	for _, rule := range carried.nsgRules {
		if rule.name == "AllowVnetInBound" {
			t.Error("a default rule reached the reachability walk, which would draw the same fan out of every group")
		}
	}
	if len(carried.nsgRules) != 4 {
		t.Errorf("the group carried %d rules forward for reachability, expected its 4 own", len(carried.nsgRules))
	}
}

// TestFirewallEdges_OnlyResourceTargetsBecomeEdges. A translated target that is
// a bare address names nothing this graph has a node for.
func TestFirewallEdges_OnlyResourceTargetsBecomeEdges(t *testing.T) {
	edges := firewallEdges(azureFirewall())
	if _, ok := edgeBetween(edges, firewallID, vmID, edgeProtects); !ok {
		t.Error("a rule translating to a resource id drew no edge")
	}
	for _, e := range edges {
		if e.relation == edgeProtects && e.to == "10.0.1.9" {
			t.Error("a rule translating to a bare address drew an edge to the address")
		}
	}
	// The derived network edge: a firewall's subnet id names its network.
	if _, ok := edgeBetween(edges, firewallID, vnetID, edgeUsesNetwork); !ok {
		t.Error("the firewall drew no edge to the network its subnet sits in")
	}
}

// TestPrivateEndpointEdges_ReadBothConnectionLists. An endpoint into another
// subscription is approved manually, and a walk reading only the automatic list
// would miss every one of them.
func TestPrivateEndpointEdges_ReadBothConnectionLists(t *testing.T) {
	edges := privateEndpointEdges(privateEndpoint())
	if _, ok := edgeBetween(edges, peID, storageID, edgeTargets); !ok {
		t.Error("the automatically approved connection drew no edge")
	}
	if _, ok := edgeBetween(edges, peID, rgID+"/providers/Microsoft.Sql/servers/sql1", edgeTargets); !ok {
		t.Error("the manually approved connection drew no edge, so an endpoint into another subscription is invisible")
	}
}

// TestLoadBalancerResource_CarriesFrontendAddressesForward is the walk fact
// resolver 2 depends on, and its negative: a malformed address is not carried,
// because an index entry that is not an address can never be hit.
func TestLoadBalancerResource_CarriesFrontendAddressesForward(t *testing.T) {
	r := fx{t}.res(loadBalancerResource(loadBalancer()))
	if len(r.lbFrontendIPs) != 1 || r.lbFrontendIPs[0] != "10.0.1.4" {
		t.Errorf("the balancer carried %v forward, expected its one frontend address", r.lbFrontendIPs)
	}

	malformed := loadBalancer()
	malformed.Properties.FrontendIPConfigurations[0].Properties.PrivateIPAddress = new("not-an-address")
	got := fx{t}.res(loadBalancerResource(malformed))
	if len(got.lbFrontendIPs) != 0 {
		t.Errorf("a malformed frontend address was carried forward: %v", got.lbFrontendIPs)
	}
}
