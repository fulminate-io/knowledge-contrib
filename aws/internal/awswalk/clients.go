// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// clients.go — THE SERVICE CLIENTS, HELD AS INTERFACES.
//
// EVERY FIELD IS A NARROW INTERFACE declaring exactly the operations this
// collector calls, never the SDK's concrete client. That is what lets the whole
// walk be tested without an AWS credential, an HTTP endpoint or a network: a test
// supplies a struct answering those methods with in-memory responses. It is the
// idiom the built-in collector already uses for the same reason, and here it is
// the DEFAULT rather than the exception, because the owner deferred live testing
// and these fakes are the only thing standing between this walk and no coverage.
//
// THE INTERFACES ARE DECLARED BESIDE THEIR WALKS rather than in this file: a
// reader asking what a service walk calls should find the answer in the file that
// calls it, and this struct is the assembly point.

// Clients holds one client per AWS service this collector reads.
type Clients struct {
	ACM            acmAPI
	APIGateway     apiGatewayAPI
	APIGatewayV2   apiGatewayV2API
	CloudFront     cloudFrontAPI
	CloudTrail     cloudTrailAPI
	CloudWatch     cloudWatchAPI
	CloudWatchLogs cloudWatchLogsAPI
	DynamoDB       dynamoDBAPI
	EC2            ec2API
	ECR            ecrAPI
	ECS            ecsAPI
	EFS            efsAPI
	EKS            eksAPI
	ElastiCache    elastiCacheAPI
	ELBv2          elbv2API
	EventBridge    eventBridgeAPI
	IAM            iamAPI
	Kinesis        kinesisAPI
	KMS            kmsAPI
	Lambda         lambdaAPI
	OpenSearch     openSearchAPI
	RDS            rdsAPI
	Redshift       redshiftAPI
	Route53        route53API
	S3             s3API
	SecretsManager secretsManagerAPI
	SES            sesAPI
	SFN            sfnAPI
	SNS            snsAPI
	SQS            sqsAPI
}

// NewClients builds every service client from one resolved AWS config.
//
// ONE CONFIG FOR EVERY SERVICE, and no per-service credential handling: the
// credential chain resolves once, and each client inherits the same credentials,
// region and endpoint resolution. That is also what makes an endpoint override —
// AWS_ENDPOINT_URL, or a per-service AWS_ENDPOINT_URL_<SERVICE> — reach every
// client, which is why those names are in the example entry's environment block.
func NewClients(cfg aws.Config) *Clients {
	return &Clients{
		ACM:            acm.NewFromConfig(cfg),
		APIGateway:     apigateway.NewFromConfig(cfg),
		APIGatewayV2:   apigatewayv2.NewFromConfig(cfg),
		CloudFront:     cloudfront.NewFromConfig(cfg),
		CloudTrail:     cloudtrail.NewFromConfig(cfg),
		CloudWatch:     cloudwatch.NewFromConfig(cfg),
		CloudWatchLogs: cloudwatchlogs.NewFromConfig(cfg),
		DynamoDB:       dynamodb.NewFromConfig(cfg),
		EC2:            ec2.NewFromConfig(cfg),
		ECR:            ecr.NewFromConfig(cfg),
		ECS:            ecs.NewFromConfig(cfg),
		EFS:            efs.NewFromConfig(cfg),
		EKS:            eks.NewFromConfig(cfg),
		ElastiCache:    elasticache.NewFromConfig(cfg),
		ELBv2:          elasticloadbalancingv2.NewFromConfig(cfg),
		EventBridge:    eventbridge.NewFromConfig(cfg),
		IAM:            iam.NewFromConfig(cfg),
		Kinesis:        kinesis.NewFromConfig(cfg),
		KMS:            kms.NewFromConfig(cfg),
		Lambda:         lambda.NewFromConfig(cfg),
		OpenSearch:     opensearch.NewFromConfig(cfg),
		RDS:            rds.NewFromConfig(cfg),
		Redshift:       redshift.NewFromConfig(cfg),
		Route53:        route53.NewFromConfig(cfg),
		S3:             s3.NewFromConfig(cfg),
		SecretsManager: secretsmanager.NewFromConfig(cfg),
		SES:            sesv2.NewFromConfig(cfg),
		SFN:            sfn.NewFromConfig(cfg),
		SNS:            sns.NewFromConfig(cfg),
		SQS:            sqs.NewFromConfig(cfg),
	}
}

// allServiceWalks is the full list of subcollectors, in a FIXED order.
//
// The order does not decide the output — the fan-out runs them concurrently and
// the sink sorts — but it decides the order failures are reported in when several
// services fail at once, and a list a reader can scan is what says at a glance
// which services this collector covers.
func allServiceWalks() []serviceWalk {
	return []serviceWalk{
		{"acm", walkACM},
		{"apigateway", walkAPIGateway},
		{"apigatewayv2", walkAPIGatewayV2},
		{"cloudfront", walkCloudFront},
		{"cloudtrail", walkCloudTrail},
		{"cloudwatch", walkCloudWatch},
		{"dynamodb", walkDynamoDB},
		{"ebs", walkEBSVolumes},
		{"ec2", walkEC2Instances},
		{"ecr", walkECR},
		{"ecs", walkECS},
		{"efs", walkEFS},
		{"eks", walkEKS},
		{"elasticache", walkElastiCache},
		{"elbv2", walkELBv2},
		{"eventbridge", walkEventBridge},
		{"flowlogs", walkFlowLogs},
		{"iam-group", walkIAMGroups},
		{"iam-policy", walkIAMPolicies},
		{"iam-role", walkIAMRoles},
		{"iam-user", walkIAMUsers},
		{"igw", walkInternetGateways},
		{"kinesis", walkKinesis},
		{"kms", walkKMS},
		{"lambda", walkLambda},
		{"natgateway", walkNATGateways},
		{"networkacl", walkNetworkACLs},
		{"opensearch", walkOpenSearch},
		{"rds", walkRDS},
		{"redshift", walkRedshift},
		{"route53", walkRoute53},
		{"s3", walkS3},
		{"secretsmanager", walkSecretsManager},
		{"securitygroup", walkSecurityGroups},
		{"ses", walkSES},
		{"sns", walkSNS},
		{"sqs", walkSQS},
		{"stepfunctions", walkStepFunctions},
		{"subnet", walkSubnets},
		{"transit-gateway", walkTransitGateways},
		{"vpc", walkVPCs},
		{"vpc-endpoint", walkVPCEndpoints},
		{"vpc-peering", walkVPCPeering},
	}
}
