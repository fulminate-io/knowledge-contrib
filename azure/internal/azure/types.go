// SPDX-License-Identifier: Apache-2.0

package azure

// types.go — THE VOCABULARY: every resource type this collector emits and every
// relationship it draws, as named constants.
//
// WHY CONSTANTS AND NOT LITERALS AT THE EMISSION SITES. Both vocabularies are
// duplicated string literals with no compiler behind them: the graph accepts
// any string, so a typo in a resource type produces a node nothing queries and
// a typo in an edge type produces a relationship nothing traverses, both
// silently. Naming them here makes the vocabulary countable, gives every
// emission site one spelling, and makes an addition visible in one diff.

// The ARM resource types this collector enumerates. The value is the Azure
// resource type string exactly as ARM spells it, because that is what an
// operator filters on and what Azure's own documentation names.
const (
	rtAPIMService       = "Microsoft.ApiManagement/service"
	rtAPIMAPI           = "Microsoft.ApiManagement/service/apis"
	rtRedis             = "Microsoft.Cache/redis"
	rtCDNProfile        = "Microsoft.Cdn/profiles"
	rtAFDEndpoint       = "Microsoft.Cdn/profiles/afdEndpoints"
	rtDisk              = "Microsoft.Compute/disks"
	rtVMSS              = "Microsoft.Compute/virtualMachineScaleSets"
	rtVM                = "Microsoft.Compute/virtualMachines"
	rtRegistry          = "Microsoft.ContainerRegistry/registries"
	rtManagedCluster    = "Microsoft.ContainerService/managedClusters"
	rtCosmosAccount     = "Microsoft.DocumentDB/databaseAccounts"
	rtEventGridSub      = "Microsoft.EventGrid/eventSubscriptions"
	rtEventGridTopic    = "Microsoft.EventGrid/topics"
	rtEventHubNS        = "Microsoft.EventHub/namespaces"
	rtEventHub          = "Microsoft.EventHub/namespaces/eventhubs"
	rtConsumerGroup     = "Microsoft.EventHub/namespaces/eventhubs/consumergroups"
	rtAppInsights       = "Microsoft.Insights/components"
	rtDiagnosticSetting = "Microsoft.Insights/diagnosticSettings"
	rtMetricAlert       = "Microsoft.Insights/metricAlerts"
	rtVault             = "Microsoft.KeyVault/vaults"
	rtWorkflow          = "Microsoft.Logic/workflows"
	rtManagedIdentity   = "Microsoft.ManagedIdentity/userAssignedIdentities"
	rtAppGateway        = "Microsoft.Network/applicationGateways"
	rtFirewall          = "Microsoft.Network/azureFirewalls"
	rtDNSZone           = "Microsoft.Network/dnsZones"
	rtDNSRecordSet      = "Microsoft.Network/dnsZones/recordSets"
	rtLoadBalancer      = "Microsoft.Network/loadBalancers"
	rtNATGateway        = "Microsoft.Network/natGateways"
	rtNSG               = "Microsoft.Network/networkSecurityGroups"
	rtFlowLog           = "Microsoft.Network/networkWatchers/flowLogs"
	rtPrivateDNSZone    = "Microsoft.Network/privateDnsZones"
	rtPrivateEndpoint   = "Microsoft.Network/privateEndpoints"
	rtVNet              = "Microsoft.Network/virtualNetworks"
	rtSubnet            = "Microsoft.Network/virtualNetworks/subnets"
	rtVNetPeering       = "Microsoft.Network/virtualNetworks/virtualNetworkPeerings"
	rtLogAnalytics      = "Microsoft.OperationalInsights/workspaces"
	rtSearchService     = "Microsoft.Search/searchServices"
	rtServiceBusNS      = "Microsoft.ServiceBus/namespaces"
	rtServiceBusQueue   = "Microsoft.ServiceBus/namespaces/queues"
	rtServiceBusTopic   = "Microsoft.ServiceBus/namespaces/topics"
	rtServiceBusSub     = "Microsoft.ServiceBus/namespaces/topics/subscriptions"
	rtSQLServer         = "Microsoft.Sql/servers"
	rtSQLDatabase       = "Microsoft.Sql/servers/databases"
	rtStorageAccount    = "Microsoft.Storage/storageAccounts"
	rtFileShare         = "Microsoft.Storage/storageAccounts/fileServices/shares"
	rtSynapseWorkspace  = "Microsoft.Synapse/workspaces"
	rtSparkPool         = "Microsoft.Synapse/workspaces/bigDataPools"
	rtSQLPool           = "Microsoft.Synapse/workspaces/sqlPools"
	rtWebCertificate    = "Microsoft.Web/certificates"
	rtWebSite           = "Microsoft.Web/sites"
	rtFunctionApp       = "Microsoft.Web/sites/functionapp"
)

// armResourceTypes is every ARM resource type above, in the order a reader
// would enumerate them. It is what the coverage test counts and what a
// consumer can enumerate to know the collector's reach.
var armResourceTypes = []string{
	rtAPIMService, rtAPIMAPI, rtRedis, rtCDNProfile, rtAFDEndpoint, rtDisk, rtVMSS, rtVM,
	rtRegistry, rtManagedCluster, rtCosmosAccount, rtEventGridSub, rtEventGridTopic,
	rtEventHubNS, rtEventHub, rtConsumerGroup, rtAppInsights, rtDiagnosticSetting,
	rtMetricAlert, rtVault, rtWorkflow, rtManagedIdentity, rtAppGateway, rtFirewall,
	rtDNSZone, rtDNSRecordSet, rtLoadBalancer, rtNATGateway, rtNSG, rtFlowLog,
	rtPrivateDNSZone, rtPrivateEndpoint, rtVNet, rtSubnet, rtVNetPeering, rtLogAnalytics,
	rtSearchService, rtServiceBusNS, rtServiceBusQueue, rtServiceBusTopic, rtServiceBusSub,
	rtSQLServer, rtSQLDatabase, rtStorageAccount, rtFileShare, rtSynapseWorkspace,
	rtSparkPool, rtSQLPool, rtWebCertificate, rtWebSite, rtFunctionApp,
}

// The SYNTHETIC resource types: proxy nodes for things this collector
// references but cannot enumerate, because the reference does not carry the
// identity needed to reconstruct an ARM id. Their ids are namespaced so they
// can never collide with an ARM id, which always begins /subscriptions/.
const (
	rtAADGroup       = "azure:aad:group"
	rtAPIMBackend    = "azure:apim:backend"
	rtCertAuthority  = "azure:ca"
	rtVaultProxy     = "azure:keyvault:vault"
	rtNSGRule        = "azure:nsg:rule"
	rtSBQueueProxy   = "azure:servicebus:queue"
	rtSBTopicProxy   = "azure:servicebus:topic"
	rtEventHubProxy  = "azure:eventhub:hub"
	rtStorageQueue   = "azure:storage:queue"
	rtStorageBlob    = "azure:storage:blob"
	rtOIDCIdentity   = "oidc:identity"
	rtGitHubIdentity = "github:identity"
	// rtCIDRBlock is the resolver's own: the address range an NSG rule admits
	// or permits, which is not an Azure resource at all and exists only as the
	// far endpoint of a reachability edge.
	rtCIDRBlock = "azure-cidr-block"
)

// syntheticResourceTypes is every synthetic type above. Like
// [armResourceTypes], it is what the coverage test counts.
var syntheticResourceTypes = []string{
	rtAADGroup, rtAPIMBackend, rtCertAuthority, rtVaultProxy, rtNSGRule,
	rtSBQueueProxy, rtSBTopicProxy, rtEventHubProxy, rtStorageQueue, rtStorageBlob,
	rtOIDCIdentity, rtGitHubIdentity, rtCIDRBlock,
}

// The relationship vocabulary. Values are the graph's own edge type strings,
// which other collector families use for the same meanings, so a query written
// against one cloud reads the next.
const (
	edgeAccessedBy        = "ACCESSED_BY"
	edgeAllowsEgressTo    = "ALLOWS_EGRESS_TO"
	edgeAllowsIngressFrom = "ALLOWS_INGRESS_FROM"
	edgeAssumesRole       = "ASSUMES_ROLE"
	edgeBoundTo           = "BOUND_TO"
	edgeContains          = "CONTAINS"
	edgeDeadLettersTo     = "DEAD_LETTERS_TO"
	edgeEncryptsWith      = "ENCRYPTS_WITH"
	edgeHasMember         = "HAS_MEMBER"
	edgeIssuedBy          = "ISSUED_BY"
	edgeMemberOf          = "MEMBER_OF"
	edgeMonitors          = "MONITORS"
	edgeMountsSecret      = "MOUNTS_SECRET"
	edgePeeredWith        = "PEERED_WITH"
	edgeProtects          = "PROTECTS"
	edgeRoutesTo          = "ROUTES_TO"
	edgeSinksTo           = "SINKS_TO"
	edgeStoredIn          = "STORED_IN"
	edgeSubscribesTo      = "SUBSCRIBES_TO"
	edgeTargets           = "TARGETS"
	edgeTriggers          = "TRIGGERS"
	edgeTrusts            = "TRUSTS"
	edgeUsesCert          = "USES_CERT"
	edgeUsesImage         = "USES_IMAGE"
	edgeUsesNetwork       = "USES_NETWORK"
	edgeUsesSecurityGroup = "USES_SECURITY_GROUP"
	edgeUsesSubnet        = "USES_SUBNET"
	edgeWorkloadIdentity  = "WORKLOAD_IDENTITY"
)

// edgeTypes is every relationship above. The coverage test asserts that a
// fixture exists producing each one, so an edge type declared here and emitted
// nowhere is a failure rather than a decoration.
var edgeTypes = []string{
	edgeAccessedBy, edgeAllowsEgressTo, edgeAllowsIngressFrom, edgeAssumesRole,
	edgeBoundTo, edgeContains, edgeDeadLettersTo, edgeEncryptsWith, edgeHasMember,
	edgeIssuedBy, edgeMemberOf, edgeMonitors, edgeMountsSecret, edgePeeredWith,
	edgeProtects, edgeRoutesTo, edgeSinksTo, edgeStoredIn, edgeSubscribesTo,
	edgeTargets, edgeTriggers, edgeTrusts, edgeUsesCert, edgeUsesImage,
	edgeUsesNetwork, edgeUsesSecurityGroup, edgeUsesSubnet, edgeWorkloadIdentity,
}

// The EDGE METADATA KEYS the resolvers read by name. A key is an informal
// contract between the subcollector that stamps it and the resolver that gates
// on it: a resolver reading a key nothing stamps is dead code that still
// passes every count-the-types census, which is why both sides name the
// constant.
const (
	// mdPrincipalType is the Entra principal kind of a role assignment's
	// principal. Resolver 4's RBAC half and resolver 5's assumes-role half both
	// gate on it.
	mdPrincipalType = "principal_type"
	// mdIssuer is a federated credential's OIDC issuer. Resolver 4's federated
	// half gates on it.
	mdIssuer = "issuer"
	// mdSource distinguishes a vault's legacy access-policy grants from its
	// RBAC role assignments, which are two different arms feeding one edge type.
	mdSource = "source"
	// mdRoleSource marks an ASSUMES_ROLE edge drawn from a resource's attached
	// managed identity rather than from a role assignment.
	mdRoleSource = "role_source"
	mdRoleDefID  = "role_definition_id"
	mdSubject    = "subject"
	mdAudiences  = "audiences"
	mdTenantID   = "tenant_id"

	// The NODE metadata keys the resolvers read. tenantId and principalId are
	// stamped on a managed identity by its own walk and gate resolvers 4 and 5;
	// a walk that stops stamping either makes the resolver that reads it
	// produce nothing while every edge-type census still passes.
	mdNodeTenantID    = "tenantId"
	mdNodePrincipalID = "principalId"
	mdNodeClientID    = "clientId"
)
