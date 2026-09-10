// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/aws/aws-sdk-go-v2/service/acm"
	acmtypes "github.com/aws/aws-sdk-go-v2/service/acm/types"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	apigatewaytypes "github.com/aws/aws-sdk-go-v2/service/apigateway/types"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	apigatewayv2types "github.com/aws/aws-sdk-go-v2/service/apigatewayv2/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	cloudfronttypes "github.com/aws/aws-sdk-go-v2/service/cloudfront/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	cloudwatchtypes "github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	cloudwatchlogstypes "github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	kinesistypes "github.com/aws/aws-sdk-go-v2/service/kinesis/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	secretsmanagertypes "github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sesv2types "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/aws-sdk-go-v2/service/sfn"
	sfntypes "github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// fixture_edge_test.go — the edge, delivery, messaging and observability half of
// the fully-populated fake account.

const (
	fixtureRestAPIID  = "abc123"
	fixtureHTTPAPIID  = "def456"
	fixtureWSAPIID    = "ghi789"
	fixtureDomainName = "api.example.com"
	fixtureZoneName   = "example.com."
	fixtureRuleARN    = "arn:aws:events:us-east-1:123456789012:rule/nightly"
)

func fixtureACM() *fakeAcm {
	return &fakeAcm{
		listCertificates: func(*acm.ListCertificatesInput) (*acm.ListCertificatesOutput, error) {
			return &acm.ListCertificatesOutput{CertificateSummaryList: []acmtypes.CertificateSummary{{
				CertificateArn: new(fixtureCertARN), DomainName: new(fixtureDomainName),
				Status: acmtypes.CertificateStatusIssued, Type: acmtypes.CertificateTypeAmazonIssued,
			}}}, nil
		},
		describeCertificate: func(*acm.DescribeCertificateInput) (*acm.DescribeCertificateOutput, error) {
			return &acm.DescribeCertificateOutput{Certificate: &acmtypes.CertificateDetail{
				CertificateArn: new(fixtureCertARN),
				DomainValidationOptions: []acmtypes.DomainValidation{
					{DomainName: new(fixtureDomainName), ValidationMethod: acmtypes.ValidationMethodDns},
					// AN EMAIL-VALIDATED DOMAIN, which no hosted zone proves and
					// which must therefore produce no VALIDATED_BY edge.
					{DomainName: new("mail.example.com"), ValidationMethod: acmtypes.ValidationMethodEmail},
				},
			}}, nil
		},
	}
}

func fixtureRoute53() *fakeRoute53 {
	return &fakeRoute53{
		listHostedZones: func(*route53.ListHostedZonesInput) (*route53.ListHostedZonesOutput, error) {
			return &route53.ListHostedZonesOutput{HostedZones: []route53types.HostedZone{{
				Id: new("/hostedzone/Z1EXAMPLE"), Name: new(fixtureZoneName),
				Config:                 &route53types.HostedZoneConfig{PrivateZone: false},
				ResourceRecordSetCount: new(int64(12)),
			}}}, nil
		},
	}
}

func fixtureCloudFront() *fakeCloudFront {
	return &fakeCloudFront{
		listDistributions: func(*cloudfront.ListDistributionsInput) (*cloudfront.ListDistributionsOutput, error) {
			return &cloudfront.ListDistributionsOutput{DistributionList: &cloudfronttypes.DistributionList{
				IsTruncated: new(false),
				Items: []cloudfronttypes.DistributionSummary{{
					ARN: new("arn:aws:cloudfront::123456789012:distribution/E1EXAMPLE"),
					Id:  new("E1EXAMPLE"), DomainName: new("d111.cloudfront.net"),
					Status: new("Deployed"), Enabled: new(true), Comment: new("public site"),
					ViewerCertificate: &cloudfronttypes.ViewerCertificate{ACMCertificateArn: new(fixtureCertARN)},
					Origins: &cloudfronttypes.Origins{Items: []cloudfronttypes.Origin{
						{Id: new("s3-site"), DomainName: new("site-assets.s3.us-east-1.amazonaws.com")},
						// A CUSTOM ORIGIN, which names no bucket and must yield no
						// edge.
						{Id: new("api"), DomainName: new("api.example.com")},
					}},
				}},
			}}, nil
		},
	}
}

func fixtureAPIGateway() *fakeApiGateway {
	return &fakeApiGateway{
		getRestApis: func(*apigateway.GetRestApisInput) (*apigateway.GetRestApisOutput, error) {
			return &apigateway.GetRestApisOutput{Items: []apigatewaytypes.RestApi{{
				Id: new(fixtureRestAPIID), Name: new("orders"), Description: new("orders REST API"),
			}}}, nil
		},
		getDomainNames: func(*apigateway.GetDomainNamesInput) (*apigateway.GetDomainNamesOutput, error) {
			return &apigateway.GetDomainNamesOutput{Items: []apigatewaytypes.DomainName{{
				DomainName: new(fixtureDomainName), CertificateArn: new(fixtureCertARN),
				RegionalDomainName: new("d-abc.execute-api.us-east-1.amazonaws.com"),
			}}}, nil
		},
		getBasePathMappings: func(*apigateway.GetBasePathMappingsInput) (*apigateway.GetBasePathMappingsOutput, error) {
			return &apigateway.GetBasePathMappingsOutput{Items: []apigatewaytypes.BasePathMapping{{
				RestApiId: new(fixtureRestAPIID), BasePath: new("(none)"), Stage: new("prod"),
			}}}, nil
		},
	}
}

func fixtureAPIGatewayV2() *fakeApiGatewayV2 {
	return &fakeApiGatewayV2{
		getApis: func(*apigatewayv2.GetApisInput) (*apigatewayv2.GetApisOutput, error) {
			return &apigatewayv2.GetApisOutput{Items: []apigatewayv2types.Api{
				{
					ApiId: new(fixtureHTTPAPIID), Name: new("edge"),
					ProtocolType: apigatewayv2types.ProtocolTypeHttp,
					ApiEndpoint:  new("https://def456.execute-api.us-east-1.amazonaws.com"),
				},
				{
					ApiId: new(fixtureWSAPIID), Name: new("stream"),
					ProtocolType: apigatewayv2types.ProtocolTypeWebsocket,
				},
			}}, nil
		},
	}
}

func fixtureS3() *fakeS3 {
	return &fakeS3{
		listBuckets: func(*s3.ListBucketsInput) (*s3.ListBucketsOutput, error) {
			return &s3.ListBucketsOutput{Buckets: []s3types.Bucket{
				{Name: new("site-assets")},
				{Name: new("flow-logs")},
			}}, nil
		},
	}
}

func fixtureSecretsManager() *fakeSecretsManager {
	return &fakeSecretsManager{
		listSecrets: func(*secretsmanager.ListSecretsInput) (*secretsmanager.ListSecretsOutput, error) {
			return &secretsmanager.ListSecretsOutput{SecretList: []secretsmanagertypes.SecretListEntry{{
				ARN:  new("arn:aws:secretsmanager:us-east-1:123456789012:secret:db-password-AbCdEf"),
				Name: new("db-password"), Description: new("orders database password"),
				KmsKeyId: new(fixtureKMSKeyARN),
			}}}, nil
		},
	}
}

func fixtureSQS() *fakeSqs {
	return &fakeSqs{
		listQueues: func(*sqs.ListQueuesInput) (*sqs.ListQueuesOutput, error) {
			return &sqs.ListQueuesOutput{QueueUrls: []string{
				"https://sqs.us-east-1.amazonaws.com/123456789012/jobs",
			}}, nil
		},
	}
}

func fixtureSNS() *fakeSns {
	return &fakeSns{
		listTopics: func(*sns.ListTopicsInput) (*sns.ListTopicsOutput, error) {
			return &sns.ListTopicsOutput{Topics: []snstypes.Topic{{TopicArn: new(fixtureTopicARN)}}}, nil
		},
	}
}

func fixtureEventBridge() *fakeEventBridge {
	return &fakeEventBridge{
		listRules: func(*eventbridge.ListRulesInput) (*eventbridge.ListRulesOutput, error) {
			return &eventbridge.ListRulesOutput{Rules: []eventbridgetypes.Rule{{
				Arn: new(fixtureRuleARN), Name: new("nightly"),
				State: eventbridgetypes.RuleStateEnabled, ScheduleExpression: new("cron(0 3 * * ? *)"),
			}}}, nil
		},
		listTargetsByRule: func(*eventbridge.ListTargetsByRuleInput) (*eventbridge.ListTargetsByRuleOutput, error) {
			return &eventbridge.ListTargetsByRuleOutput{Targets: []eventbridgetypes.Target{{
				Id: new("t1"), Arn: new(fixtureLambdaARN),
				DeadLetterConfig: &eventbridgetypes.DeadLetterConfig{Arn: new(fixtureQueueARN)},
			}}}, nil
		},
	}
}

func fixtureKinesis() *fakeKinesis {
	return &fakeKinesis{
		listStreams: func(*kinesis.ListStreamsInput) (*kinesis.ListStreamsOutput, error) {
			return &kinesis.ListStreamsOutput{StreamSummaries: []kinesistypes.StreamSummary{{
				StreamARN:  new("arn:aws:kinesis:us-east-1:123456789012:stream/events"),
				StreamName: new("events"), StreamStatus: kinesistypes.StreamStatusActive,
			}}}, nil
		},
	}
}

func fixtureSFN() *fakeSfn {
	return &fakeSfn{
		listStateMachines: func(*sfn.ListStateMachinesInput) (*sfn.ListStateMachinesOutput, error) {
			return &sfn.ListStateMachinesOutput{StateMachines: []sfntypes.StateMachineListItem{{
				StateMachineArn: new("arn:aws:states:us-east-1:123456789012:stateMachine:fulfill"),
				Name:            new("fulfill"), Type: sfntypes.StateMachineTypeStandard,
			}}}, nil
		},
	}
}

func fixtureCloudWatch() *fakeCloudWatch {
	return &fakeCloudWatch{
		describeAlarms: func(*cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error) {
			return &cloudwatch.DescribeAlarmsOutput{MetricAlarms: []cloudwatchtypes.MetricAlarm{{
				AlarmArn:  new("arn:aws:cloudwatch:us-east-1:123456789012:alarm:cpu-high"),
				AlarmName: new("cpu-high"), MetricName: new("CPUUtilization"),
				Namespace:          new("AWS/EC2"),
				ComparisonOperator: cloudwatchtypes.ComparisonOperatorGreaterThanThreshold,
				AlarmActions:       []string{fixtureTopicARN},
				Dimensions: []cloudwatchtypes.Dimension{
					{Name: new("InstanceId"), Value: new(fixtureInst)},
					// A DIMENSION THIS COLLECTOR CANNOT MAP, which must yield no
					// edge: a composed id for an unknown dimension kind would
					// resolve against nothing.
					{Name: new("ClusterName"), Value: new("apps")},
				},
			}}}, nil
		},
	}
}

func fixtureCloudWatchLogs() *fakeCloudWatchLogs {
	return &fakeCloudWatchLogs{
		describeLogGroups: func(*cloudwatchlogs.DescribeLogGroupsInput) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
			return &cloudwatchlogs.DescribeLogGroupsOutput{LogGroups: []cloudwatchlogstypes.LogGroup{{
				LogGroupName:    new("/aws/lambda/worker"),
				Arn:             new("arn:aws:logs:us-east-1:123456789012:log-group:/aws/lambda/worker:*"),
				RetentionInDays: new(int32(30)), KmsKeyId: new(fixtureKMSKeyARN),
			}}}, nil
		},
	}
}

func fixtureCloudTrail() *fakeCloudTrail {
	return &fakeCloudTrail{
		describeTrails: func(*cloudtrail.DescribeTrailsInput) (*cloudtrail.DescribeTrailsOutput, error) {
			return &cloudtrail.DescribeTrailsOutput{TrailList: []cloudtrailtypes.Trail{{
				TrailARN: new("arn:aws:cloudtrail:us-east-1:123456789012:trail/audit"),
				Name:     new("audit"), HomeRegion: new(fixtureRegion), IsMultiRegionTrail: new(true),
				S3BucketName: new("flow-logs"), KmsKeyId: new(fixtureKMSKeyARN),
			}}}, nil
		},
	}
}

func fixtureSES() *fakeSes {
	return &fakeSes{
		listEmailIdentities: func(*sesv2.ListEmailIdentitiesInput) (*sesv2.ListEmailIdentitiesOutput, error) {
			return &sesv2.ListEmailIdentitiesOutput{EmailIdentities: []sesv2types.IdentityInfo{{
				IdentityName: new("example.com"), IdentityType: sesv2types.IdentityTypeDomain,
				SendingEnabled: true,
			}}}, nil
		},
	}
}
