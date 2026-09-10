module github.com/fulminate-io/knowledge-contrib/aws

go 1.26.4

toolchain go1.26.8

// Dependency-purity lock: this module is a pure PEER-CONSUMER of the collector
// contract, which is JSON over MCP rather than a Go type. It requires the
// collector framework and NOTHING ELSE OF OURS: not cmd/knowledge, not
// cmd/knowledge-server, not gen/, and not the root module. The client's own
// implementation of the contract lives under cmd/knowledge/internal, which the
// toolchain refuses to import from here in any case.
//
// The framework arrives as a `require` AND a `replace ../framework`. A bare
// workspace-resolved import builds but cannot be tidied and writes no go.sum,
// and the CI cache family names this module's go.sum by explicit path.
require github.com/fulminate-io/knowledge-contrib/framework v0.0.0-00010101000000-000000000000

require (
	github.com/aws/aws-sdk-go-v2 v1.46.0
	github.com/aws/aws-sdk-go-v2/config v1.32.14
	github.com/aws/aws-sdk-go-v2/service/acm v1.49.0
	github.com/aws/aws-sdk-go-v2/service/apigateway v1.46.0
	github.com/aws/aws-sdk-go-v2/service/apigatewayv2 v1.41.0
	github.com/aws/aws-sdk-go-v2/service/cloudfront v1.72.0
	github.com/aws/aws-sdk-go-v2/service/cloudtrail v1.63.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatch v1.71.0
	github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs v1.86.0
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.67.0
	github.com/aws/aws-sdk-go-v2/service/ec2 v1.329.0
	github.com/aws/aws-sdk-go-v2/service/ecr v1.64.0
	github.com/aws/aws-sdk-go-v2/service/ecs v1.96.0
	github.com/aws/aws-sdk-go-v2/service/efs v1.48.0
	github.com/aws/aws-sdk-go-v2/service/eks v1.98.0
	github.com/aws/aws-sdk-go-v2/service/elasticache v1.60.0
	github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2 v1.62.0
	github.com/aws/aws-sdk-go-v2/service/eventbridge v1.53.0
	github.com/aws/aws-sdk-go-v2/service/iam v1.63.0
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.53.0
	github.com/aws/aws-sdk-go-v2/service/kms v1.59.0
	github.com/aws/aws-sdk-go-v2/service/lambda v1.107.0
	github.com/aws/aws-sdk-go-v2/service/opensearch v1.79.0
	github.com/aws/aws-sdk-go-v2/service/rds v1.128.0
	github.com/aws/aws-sdk-go-v2/service/redshift v1.70.0
	github.com/aws/aws-sdk-go-v2/service/route53 v1.69.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.111.0
	github.com/aws/aws-sdk-go-v2/service/secretsmanager v1.48.0
	github.com/aws/aws-sdk-go-v2/service/sesv2 v1.72.0
	github.com/aws/aws-sdk-go-v2/service/sfn v1.49.0
	github.com/aws/aws-sdk-go-v2/service/sns v1.46.0
	github.com/aws/aws-sdk-go-v2/service/sqs v1.51.0
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.10
	github.com/modelcontextprotocol/go-sdk v1.7.0
)

require (
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.20 // indirect
	github.com/aws/aws-sdk-go-v2/credentials v1.19.14 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.5.2 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.8.2 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.6 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.5.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.19 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.11.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.13.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.14.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.20.2 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.9 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.15 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.19 // indirect
	github.com/aws/smithy-go v1.28.1 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/fulminate-io/knowledge-contrib/framework => ../framework
