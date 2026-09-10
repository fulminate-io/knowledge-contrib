// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/cdn/armcdn/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/eventhub/armeventhub"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/servicebus/armservicebus/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/sql/armsql"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// census_test.go — THE PARITY CENSUS, DRIVEN THROUGH THE EMITTERS.
//
// WHY THIS EXISTS BESIDE THE FIXTURE CENSUS. The fixture census in
// fixtures_test.go proves each conversion function CAN produce its type. It
// cannot prove the walk calls it, and a fixture that builds an edge itself
// proves nothing at all — which is how SUBSCRIBES_TO came to have two
// production emitters that could both be deleted with a green suite.
//
// SO THIS ONE OWNS THE OBLIGATION: it runs every one of the 36 subcollectors
// against a fake management plane, merges what they produced, runs the
// resolvers over it, and asserts the whole declared vocabulary appears. Every
// node and every edge in it came out of the shipped code path — the real
// client, the real pager, the real page loop, the real conversion function — so
// deleting any emitter fails this census BY NAME.
//
// THERE IS NO LITERAL IN IT. The only things this test constructs are the API
// RESPONSES, which is what the far side of the seam is.

const censusSubscription = "0000"

// censusWalk runs every subcollector against the fake plane and returns the
// merged, resolved outcome. It is one run several rows read, because standing
// the whole set up is the expensive part and the rows differ only in what they
// assert about it.
func censusWalk(t *testing.T) walkOutcome {
	t.Helper()
	fake := newARMFake(censusRoutes(t))
	subs := censusSubCollectors(t, fake)

	if len(subs) != len(subCollectorNames) {
		t.Fatalf("the census drives %d subcollectors and the walk declares %d", len(subs), len(subCollectorNames))
	}

	outcome := runSubCollectors(context.Background(), subs, 4)
	for _, f := range outcome.failures {
		t.Errorf("%s failed against the fake management plane: %v", f.name, f.err)
	}
	fake.assertEveryRouteFired(t)
	return runResolvers(outcome)
}

// TestCensus_EverySubcollectorRunsAndEveryDeclaredTypeIsEmitted.
//
// IT IS A TYPE census and states its own limit: a relationship with two
// emitters survives the loss of one, so this row cannot see a single deleted
// emission site. That is what [TestCensus_ElidedEmissionSitesAreEachObserved]
// is for, and the two together are what the parity floor actually needs.
func TestCensus_EverySubcollectorRunsAndEveryDeclaredTypeIsEmitted(t *testing.T) {
	outcome := censusWalk(t)

	// EVERY DECLARED RELATIONSHIP, produced by an emitter rather than a literal.
	produced := relationsOf(outcome.edges)
	var missing []string
	for _, et := range edgeTypes {
		if produced[et] == 0 {
			missing = append(missing, et)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d declared relationship(s) were not emitted by any subcollector or resolver:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}

	// EVERY DECLARED RESOURCE TYPE, likewise.
	types := resourceTypesOf(outcome.resources)
	missing = nil
	for _, rt := range append(append([]string(nil), armResourceTypes...), syntheticResourceTypes...) {
		if types[rt] == 0 {
			missing = append(missing, rt)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Errorf("%d declared resource type(s) were not emitted by any subcollector or resolver:\n  %s",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestCensus_ElidedEmissionSitesAreEachObserved — ONE ROW PER EMISSION SITE
// WHOSE TYPE HAS ANOTHER EMITTER.
//
// WHY A TYPE CENSUS IS NOT ENOUGH, and this is the whole reason this test
// exists beside it: eight of this collector's emissions sit in listers whose
// relationship type is also emitted somewhere else. Delete any one of them and
// every count of types still passes, while the graph quietly loses that
// specific relationship — a subscription's link to its topic, a workspace's to
// the network it serves, an alert's to what it watches.
//
// SO EACH ROW NAMES ITS SITE AND ITS EXACT ENDPOINTS. A row fails by naming the
// file and the relationship, which is what makes the failure actionable rather
// than a count that moved.
func TestCensus_ElidedEmissionSitesAreEachObserved(t *testing.T) {
	outcome := censusWalk(t)

	for _, site := range []struct {
		site     string
		from     string
		to       string
		relation string
	}{
		{"sub_servicebus.go subscriptionEdges", sbSubID, sbTopicID, edgeSubscribesTo},
		{"sub_eventing.go consumerGroupEdges", ehGroupID, ehID, edgeSubscribesTo},
		{"sub_data.go collectPrivateEndpoints", sqlServerID, peID, edgeUsesSubnet},
		{"sub_dns.go zoneLinkEdges", privZoneID, vnetID, edgeUsesNetwork},
		{"sub_eventing.go collectNetworkRules", ehNamespaceID, subnetID, edgeUsesSubnet},
		{"sub_eventing.go collectEventSubscriptions", egSubID, functionAppID, edgeTargets},
		{"sub_monitor.go collectComponents", componentID, workspaceID, edgeSinksTo},
		{"sub_servicebus.go collectNetworkRules", sbNamespaceID, subnetID, edgeUsesSubnet},
	} {
		t.Run(site.site+" "+site.relation, func(t *testing.T) {
			if _, ok := edgeBetween(outcome.edges, site.from, site.to, site.relation); !ok {
				t.Errorf("%s emitted no %s from %s to %s; its type has another emitter, so no count of "+
					"relationship types would notice this relationship going missing",
					site.site, site.relation, site.from, site.to)
			}
		})
	}
}

// TestCensus_TheWalkAssertsCompleteOverTheFakePlane. The same run seen from the
// top: nothing failed, so the collect asserts a complete walk — which is the
// control that makes the incomplete rows elsewhere mean something.
func TestCensus_TheWalkAssertsCompleteOverTheFakePlane(t *testing.T) {
	fake := newARMFake(censusRoutes(t))
	c := &Collector{
		newCredential: func(context.Context) (azureCredential, error) { return fakeCredential{}, nil },
		buildSubs: func(cred azureCredential, subscriptionID string) []subCollector {
			return censusSubCollectors(t, fake)
		},
		lookupEnv: func(string) (string, bool) { return "", false },
	}
	result, err := c.Walk(context.Background(), censusSubscription, Params{}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if !result.Complete.IsComplete() {
		t.Fatalf("the walk over the fake plane asserted itself incomplete: %s", result.Complete.Reason())
	}
	if len(result.Nodes) == 0 || len(result.Edges) == 0 {
		t.Fatalf("the walk produced %d nodes and %d edges", len(result.Nodes), len(result.Edges))
	}
}

// censusSubCollectors builds the whole declared set on the fake transport, with
// the one subcollector that does not talk to ARM given its own seam.
func censusSubCollectors(t *testing.T, fake *armFake) []subCollector {
	t.Helper()
	subs := buildSubCollectorsOn(fakeCredential{}, censusSubscription, fake)

	// The directory walk speaks Microsoft Graph over its own HTTP client rather
	// than through an ARM client, so the ARM transport does not reach it. Its
	// own seam is the one the Graph tests use.
	graph := fakeGraph(t, 200)
	for i, sub := range subs {
		if sub.Name() == nameAADGroups {
			subs[i] = graph
		}
	}
	return subs
}

// censusRoutes is the fake management plane's whole answer set: one route per
// list call whose response this census populates, in most-specific-first order.
//
// THE VALUES ARE THE CONVERTER TESTS' OWN FIXTURES. One hand-built response
// serves both the unit row and this census, so the two cannot drift.
func censusRoutes(t *testing.T) []armRoute {
	t.Helper()
	return []armRoute{
		// --- compute -------------------------------------------------------
		{"virtual machines", "/providers/Microsoft.Compute/virtualMachines", armList(t, virtualMachine(vmID, "vm1"))},
		{"scale sets", "/providers/Microsoft.Compute/virtualMachineScaleSets", armList(t, scaleSet())},
		{"disks", "/providers/Microsoft.Compute/disks", armList(t, managedDisk())},

		// --- network -------------------------------------------------------
		{"network interfaces", "/providers/Microsoft.Network/networkInterfaces", armList(t, networkInterface())},
		{"virtual network peerings", "/virtualNetworkPeerings", armList(t, networkPeering())},
		{"virtual networks", "/providers/Microsoft.Network/virtualNetworks", armList(t, virtualNetwork())},
		{"security groups", "/providers/Microsoft.Network/networkSecurityGroups", armList(t, securityGroup())},
		{"load balancers", "/providers/Microsoft.Network/loadBalancers", armList(t, loadBalancer())},
		{"application gateways", "/providers/Microsoft.Network/applicationGateways", armList(t, applicationGateway())},
		{"firewalls", "/providers/Microsoft.Network/azureFirewalls", armList(t, azureFirewall())},
		{"nat gateways", "/providers/Microsoft.Network/natGateways", armList(t, natGateway())},
		{"private endpoints", "/providers/Microsoft.Network/privateEndpoints", armList(t, privateEndpoint())},
		{"flow logs", "/flowLogs", armList(t, flowLog())},
		{"network watchers", "/providers/Microsoft.Network/networkWatchers", armList(t, networkWatcher())},
		{"private dns links", "/virtualNetworkLinks", armList(t, privateDNSLink())},
		{"private dns zones", "/providers/Microsoft.Network/privateDnsZones", armList(t, privateZone())},

		// --- dns -----------------------------------------------------------
		{"dns record sets", "/dnsZones/example.com/all", armList(t, addressRecordSet(), aliasRecordSet())},
		{"dns zones", "/providers/Microsoft.Network/dnszones", armList(t, dnsZone())},

		// --- containers ----------------------------------------------------
		{"managed clusters", "/providers/Microsoft.ContainerService/managedClusters", armList(t, managedCluster())},
		{"registries", "/providers/Microsoft.ContainerRegistry/registries", armList(t, containerRegistry())},

		// --- identity ------------------------------------------------------
		{"federated credentials", "/federatedIdentityCredentials", armList(t,
			federatedCredentialFor(foreignIssuer, "workload-one"),
			federatedCredentialFor(githubIssuer, "repo:acme/service:ref:refs/heads/main"),
			federatedCredentialFor(aksIssuer, "system:serviceaccount:apps:api"))},
		{"managed identities", "/providers/Microsoft.ManagedIdentity/userAssignedIdentities", armList(t, managedIdentity())},
		{"role assignments", "/providers/Microsoft.Authorization/roleAssignments", armList(t, censusRoleAssignment())},

		// --- data ----------------------------------------------------------
		{"key vaults", "/providers/Microsoft.KeyVault/vaults", armList(t, keyVault())},
		{"file shares", "/fileServices/default/shares", armList(t, fileShare())},
		{"storage accounts", "/providers/Microsoft.Storage/storageAccounts", armList(t, storageAccount())},
		{"sql private endpoint connections", "/privateEndpointConnections", armList(t, sqlPrivateEndpointConnection())},
		{"sql databases", "/databases", armList(t, sqlDatabase())},
		{"sql servers", "/providers/Microsoft.Sql/servers", armList(t, sqlServer())},
		{"cosmos accounts", "/providers/Microsoft.DocumentDB/databaseAccounts", armList(t, cosmosAccount())},
		{"redis caches", "/providers/Microsoft.Cache/redis", armList(t, redisCache())},
		{"search services", "/providers/Microsoft.Search/searchServices", armList(t, searchService())},
		{"synapse sql pools", "/sqlPools", armList(t, synapseSQLPool())},
		{"synapse spark pools", "/bigDataPools", armList(t, synapseSparkPool())},
		{"synapse workspaces", "/providers/Microsoft.Synapse/workspaces", armList(t, synapseWorkspace())},

		// --- messaging -----------------------------------------------------
		{"service bus network rules", "/providers/Microsoft.ServiceBus/namespaces/sb1/networkRuleSets", armObject(t, serviceBusNetworkRuleSet())},
		{"service bus subscriptions", "/topics/events/subscriptions", armList(t, serviceBusSubscription())},
		{"service bus topics", "/providers/Microsoft.ServiceBus/namespaces/sb1/topics", armList(t, serviceBusTopic())},
		{"service bus queues", "/queues", armList(t, serviceBusQueue())},
		{"service bus namespaces", "/providers/Microsoft.ServiceBus/namespaces", armList(t, serviceBusNamespace())},
		{"event hub network rules", "/providers/Microsoft.EventHub/namespaces/eh1/networkRuleSets", armObject(t, eventHubNetworkRuleSet())},
		{"consumer groups", "/consumergroups", armList(t, consumerGroup())},
		{"event hubs", "/eventhubs", armList(t, eventHub())},
		{"event hub namespaces", "/providers/Microsoft.EventHub/namespaces", armList(t, eventHubNamespace())},
		{"event grid subscriptions", "/providers/Microsoft.EventGrid/eventSubscriptions", armList(t, eventGridSubscription())},
		{"event grid topics", "/providers/Microsoft.EventGrid/topics", armList(t, eventGridTopic())},

		// --- web and integration -------------------------------------------
		{"site functions", "/functions", armList(t, siteFunction())},
		{"sites", "/providers/Microsoft.Web/sites", armList(t, webSite(), functionApp())},
		{"certificates", "/providers/Microsoft.Web/certificates", armList(t, appCertificate())},
		{"api management apis", "/apis", armList(t, apimAPI())},
		{"api management services", "/providers/Microsoft.ApiManagement/service", armList(t, apimService())},
		{"logic app workflows", "/providers/Microsoft.Logic/workflows", armList(t, logicWorkflow())},

		// --- front door ----------------------------------------------------
		{"front door endpoints", "/afdEndpoints", armList(t, frontDoorEndpoint())},
		{"front door origins", "/originGroups/origins/origins", armList(t, frontDoorOrigin())},
		{"front door origin groups", "/originGroups", armList(t, frontDoorOriginGroup())},
		{"front door security policies", "/securityPolicies", armList(t, frontDoorSecurityPolicy())},
		{"front door custom domains", "/customDomains", armList(t, frontDoorDomain())},
		{"front door profiles", "/providers/Microsoft.Cdn/profiles", armList(t, frontDoorProfile())},

		// --- monitoring ----------------------------------------------------
		{"log analytics workspaces", "/providers/Microsoft.OperationalInsights/workspaces", armList(t, logAnalyticsWorkspace())},
		{"application insights", "/providers/Microsoft.Insights/components", armList(t, insightsComponent())},
		{"diagnostic settings", "/providers/Microsoft.Insights/diagnosticSettings", armList(t, diagnosticSetting())},
		{"metric alerts", "/providers/Microsoft.Insights/metricAlerts", armList(t, metricAlert())},
	}
}

// The response fixtures this census needs beyond the converter tests' own.

// networkInterface is what the virtual-machine walk lists to learn which subnet
// a machine sits in: the machine names its interface, and only the interface
// names the subnet.
func networkInterface() *armnetwork.Interface {
	return &armnetwork.Interface{
		ID:       new(nicID),
		Name:     new("nic1"),
		Location: new("westeurope"),
		Properties: &armnetwork.InterfacePropertiesFormat{
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{{
				ID: new(nicID + "/ipConfigurations/ipconfig1"),
				Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
					Subnet: &armnetwork.Subnet{ID: new(subnetID)},
				},
			}},
		},
	}
}

// networkWatcher is the intermediate the flow-log walk lists through. It is
// never emitted as a node, which is why no converter test has one.
func networkWatcher() *armnetwork.Watcher {
	return &armnetwork.Watcher{
		ID:       new(rgID + "/providers/Microsoft.Network/networkWatchers/nw1"),
		Name:     new("nw1"),
		Location: new("westeurope"),
	}
}

// privateDNSLink is a private zone's link to a network.
//
// IT IS THE REAL TYPE, and that is not pedantry: an earlier version of this
// fixture used the network-peering type as a stand-in because the nesting
// looked the same, and the two spell the network field differently — peering
// says remoteVirtualNetwork, a link says virtualNetwork — so the walk decoded a
// link with no network and emitted nothing. The per-site census caught it; the
// type census could not, because another emitter produces that relationship.
func privateDNSLink() *armprivatedns.VirtualNetworkLink {
	return &armprivatedns.VirtualNetworkLink{
		ID:   new(privZoneID + "/virtualNetworkLinks/link1"),
		Name: new("link1"),
		Properties: &armprivatedns.VirtualNetworkLinkProperties{
			VirtualNetwork: &armprivatedns.SubResource{ID: new(vnetID)},
		},
	}
}

// censusRoleAssignment carries a principal type the walk's own filter can
// produce, which is ServicePrincipal.
func censusRoleAssignment() any { return roleAssignments()[0] }

func sqlPrivateEndpointConnection() *armsql.PrivateEndpointConnection {
	return &armsql.PrivateEndpointConnection{
		ID: new(sqlServerID + "/privateEndpointConnections/pec1"),
		Properties: &armsql.PrivateEndpointConnectionProperties{
			PrivateEndpoint: &armsql.PrivateEndpointProperty{ID: new(peID)},
		},
	}
}

func serviceBusNetworkRuleSet() *armservicebus.NetworkRuleSet {
	return &armservicebus.NetworkRuleSet{
		ID: new(sbNamespaceID + "/networkRuleSets/default"),
		Properties: &armservicebus.NetworkRuleSetProperties{
			VirtualNetworkRules: []*armservicebus.NWRuleSetVirtualNetworkRules{{
				Subnet: &armservicebus.Subnet{ID: new(subnetID)},
			}},
		},
	}
}

func eventHubNetworkRuleSet() *armeventhub.NetworkRuleSet {
	return &armeventhub.NetworkRuleSet{
		ID: new(ehNamespaceID + "/networkRuleSets/default"),
		Properties: &armeventhub.NetworkRuleSetProperties{
			VirtualNetworkRules: []*armeventhub.NWRuleSetVirtualNetworkRules{{
				Subnet: &armeventhub.Subnet{ID: new(subnetID)},
			}},
		},
	}
}

// siteFunction is one function inside a function app, carrying the binding
// configuration the trigger walk reads.
func siteFunction() *armappservice.FunctionEnvelope {
	return &armappservice.FunctionEnvelope{
		ID:   new(functionAppID + "/functions/process"),
		Name: new("process"),
		Properties: &armappservice.FunctionEnvelopeProperties{
			Config: functionBindings(),
		},
	}
}

func frontDoorOriginGroup() *armcdn.AFDOriginGroup {
	return &armcdn.AFDOriginGroup{
		ID:   new(profileID + "/originGroups/origins"),
		Name: new("origins"),
	}
}
