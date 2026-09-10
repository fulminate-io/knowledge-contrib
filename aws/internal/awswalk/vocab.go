// SPDX-License-Identifier: Apache-2.0

// Package awswalk is the AWS collector's WALK: it enumerates an account's
// resources through the AWS SDK and returns them as the collector contract's
// nodes and edges. Nothing about MCP, JSON Schema or the contract envelope lives
// here — the collector framework holds all three, and this package implements
// its Collector interface and nothing else.
package awswalk

// vocab.go — THE PRODUCED VOCABULARY, and it is a FLOOR rather than a menu.
//
// The ticket's coverage target is parity with the vocabulary the built-in AWS
// collector produces: one node type, the resource_type values below, and the
// edge relationship types below. A censusing test asserts that a walk over a
// fully-populated fake account emits every one of them, so a resource type
// dropped from a service walk turns that test red BY NAME rather than shrinking
// the graph silently.
//
// THE CONSTANTS EXIST SO THE CENSUS CAN BE WRITTEN AT ALL. A walk that spelled
// its resource types inline would give the census nothing to compare against but
// a second inline list, and two hand-written lists agree with each other rather
// than with the code.

// NodeTypeCloudResource is the ONE node type this collector emits. Every node —
// every service resource and every derived CIDR sentinel — carries it, and the
// resource kind rides in the `resource_type` metadata key rather than in the
// node type, which is the shape the built-in collector's graphs already have.
const NodeTypeCloudResource = "cloud-resource"

// Metadata keys every node carries. They are named here rather than spelled at
// each construction site because a consumer reads them by name: `resource_type`
// is what the census, the cross-graph linker and every topology query select on.
const (
	MetaResourceType = "resource_type"
	MetaRegion       = "region"
	MetaAccount      = "account"
)

// The resource types, one constant per value, grouped by the service walk that
// produces it. FIFTY-FOUR in all: fifty-three from the service walks and
// ResourceTypeCIDRBlock from the derived rules pass.
const (
	// Networking.
	ResourceTypeVPC                  = "vpc"
	ResourceTypeSubnet               = "subnet"
	ResourceTypeSecurityGroup        = "security-group"
	ResourceTypeNetworkACL           = "network-acl"
	ResourceTypeVPCPeeringConnection = "vpc-peering-connection"
	ResourceTypeTransitGateway       = "transit-gateway"
	ResourceTypeTransitGatewayAttach = "transit-gateway-attachment"
	ResourceTypeVPCEndpoint          = "vpc-endpoint"
	ResourceTypeInternetGateway      = "internet-gateway"
	ResourceTypeNATGateway           = "nat-gateway"
	ResourceTypeFlowLog              = "flow-log"

	// Compute and storage volumes.
	ResourceTypeEC2Instance = "ec2-instance"
	ResourceTypeEBSVolume   = "ebs-volume"
	ResourceTypeLambda      = "lambda-function"

	// Containers.
	ResourceTypeEKSCluster    = "eks-cluster"
	ResourceTypeECSCluster    = "ecs-cluster"
	ResourceTypeECSService    = "ecs-service"
	ResourceTypeECRRepository = "ecr-repository"

	// Data stores.
	ResourceTypeRDSInstance               = "rds-instance"
	ResourceTypeRedshiftCluster           = "redshift-cluster"
	ResourceTypeDynamoDBTable             = "dynamodb-table"
	ResourceTypeDynamoDBBackup            = "dynamodb-backup"
	ResourceTypeDynamoDBPITR              = "dynamodb-pitr"
	ResourceTypeElastiCacheCluster        = "elasticache-cluster"
	ResourceTypeElastiCacheReplicationGrp = "elasticache-replication-group"
	ResourceTypeOpenSearchDomain          = "opensearch-domain"
	ResourceTypeEFSFileSystem             = "efs-filesystem"

	// Object storage, secrets and keys.
	ResourceTypeS3Bucket           = "s3-bucket"
	ResourceTypeSecretsManagerItem = "secretsmanager-secret"
	ResourceTypeKMSKey             = "kms-key"

	// Edge and delivery.
	ResourceTypeCloudFrontDistribution = "cloudfront-distribution"
	ResourceTypeACMCertificate         = "acm-certificate"
	ResourceTypeRoute53HostedZone      = "route53-hostedzone"
	ResourceTypeELBv2LoadBalancer      = "elbv2-loadbalancer"
	ResourceTypeELBv2TargetGroup       = "elbv2-targetgroup"

	// API Gateway. The colon-separated spellings are the built-in collector's
	// own and are kept verbatim: they are the values a consumer's saved query
	// selects on.
	ResourceTypeAPIGWRestAPI = "apigw:restapi"
	ResourceTypeAPIGWHTTPAPI = "apigw:httpapi"
	ResourceTypeAPIGWWSAPI   = "apigw:wsapi"
	ResourceTypeAPIGWDomain  = "apigw:domain"

	// Identity.
	ResourceTypeIAMRole   = "iam-role"
	ResourceTypeIAMUser   = "iam-user"
	ResourceTypeIAMGroup  = "iam-group"
	ResourceTypeIAMPolicy = "iam-policy"

	// Messaging and orchestration.
	ResourceTypeSQSQueue                  = "sqs-queue"
	ResourceTypeSNSTopic                  = "sns-topic"
	ResourceTypeEventBridgeRule           = "eventbridge-rule"
	ResourceTypeKinesisStream             = "kinesis-stream"
	ResourceTypeStepFunctionsStateMachine = "stepfunctions-statemachine"

	// Observability and mail.
	ResourceTypeCloudWatchAlarm    = "cloudwatch-alarm"
	ResourceTypeCloudWatchLogGroup = "cloudwatch-loggroup"
	ResourceTypeCloudTrailTrail    = "cloudtrail-trail"
	ResourceTypeSESIdentity        = "ses-identity"

	// DERIVED, and emitted by no service walk: one sentinel per distinct CIDR
	// any security-group or network-ACL rule references, so both endpoints of
	// every ALLOWS edge are nodes in the same result.
	ResourceTypeCIDRBlock = "cidr-block"
)

// AllResourceTypes is the parity floor as a list. The censusing test walks a
// fully-populated fake account and asserts every entry appears; a service walk
// that stops emitting one turns it red naming the value.
//
// IT IS FIFTY-THREE, NOT FIFTY-FOUR, and the missing one is declared in
// ParityGaps rather than left as a discrepancy for a reader to discover. A count
// that does not match the reference collector's is a claim about coverage, and a
// claim about coverage is only honest when the shortfall is named.
var AllResourceTypes = []string{
	ResourceTypeACMCertificate,
	ResourceTypeAPIGWDomain,
	ResourceTypeAPIGWHTTPAPI,
	ResourceTypeAPIGWRestAPI,
	ResourceTypeAPIGWWSAPI,
	ResourceTypeCIDRBlock,
	ResourceTypeCloudFrontDistribution,
	ResourceTypeCloudTrailTrail,
	ResourceTypeCloudWatchAlarm,
	ResourceTypeCloudWatchLogGroup,
	ResourceTypeDynamoDBBackup,
	ResourceTypeDynamoDBPITR,
	ResourceTypeDynamoDBTable,
	ResourceTypeEBSVolume,
	ResourceTypeEC2Instance,
	ResourceTypeECRRepository,
	ResourceTypeECSCluster,
	ResourceTypeECSService,
	ResourceTypeEFSFileSystem,
	ResourceTypeEKSCluster,
	ResourceTypeElastiCacheCluster,
	ResourceTypeElastiCacheReplicationGrp,
	ResourceTypeELBv2LoadBalancer,
	ResourceTypeELBv2TargetGroup,
	ResourceTypeEventBridgeRule,
	ResourceTypeFlowLog,
	ResourceTypeIAMGroup,
	ResourceTypeIAMPolicy,
	ResourceTypeIAMRole,
	ResourceTypeIAMUser,
	ResourceTypeInternetGateway,
	ResourceTypeKinesisStream,
	ResourceTypeKMSKey,
	ResourceTypeLambda,
	ResourceTypeNATGateway,
	ResourceTypeNetworkACL,
	ResourceTypeOpenSearchDomain,
	ResourceTypeRDSInstance,
	ResourceTypeRedshiftCluster,
	ResourceTypeRoute53HostedZone,
	ResourceTypeS3Bucket,
	ResourceTypeSecretsManagerItem,
	ResourceTypeSecurityGroup,
	ResourceTypeSESIdentity,
	ResourceTypeSNSTopic,
	ResourceTypeSQSQueue,
	ResourceTypeStepFunctionsStateMachine,
	ResourceTypeSubnet,
	ResourceTypeTransitGateway,
	ResourceTypeTransitGatewayAttach,
	ResourceTypeVPC,
	ResourceTypeVPCEndpoint,
	ResourceTypeVPCPeeringConnection,
}

// ParityGap is one resource_type the reference collector emits that this
// collector does NOT, together with why and what would close it.
//
// WHY THIS EXISTS AS A DECLARATION rather than as a smaller number. The parity
// floor used to be a list of resource_type STRINGS with nothing tying a string to
// the API operation that legitimately produces it, so the floor could be met by
// emitting an object of a different kind under the right label — which is exactly
// what happened for ses-receipt-rule, where SES EMAIL TEMPLATES were relabelled as
// receipt rules. Deleting the type and quietly moving the count to 53 would fix
// the lie and lose the information. Declaring the gap keeps the shortfall in the
// source, keeps it in the census's failure text, and gives the closing condition
// a name a test can check.
type ParityGap struct {
	// ResourceType is the value the reference collector emits and this one does not.
	ResourceType string
	// Reason is why, in one sentence, for whoever reads the census failure.
	Reason string
	// SDKModule is the pinned module that would have to gain the operation, and
	// OperationGlob is the api_op_*.go filename shape that would appear in it. The
	// census matches these against the module cache, so a future SDK bump that
	// gains the operation REDS rather than silently leaving the gap declared.
	SDKModule     string
	OperationGlob string
}

// ParityGaps is the declared shortfall against the reference collector's
// vocabulary. len(AllResourceTypes) + len(ParityGaps) is the reference count.
var ParityGaps = []ParityGap{{
	ResourceType: "ses-receipt-rule",
	Reason: "the pinned sesv2 SDK exposes no receipt-rule operation at all; receipt rules live in the v1 " +
		"ses package, which this module does not depend on, and the reference collector reads them there " +
		"through DescribeActiveReceiptRuleSet",
	SDKModule:     "github.com/aws/aws-sdk-go-v2/service/sesv2",
	OperationGlob: "api_op_*Receipt*.go",
}}

// ReferenceResourceTypeCount is the size of the reference collector's
// resource_type vocabulary, which is the number the ticket's parity target names.
const ReferenceResourceTypeCount = 54

// The edge relationship types. THIRTY-THREE, the built-in collector's set,
// spelled exactly as it spells them: a consumer traverses by this string.
const (
	EdgeAllowsEgressTo       = "ALLOWS_EGRESS_TO"
	EdgeAllowsIngressFrom    = "ALLOWS_INGRESS_FROM"
	EdgeAssociatedWithSubnet = "ASSOCIATED_WITH_SUBNET"
	EdgeAssumesRole          = "ASSUMES_ROLE"
	EdgeBackedUpBy           = "BACKED_UP_BY"
	EdgeBoundTo              = "BOUND_TO"
	EdgeContains             = "CONTAINS"
	EdgeDeadLettersTo        = "DEAD_LETTERS_TO"
	EdgeEncryptsWith         = "ENCRYPTS_WITH"
	EdgeExposedVia           = "EXPOSED_VIA"
	EdgeGrants               = "GRANTS"
	EdgeHasMember            = "HAS_MEMBER"
	EdgeMemberOf             = "MEMBER_OF"
	EdgeMonitors             = "MONITORS"
	EdgeNotifiesVia          = "NOTIFIES_VIA"
	EdgePeeredWith           = "PEERED_WITH"
	EdgeProtects             = "PROTECTS"
	EdgeReplicatesTo         = "REPLICATES_TO"
	EdgeRoutesTo             = "ROUTES_TO"
	EdgeRoutesToPeer         = "ROUTES_TO_PEER"
	EdgeRoutesVia            = "ROUTES_VIA"
	EdgeSinksTo              = "SINKS_TO"
	EdgeTargets              = "TARGETS"
	EdgeTriggers             = "TRIGGERS"
	EdgeTrusts               = "TRUSTS"
	EdgeUsesCert             = "USES_CERT"
	EdgeUsesImage            = "USES_IMAGE"
	EdgeUsesNetwork          = "USES_NETWORK"
	EdgeUsesSA               = "USES_SA"
	EdgeUsesSecurityGroup    = "USES_SECURITY_GROUP"
	EdgeUsesSubnet           = "USES_SUBNET"
	EdgeValidatedBy          = "VALIDATED_BY"
	EdgeWorkloadIdentity     = "WORKLOAD_IDENTITY"
)

// AllEdgeTypes is the edge half of the parity floor, on the same terms as
// AllResourceTypes.
var AllEdgeTypes = []string{
	EdgeAllowsEgressTo,
	EdgeAllowsIngressFrom,
	EdgeAssociatedWithSubnet,
	EdgeAssumesRole,
	EdgeBackedUpBy,
	EdgeBoundTo,
	EdgeContains,
	EdgeDeadLettersTo,
	EdgeEncryptsWith,
	EdgeExposedVia,
	EdgeGrants,
	EdgeHasMember,
	EdgeMemberOf,
	EdgeMonitors,
	EdgeNotifiesVia,
	EdgePeeredWith,
	EdgeProtects,
	EdgeReplicatesTo,
	EdgeRoutesTo,
	EdgeRoutesToPeer,
	EdgeRoutesVia,
	EdgeSinksTo,
	EdgeTargets,
	EdgeTriggers,
	EdgeTrusts,
	EdgeUsesCert,
	EdgeUsesImage,
	EdgeUsesNetwork,
	EdgeUsesSA,
	EdgeUsesSecurityGroup,
	EdgeUsesSubnet,
	EdgeValidatedBy,
	EdgeWorkloadIdentity,
}
