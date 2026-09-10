// SPDX-License-Identifier: Apache-2.0

// Package gcpgraph is this collector's GRAPH VOCABULARY and its node/edge
// construction: the resource-type and edge-type parity floors, the id spellings
// the floors are joined on, and the one place a walk's intermediate resources
// and relations become contract nodes and edges.
//
// WHY THE FLOORS ARE CHECKED IN RATHER THAN DERIVED. Coverage of this
// collector's source-of-truth vocabulary is a REQUIREMENT, not an emergent
// property: a converter dropped in a refactor takes its resource type out of the
// emitted set silently, and a set derived from the code can never notice. The
// two lists below are the target the emitted set is asserted to be a superset
// of, so a dropped converter is a red rather than a smaller graph.
package gcpgraph

import "slices"

// The resource types this collector emits. Forty-eight are colon-namespaced
// under the GCP convention; the forty-ninth is the firewall CIDR sentinel, which
// is NOT, deliberately — see [ResourceTypeCIDRBlock].
const (
	ResourceTypeARRemote                = "gcp:ar:remote"
	ResourceTypeArtifactRegistryRepo    = "gcp:artifactregistry:repository"
	ResourceTypeBGPPeer                 = "gcp:bgp:peer"
	ResourceTypeBigQueryDataset         = "gcp:bigquery:dataset"
	ResourceTypeBigQueryTable           = "gcp:bigquery:table"
	ResourceTypeCloudFunction           = "gcp:cloudfunctions:function"
	ResourceTypeCloudIdentityGroup      = "gcp:cloudidentity:group"
	ResourceTypeKMSCryptoKey            = "gcp:cloudkms:cryptoKey"
	ResourceTypeKMSKeyRing              = "gcp:cloudkms:keyRing"
	ResourceTypeCloudTasksQueue         = "gcp:cloudtasks:queue"
	ResourceTypeBackendService          = "gcp:compute:backendService"
	ResourceTypeDisk                    = "gcp:compute:disk"
	ResourceTypeFirewall                = "gcp:compute:firewall"
	ResourceTypeForwardingRule          = "gcp:compute:forwardingRule"
	ResourceTypeInstance                = "gcp:compute:instance"
	ResourceTypeInstanceGroup           = "gcp:compute:instanceGroup"
	ResourceTypeNAT                     = "gcp:compute:nat"
	ResourceTypeNetwork                 = "gcp:compute:network"
	ResourceTypeRouter                  = "gcp:compute:router"
	ResourceTypeSecurityPolicy          = "gcp:compute:securityPolicy"
	ResourceTypeSSLCertificate          = "gcp:compute:sslCertificate"
	ResourceTypeSubnetwork              = "gcp:compute:subnetwork"
	ResourceTypeTargetHTTPProxy         = "gcp:compute:targetHttpProxy"
	ResourceTypeTargetHTTPSProxy        = "gcp:compute:targetHttpsProxy"
	ResourceTypeURLMap                  = "gcp:compute:urlMap"
	ResourceTypeGKECluster              = "gcp:container:cluster"
	ResourceTypeDataflowJob             = "gcp:dataflow:job"
	ResourceTypeDNSManagedZone          = "gcp:dns:managedZone"
	ResourceTypeDNSRecordSet            = "gcp:dns:recordSet"
	ResourceTypeEventarcTrigger         = "gcp:eventarc:trigger"
	ResourceTypeFilestoreInstance       = "gcp:file:instance"
	ResourceTypeFirestoreBackupSchedule = "gcp:firestore:backupSchedule"
	ResourceTypeFirestoreDatabase       = "gcp:firestore:database"
	ResourceTypeServiceAccount          = "gcp:iam:serviceAccount"
	ResourceTypeLoggingSink             = "gcp:logging:sink"
	ResourceTypeAlertPolicy             = "gcp:monitoring:alertPolicy"
	ResourceTypeNotificationChannel     = "gcp:monitoring:notificationChannel"
	ResourceTypePubSubSubscription      = "gcp:pubsub:subscription"
	ResourceTypePubSubTopic             = "gcp:pubsub:topic"
	ResourceTypeRedisInstance           = "gcp:redis:instance"
	ResourceTypeProject                 = "gcp:resourcemanager:project"
	ResourceTypeRunService              = "gcp:run:service"
	ResourceTypeSchedulerJob            = "gcp:scheduler:job"
	ResourceTypeSecret                  = "gcp:secretmanager:secret"
	ResourceTypeSQLInstance             = "gcp:sql:instance"
	ResourceTypeStorageBucket           = "gcp:storage:bucket"
	ResourceTypeVPCAccessConnector      = "gcp:vpcaccess:connector"
	ResourceTypeWorkflow                = "gcp:workflows:workflow"

	// ResourceTypeCIDRBlock is the firewall CIDR sentinel, and its spelling
	// BREAKS the colon-namespaced GCP convention every other type above follows.
	//
	// That is deliberate and it is not this collector's choice to make: coverage
	// is parity on the produced graph, never a behavior change, so the
	// vocabulary is carried forward verbatim. Normalizing it to gcp:cidr:block
	// would change the graph shape a consumer already queries on.
	ResourceTypeCIDRBlock = "gcp-cidr-block"
)

// The edge types this collector emits. Five of them — [EdgeAllowsIngressFrom],
// [EdgeAllowsEgressTo], [EdgeSharedWith], [EdgeUsesImage] and [EdgeTrusts] — are
// emitted by NO enumeration: they are DERIVED by the resolvers this module runs
// in memory before it returns, in place of a post-collect hook the contract
// gives a provider no place to run.
const (
	EdgeAllowsEgressTo    = "ALLOWS_EGRESS_TO"
	EdgeAllowsIngressFrom = "ALLOWS_INGRESS_FROM"
	EdgeBackedUpBy        = "BACKED_UP_BY"
	EdgeBoundTo           = "BOUND_TO"
	EdgeContains          = "CONTAINS"
	EdgeDeadLettersTo     = "DEAD_LETTERS_TO"
	EdgeEncryptsWith      = "ENCRYPTS_WITH"
	EdgeFromImage         = "FROM_IMAGE"
	EdgeFromSnapshot      = "FROM_SNAPSHOT"
	EdgeGrants            = "GRANTS"
	EdgeHasMember         = "HAS_MEMBER"
	EdgeMemberOf          = "MEMBER_OF"
	EdgeMonitors          = "MONITORS"
	EdgeMountsSecret      = "MOUNTS_SECRET"
	EdgeNotifiesVia       = "NOTIFIES_VIA"
	EdgePeeredWith        = "PEERED_WITH"
	EdgeProtects          = "PROTECTS"
	EdgeProxiesFrom       = "PROXIES_FROM"
	EdgeRoutesTo          = "ROUTES_TO"
	EdgeRoutesVia         = "ROUTES_VIA"
	EdgeSharedWith        = "SHARED_WITH"
	EdgeSinksTo           = "SINKS_TO"
	EdgeSubscribesTo      = "SUBSCRIBES_TO"
	EdgeTargets           = "TARGETS"
	EdgeTriggers          = "TRIGGERS"
	EdgeTrusts            = "TRUSTS"
	EdgeUsesCert          = "USES_CERT"
	EdgeUsesImage         = "USES_IMAGE"
	EdgeUsesNetwork       = "USES_NETWORK"
	EdgeUsesSA            = "USES_SA"
	EdgeUsesSubnet        = "USES_SUBNET"
	EdgeWorkloadIdentity  = "WORKLOAD_IDENTITY"
)

// resourceTypeFloor is the checked-in parity list. It is unexported and served
// through [ResourceTypes] as a COPY: a caller that could append to or rewrite
// this slice could make the superset assertion that reads it pass by shrinking
// the target rather than by emitting the type.
var resourceTypeFloor = []string{
	ResourceTypeARRemote,
	ResourceTypeArtifactRegistryRepo,
	ResourceTypeBGPPeer,
	ResourceTypeBigQueryDataset,
	ResourceTypeBigQueryTable,
	ResourceTypeCloudFunction,
	ResourceTypeCloudIdentityGroup,
	ResourceTypeKMSCryptoKey,
	ResourceTypeKMSKeyRing,
	ResourceTypeCloudTasksQueue,
	ResourceTypeBackendService,
	ResourceTypeDisk,
	ResourceTypeFirewall,
	ResourceTypeForwardingRule,
	ResourceTypeInstance,
	ResourceTypeInstanceGroup,
	ResourceTypeNAT,
	ResourceTypeNetwork,
	ResourceTypeRouter,
	ResourceTypeSecurityPolicy,
	ResourceTypeSSLCertificate,
	ResourceTypeSubnetwork,
	ResourceTypeTargetHTTPProxy,
	ResourceTypeTargetHTTPSProxy,
	ResourceTypeURLMap,
	ResourceTypeGKECluster,
	ResourceTypeDataflowJob,
	ResourceTypeDNSManagedZone,
	ResourceTypeDNSRecordSet,
	ResourceTypeEventarcTrigger,
	ResourceTypeFilestoreInstance,
	ResourceTypeFirestoreBackupSchedule,
	ResourceTypeFirestoreDatabase,
	ResourceTypeServiceAccount,
	ResourceTypeLoggingSink,
	ResourceTypeAlertPolicy,
	ResourceTypeNotificationChannel,
	ResourceTypePubSubSubscription,
	ResourceTypePubSubTopic,
	ResourceTypeRedisInstance,
	ResourceTypeProject,
	ResourceTypeRunService,
	ResourceTypeSchedulerJob,
	ResourceTypeSecret,
	ResourceTypeSQLInstance,
	ResourceTypeStorageBucket,
	ResourceTypeVPCAccessConnector,
	ResourceTypeWorkflow,
	ResourceTypeCIDRBlock,
}

// edgeTypeFloor is the checked-in parity list, served through [EdgeTypes] as a
// copy on the same terms as [resourceTypeFloor].
var edgeTypeFloor = []string{
	EdgeAllowsEgressTo,
	EdgeAllowsIngressFrom,
	EdgeBackedUpBy,
	EdgeBoundTo,
	EdgeContains,
	EdgeDeadLettersTo,
	EdgeEncryptsWith,
	EdgeFromImage,
	EdgeFromSnapshot,
	EdgeGrants,
	EdgeHasMember,
	EdgeMemberOf,
	EdgeMonitors,
	EdgeMountsSecret,
	EdgeNotifiesVia,
	EdgePeeredWith,
	EdgeProtects,
	EdgeProxiesFrom,
	EdgeRoutesTo,
	EdgeRoutesVia,
	EdgeSharedWith,
	EdgeSinksTo,
	EdgeSubscribesTo,
	EdgeTargets,
	EdgeTriggers,
	EdgeTrusts,
	EdgeUsesCert,
	EdgeUsesImage,
	EdgeUsesNetwork,
	EdgeUsesSA,
	EdgeUsesSubnet,
	EdgeWorkloadIdentity,
}

// ResourceTypes returns the resource-type parity floor.
func ResourceTypes() []string { return slices.Clone(resourceTypeFloor) }

// EdgeTypes returns the edge-type parity floor.
func EdgeTypes() []string { return slices.Clone(edgeTypeFloor) }
