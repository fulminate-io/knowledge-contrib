// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
)

// subs.go — THE SUBCOLLECTOR SET, one per Azure service.
//
// WHY ONE PER SERVICE RATHER THAN ONE PER RESOURCE TYPE. A service's list APIs
// share a client and often a parent: enumerating Service Bus queues means
// listing namespaces first, and splitting namespaces from queues would list the
// namespaces twice. WHY NOT ONE PER SUBSCRIPTION: a single walk would be a
// serial chain of three dozen round-trip latencies, and one service's failure
// would take the rest with it.
//
// EVERY SUBCOLLECTOR IS INDEPENDENT. None reads another's output; the
// relationships that need two services are the resolvers' work.

// subBase is the state every subcollector shares: which subscription it walks,
// what it authenticates with, and the ONE seam that makes a subcollector
// drivable without Azure.
type subBase struct {
	name           string
	cred           azureCredential
	subscriptionID string
	// transport replaces the HTTP transport every ARM client in this walk is
	// built on. It is nil in the shipped binary, where the SDK's own default
	// client talks to the management plane.
	//
	// WHY THE SEAM IS HERE AND NOT AT EACH LISTER. A collector's listing layer
	// is the half that pages, that stops at the first error and that wires the
	// response into the conversion functions, and none of it can be observed by
	// a test that calls the converters directly. Replacing the TRANSPORT drives
	// every one of those paths for real — the real client, the real pager, the
	// real decode — against responses the test wrote, which is the only way the
	// listers get executed at all without a subscription.
	transport policy.Transporter
}

func (s subBase) Name() string { return s.name }

// clientOptions is what every ARM client in this walk is constructed with.
//
// IT RETURNS NIL WHEN NO TRANSPORT IS SET, which is the shipped path: passing
// nil options is exactly what each constructor received before this seam
// existed, so the production behaviour is unchanged by construction rather than
// by inspection.
func (s subBase) clientOptions() *arm.ClientOptions {
	if s.transport == nil {
		return nil
	}
	return &arm.ClientOptions{ClientOptions: policy.ClientOptions{Transport: s.transport}}
}

// shared exposes the state every subcollector was built with, so a test can
// assert that each one actually received the subscription and credential it
// will authenticate with rather than inferring it from a successful call.
func (s subBase) shared() subBase { return s }

// The subcollector names. They are operator-facing: an incomplete walk names
// the ones that failed, so they read as service names rather than as Go
// identifiers.
const (
	nameAADGroups        = "azure-aad-groups"
	nameAKS              = "azure-aks"
	nameAPIM             = "azure-apim"
	nameAppGateways      = "azure-appgateways"
	nameAppService       = "azure-appservice"
	nameCertificates     = "azure-certificates"
	nameContainerReg     = "azure-containerregistry"
	nameCosmosDB         = "azure-cosmosdb"
	nameDisks            = "azure-disks"
	nameDNS              = "azure-dns"
	nameEventGrid        = "azure-eventgrid"
	nameEventHubs        = "azure-eventhubs"
	nameFirewalls        = "azure-firewalls"
	nameFlowLogs         = "azure-flowlogs"
	nameFrontDoor        = "azure-frontdoor"
	nameFunctions        = "azure-functions"
	nameIdentity         = "azure-identity"
	nameKeyVault         = "azure-keyvault"
	nameLoadBalancers    = "azure-loadbalancers"
	nameLogicApps        = "azure-logic-apps"
	nameMonitoring       = "azure-monitoring"
	nameMonitoringAlerts = "azure-monitoring-alerts"
	nameNATGateways      = "azure-natgateways"
	nameNSGs             = "azure-nsgs"
	namePrivateDNS       = "azure-private-dns"
	namePrivateEndpoints = "azure-private-endpoints"
	nameRedis            = "azure-redis"
	nameSearch           = "azure-search"
	nameServiceBus       = "azure-servicebus"
	nameSQL              = "azure-sql"
	nameStorage          = "azure-storage"
	nameSynapse          = "azure-synapse"
	nameVMs              = "azure-vms"
	nameVMSS             = "azure-vmss"
	nameVNetPeering      = "azure-vnet-peering"
	nameVNets            = "azure-vnets"
)

// subCollectorNames is every subcollector this walk runs, in the order
// buildSubCollectors returns them. The coverage test reads it.
var subCollectorNames = []string{
	nameVMs, nameVMSS, nameDisks, nameVNets, nameNSGs, nameVNetPeering,
	nameLoadBalancers, nameAppGateways, nameFirewalls, nameNATGateways,
	namePrivateEndpoints, nameFlowLogs, nameDNS, namePrivateDNS,
	nameAKS, nameContainerReg, nameIdentity, nameAADGroups, nameKeyVault,
	nameStorage, nameSQL, nameCosmosDB, nameRedis, nameSearch, nameSynapse,
	nameServiceBus, nameEventHubs, nameEventGrid, nameAppService, nameFunctions,
	nameCertificates, nameAPIM, nameLogicApps, nameFrontDoor,
	nameMonitoring, nameMonitoringAlerts,
}

// buildSubCollectors returns the walk's subcollectors, sharing one credential.
//
// THE ORDER IS THE MERGE ORDER. The runner fans them out concurrently but
// merges their results in this order, so the sequence a collect produces
// depends on this list rather than on which goroutine finished first.
func buildSubCollectors(cred azureCredential, subID string) []subCollector {
	return buildSubCollectorsOn(cred, subID, nil)
}

// buildSubCollectorsOn is buildSubCollectors with the transport seam supplied.
// The shipped path calls it with nil; the census drives the whole set through
// one fake transport.
func buildSubCollectorsOn(cred azureCredential, subID string, transport policy.Transporter) []subCollector {
	base := func(name string) subBase {
		return subBase{name: name, cred: cred, subscriptionID: subID, transport: transport}
	}
	return []subCollector{
		&vmSub{subBase: base(nameVMs)},
		&vmssSub{subBase: base(nameVMSS)},
		&diskSub{subBase: base(nameDisks)},
		&vnetSub{subBase: base(nameVNets)},
		&nsgSub{subBase: base(nameNSGs)},
		&vnetPeeringSub{subBase: base(nameVNetPeering)},
		&loadBalancerSub{subBase: base(nameLoadBalancers)},
		&appGatewaySub{subBase: base(nameAppGateways)},
		&firewallSub{subBase: base(nameFirewalls)},
		&natGatewaySub{subBase: base(nameNATGateways)},
		&privateEndpointSub{subBase: base(namePrivateEndpoints)},
		&flowLogSub{subBase: base(nameFlowLogs)},
		&dnsSub{subBase: base(nameDNS)},
		&privateDNSSub{subBase: base(namePrivateDNS)},
		&aksSub{subBase: base(nameAKS)},
		&registrySub{subBase: base(nameContainerReg)},
		&identitySub{subBase: base(nameIdentity)},
		&aadGroupSub{subBase: base(nameAADGroups)},
		&keyVaultSub{subBase: base(nameKeyVault)},
		&storageSub{subBase: base(nameStorage)},
		&sqlSub{subBase: base(nameSQL)},
		&cosmosSub{subBase: base(nameCosmosDB)},
		&redisSub{subBase: base(nameRedis)},
		&searchSub{subBase: base(nameSearch)},
		&synapseSub{subBase: base(nameSynapse)},
		&serviceBusSub{subBase: base(nameServiceBus)},
		&eventHubSub{subBase: base(nameEventHubs)},
		&eventGridSub{subBase: base(nameEventGrid)},
		&appServiceSub{subBase: base(nameAppService)},
		&functionsSub{subBase: base(nameFunctions)},
		&certificateSub{subBase: base(nameCertificates)},
		&apimSub{subBase: base(nameAPIM)},
		&logicAppSub{subBase: base(nameLogicApps)},
		&frontDoorSub{subBase: base(nameFrontDoor)},
		&monitoringSub{subBase: base(nameMonitoring)},
		&metricAlertSub{subBase: base(nameMonitoringAlerts)},
	}
}

// pager is the shape every ARM list pager has. Declaring it lets a walk take a
// pager as an argument, which is how the tests that need a faked LIST rather
// than a hand-built response struct supply one.
type pager[T any] interface {
	More() bool
	NextPage(ctx context.Context) (T, error)
}

// drain walks a pager to exhaustion, calling visit for each page, and returns
// the first error.
//
// THE CONTEXT REACHES EVERY PAGE, which is what makes a cancelled collect stop
// at the next page boundary rather than paging a large subscription to the end.
// The SDK's own retryer handles 429 and 5xx with backoff, so there is
// deliberately no retry loop here: a second one would multiply the first's
// waits and would retry a cancellation the SDK fast-fails.
func drain[T any](ctx context.Context, p pager[T], visit func(T) error) error {
	for p.More() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return err
		}
		if err := visit(page); err != nil {
			return err
		}
	}
	return nil
}
