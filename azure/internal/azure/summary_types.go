// SPDX-License-Identifier: Apache-2.0

package azure

// summary_types.go — ONE ENTRY PER ARM RESOURCE TYPE, exact string, no
// wildcards. A type present in [armResourceTypes] and absent here is a gap the
// coverage test names.
//
// The nouns are what an operator would call the thing, not the ARM type: a
// search for "virtual machine" should find one.
var summarizers = map[string]summaryFunc{
	rtAPIMService:       detailed("API Management service", "skuName", "gatewayUrl"),
	rtAPIMAPI:           detailed("API Management API", "serviceUrl"),
	rtRedis:             detailed("Redis cache", "skuName", "redisVersion", "publicNetworkAccess"),
	rtCDNProfile:        detailed("Front Door profile", "skuName"),
	rtAFDEndpoint:       detailed("Front Door endpoint", "hostName"),
	rtDisk:              detailed("Managed disk", "skuName", "diskSizeGB"),
	rtVMSS:              detailed("Virtual machine scale set", "skuName", "capacity"),
	rtVM:                detailed("Virtual machine", "vmSize", "osType", "provisioningState"),
	rtRegistry:          detailed("Container registry", "skuName", "loginServer"),
	rtManagedCluster:    detailed("AKS cluster", "kubernetesVersion", "powerState"),
	rtCosmosAccount:     detailed("Cosmos DB account", "kind", "consistencyLevel"),
	rtEventGridSub:      simple("Event Grid subscription"),
	rtEventGridTopic:    detailed("Event Grid topic", "endpoint"),
	rtEventHubNS:        detailed("Event Hubs namespace", "skuName", "skuTier"),
	rtEventHub:          simple("Event hub"),
	rtConsumerGroup:     simple("Event hub consumer group"),
	rtAppInsights:       detailed("Application Insights component", "applicationType"),
	rtDiagnosticSetting: detailed("Diagnostic setting", "logsEnabled", "metricsEnabled"),
	rtMetricAlert:       detailed("Metric alert", "severity"),
	rtVault:             detailed("Key vault", "skuName", "enableSoftDelete", "enablePurgeProtection"),
	rtWorkflow:          detailed("Logic app workflow", "state"),
	rtManagedIdentity:   detailed("User-assigned managed identity", "clientId"),
	rtAppGateway:        detailed("Application gateway", "skuName"),
	rtFirewall:          detailed("Azure Firewall", "skuName", "threatIntelMode"),
	rtDNSZone:           detailed("Public DNS zone", "numberOfRecordSets"),
	rtDNSRecordSet:      detailed("DNS record set", "recordType", "ttl"),
	rtLoadBalancer:      detailed("Load balancer", "skuName"),
	rtNATGateway:        detailed("NAT gateway", "skuName"),
	rtNSG:               detailed("Network security group", "ruleCount"),
	rtFlowLog:           detailed("NSG flow log", "enabled", "retentionDays"),
	rtPrivateDNSZone:    simple("Private DNS zone"),
	rtPrivateEndpoint:   simple("Private endpoint"),
	rtVNet:              detailed("Virtual network", "addressPrefix_0"),
	rtSubnet:            detailed("Subnet", "addressPrefix"),
	rtVNetPeering:       simple("Virtual network peering"),
	rtLogAnalytics:      detailed("Log Analytics workspace", "retentionInDays"),
	rtSearchService:     detailed("AI Search service", "skuName", "replicaCount", "partitionCount"),
	rtServiceBusNS:      detailed("Service Bus namespace", "skuName", "skuTier"),
	rtServiceBusQueue:   simple("Service Bus queue"),
	rtServiceBusTopic:   simple("Service Bus topic"),
	rtServiceBusSub:     simple("Service Bus topic subscription"),
	rtSQLServer:         detailed("SQL server", "version", "state", "fqdn"),
	rtSQLDatabase:       detailed("SQL database", "skuName", "status"),
	rtStorageAccount:    detailed("Storage account", "kind", "skuName", "accessTier"),
	rtFileShare:         detailed("Azure Files share", "shareQuotaGiB", "accessTier"),
	rtSynapseWorkspace:  detailed("Synapse workspace", "publicNetworkAccess"),
	rtSparkPool:         detailed("Synapse Spark pool", "sparkVersion", "nodeCount", "nodeSize"),
	rtSQLPool:           detailed("Synapse dedicated SQL pool", "skuName", "status"),
	rtWebCertificate:    detailed("App Service certificate", "subjectName", "expirationDate"),
	rtWebSite:           detailed("App Service site", "kind", "state", "defaultHostName"),
	rtFunctionApp:       detailed("Function app", "kind", "state", "defaultHostName"),
}
