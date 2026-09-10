// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/apigateway"
	"github.com/aws/aws-sdk-go-v2/service/apigatewayv2"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
)

// fakes_services_a_test.go — the fake for the first half of the service clients,
// alphabetically. EC2's own fake lives beside it in fakes_ec2_test.go.
//
// EVERY METHOD DEFAULTS TO AN EMPTY SUCCESSFUL RESPONSE, on the same terms the
// EC2 fake states: a test sets only the operations it is about, and every other
// walk in the run returns nothing without the test saying so. That default is
// also one of the five response classes — "the service returned nothing" — on
// every operation, which is the class a fresh region hits first.
//
// THE SHAPE IS UNIFORM ON PURPOSE. One struct per service interface, one function
// field per operation, named for the operation in lower camel case. A reader
// checking whether a service is faked at all reads one struct; a test author
// setting one operation needs no other knowledge.
//
// THE SPLIT ACROSS TWO FILES IS FOR LENGTH and nothing else: the two halves are
// alphabetical and carry no other distinction.

type fakeAcm struct {
	listCertificates    func(*acm.ListCertificatesInput) (*acm.ListCertificatesOutput, error)
	describeCertificate func(*acm.DescribeCertificateInput) (*acm.DescribeCertificateOutput, error)
}

func (f *fakeAcm) ListCertificates(_ context.Context, in *acm.ListCertificatesInput, _ ...func(*acm.Options)) (*acm.ListCertificatesOutput, error) {
	if f.listCertificates == nil {
		return &acm.ListCertificatesOutput{}, nil
	}
	return f.listCertificates(in)
}

func (f *fakeAcm) DescribeCertificate(_ context.Context, in *acm.DescribeCertificateInput, _ ...func(*acm.Options)) (*acm.DescribeCertificateOutput, error) {
	if f.describeCertificate == nil {
		return &acm.DescribeCertificateOutput{}, nil
	}
	return f.describeCertificate(in)
}

type fakeApiGateway struct {
	getRestApis         func(*apigateway.GetRestApisInput) (*apigateway.GetRestApisOutput, error)
	getDomainNames      func(*apigateway.GetDomainNamesInput) (*apigateway.GetDomainNamesOutput, error)
	getBasePathMappings func(*apigateway.GetBasePathMappingsInput) (*apigateway.GetBasePathMappingsOutput, error)
}

func (f *fakeApiGateway) GetRestApis(_ context.Context, in *apigateway.GetRestApisInput, _ ...func(*apigateway.Options)) (*apigateway.GetRestApisOutput, error) {
	if f.getRestApis == nil {
		return &apigateway.GetRestApisOutput{}, nil
	}
	return f.getRestApis(in)
}

func (f *fakeApiGateway) GetDomainNames(_ context.Context, in *apigateway.GetDomainNamesInput, _ ...func(*apigateway.Options)) (*apigateway.GetDomainNamesOutput, error) {
	if f.getDomainNames == nil {
		return &apigateway.GetDomainNamesOutput{}, nil
	}
	return f.getDomainNames(in)
}

func (f *fakeApiGateway) GetBasePathMappings(_ context.Context, in *apigateway.GetBasePathMappingsInput, _ ...func(*apigateway.Options)) (*apigateway.GetBasePathMappingsOutput, error) {
	if f.getBasePathMappings == nil {
		return &apigateway.GetBasePathMappingsOutput{}, nil
	}
	return f.getBasePathMappings(in)
}

type fakeApiGatewayV2 struct {
	getApis func(*apigatewayv2.GetApisInput) (*apigatewayv2.GetApisOutput, error)
}

func (f *fakeApiGatewayV2) GetApis(_ context.Context, in *apigatewayv2.GetApisInput, _ ...func(*apigatewayv2.Options)) (*apigatewayv2.GetApisOutput, error) {
	if f.getApis == nil {
		return &apigatewayv2.GetApisOutput{}, nil
	}
	return f.getApis(in)
}

type fakeCloudFront struct {
	listDistributions func(*cloudfront.ListDistributionsInput) (*cloudfront.ListDistributionsOutput, error)
}

func (f *fakeCloudFront) ListDistributions(_ context.Context, in *cloudfront.ListDistributionsInput, _ ...func(*cloudfront.Options)) (*cloudfront.ListDistributionsOutput, error) {
	if f.listDistributions == nil {
		return &cloudfront.ListDistributionsOutput{}, nil
	}
	return f.listDistributions(in)
}

type fakeCloudTrail struct {
	describeTrails func(*cloudtrail.DescribeTrailsInput) (*cloudtrail.DescribeTrailsOutput, error)
}

func (f *fakeCloudTrail) DescribeTrails(_ context.Context, in *cloudtrail.DescribeTrailsInput, _ ...func(*cloudtrail.Options)) (*cloudtrail.DescribeTrailsOutput, error) {
	if f.describeTrails == nil {
		return &cloudtrail.DescribeTrailsOutput{}, nil
	}
	return f.describeTrails(in)
}

type fakeCloudWatch struct {
	describeAlarms func(*cloudwatch.DescribeAlarmsInput) (*cloudwatch.DescribeAlarmsOutput, error)
}

func (f *fakeCloudWatch) DescribeAlarms(_ context.Context, in *cloudwatch.DescribeAlarmsInput, _ ...func(*cloudwatch.Options)) (*cloudwatch.DescribeAlarmsOutput, error) {
	if f.describeAlarms == nil {
		return &cloudwatch.DescribeAlarmsOutput{}, nil
	}
	return f.describeAlarms(in)
}

type fakeCloudWatchLogs struct {
	describeLogGroups func(*cloudwatchlogs.DescribeLogGroupsInput) (*cloudwatchlogs.DescribeLogGroupsOutput, error)
}

func (f *fakeCloudWatchLogs) DescribeLogGroups(_ context.Context, in *cloudwatchlogs.DescribeLogGroupsInput, _ ...func(*cloudwatchlogs.Options)) (*cloudwatchlogs.DescribeLogGroupsOutput, error) {
	if f.describeLogGroups == nil {
		return &cloudwatchlogs.DescribeLogGroupsOutput{}, nil
	}
	return f.describeLogGroups(in)
}

type fakeDynamoDB struct {
	listTables                func(*dynamodb.ListTablesInput) (*dynamodb.ListTablesOutput, error)
	describeTable             func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error)
	describeContinuousBackups func(*dynamodb.DescribeContinuousBackupsInput) (*dynamodb.DescribeContinuousBackupsOutput, error)
	listBackups               func(*dynamodb.ListBackupsInput) (*dynamodb.ListBackupsOutput, error)
}

func (f *fakeDynamoDB) ListTables(_ context.Context, in *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	if f.listTables == nil {
		return &dynamodb.ListTablesOutput{}, nil
	}
	return f.listTables(in)
}

func (f *fakeDynamoDB) DescribeTable(_ context.Context, in *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	if f.describeTable == nil {
		return &dynamodb.DescribeTableOutput{}, nil
	}
	return f.describeTable(in)
}

func (f *fakeDynamoDB) DescribeContinuousBackups(_ context.Context, in *dynamodb.DescribeContinuousBackupsInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeContinuousBackupsOutput, error) {
	if f.describeContinuousBackups == nil {
		return &dynamodb.DescribeContinuousBackupsOutput{}, nil
	}
	return f.describeContinuousBackups(in)
}

func (f *fakeDynamoDB) ListBackups(_ context.Context, in *dynamodb.ListBackupsInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListBackupsOutput, error) {
	if f.listBackups == nil {
		return &dynamodb.ListBackupsOutput{}, nil
	}
	return f.listBackups(in)
}

type fakeEcr struct {
	describeRepositories func(*ecr.DescribeRepositoriesInput) (*ecr.DescribeRepositoriesOutput, error)
}

func (f *fakeEcr) DescribeRepositories(_ context.Context, in *ecr.DescribeRepositoriesInput, _ ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error) {
	if f.describeRepositories == nil {
		return &ecr.DescribeRepositoriesOutput{}, nil
	}
	return f.describeRepositories(in)
}

type fakeEcs struct {
	listClusters           func(*ecs.ListClustersInput) (*ecs.ListClustersOutput, error)
	describeClusters       func(*ecs.DescribeClustersInput) (*ecs.DescribeClustersOutput, error)
	listServices           func(*ecs.ListServicesInput) (*ecs.ListServicesOutput, error)
	describeServices       func(*ecs.DescribeServicesInput) (*ecs.DescribeServicesOutput, error)
	describeTaskDefinition func(*ecs.DescribeTaskDefinitionInput) (*ecs.DescribeTaskDefinitionOutput, error)
}

func (f *fakeEcs) ListClusters(_ context.Context, in *ecs.ListClustersInput, _ ...func(*ecs.Options)) (*ecs.ListClustersOutput, error) {
	if f.listClusters == nil {
		return &ecs.ListClustersOutput{}, nil
	}
	return f.listClusters(in)
}

func (f *fakeEcs) DescribeClusters(_ context.Context, in *ecs.DescribeClustersInput, _ ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error) {
	if f.describeClusters == nil {
		return &ecs.DescribeClustersOutput{}, nil
	}
	return f.describeClusters(in)
}

func (f *fakeEcs) ListServices(_ context.Context, in *ecs.ListServicesInput, _ ...func(*ecs.Options)) (*ecs.ListServicesOutput, error) {
	if f.listServices == nil {
		return &ecs.ListServicesOutput{}, nil
	}
	return f.listServices(in)
}

func (f *fakeEcs) DescribeServices(_ context.Context, in *ecs.DescribeServicesInput, _ ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error) {
	if f.describeServices == nil {
		return &ecs.DescribeServicesOutput{}, nil
	}
	return f.describeServices(in)
}

func (f *fakeEcs) DescribeTaskDefinition(_ context.Context, in *ecs.DescribeTaskDefinitionInput, _ ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error) {
	if f.describeTaskDefinition == nil {
		return &ecs.DescribeTaskDefinitionOutput{}, nil
	}
	return f.describeTaskDefinition(in)
}

type fakeEfs struct {
	describeFileSystems func(*efs.DescribeFileSystemsInput) (*efs.DescribeFileSystemsOutput, error)
}

func (f *fakeEfs) DescribeFileSystems(_ context.Context, in *efs.DescribeFileSystemsInput, _ ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error) {
	if f.describeFileSystems == nil {
		return &efs.DescribeFileSystemsOutput{}, nil
	}
	return f.describeFileSystems(in)
}

type fakeEks struct {
	listClusters    func(*eks.ListClustersInput) (*eks.ListClustersOutput, error)
	describeCluster func(*eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error)
}

func (f *fakeEks) ListClusters(_ context.Context, in *eks.ListClustersInput, _ ...func(*eks.Options)) (*eks.ListClustersOutput, error) {
	if f.listClusters == nil {
		return &eks.ListClustersOutput{}, nil
	}
	return f.listClusters(in)
}

func (f *fakeEks) DescribeCluster(_ context.Context, in *eks.DescribeClusterInput, _ ...func(*eks.Options)) (*eks.DescribeClusterOutput, error) {
	if f.describeCluster == nil {
		return &eks.DescribeClusterOutput{}, nil
	}
	return f.describeCluster(in)
}

type fakeElastiCache struct {
	describeCacheClusters     func(*elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error)
	describeReplicationGroups func(*elasticache.DescribeReplicationGroupsInput) (*elasticache.DescribeReplicationGroupsOutput, error)
}

func (f *fakeElastiCache) DescribeCacheClusters(_ context.Context, in *elasticache.DescribeCacheClustersInput, _ ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error) {
	if f.describeCacheClusters == nil {
		return &elasticache.DescribeCacheClustersOutput{}, nil
	}
	return f.describeCacheClusters(in)
}

func (f *fakeElastiCache) DescribeReplicationGroups(_ context.Context, in *elasticache.DescribeReplicationGroupsInput, _ ...func(*elasticache.Options)) (*elasticache.DescribeReplicationGroupsOutput, error) {
	if f.describeReplicationGroups == nil {
		return &elasticache.DescribeReplicationGroupsOutput{}, nil
	}
	return f.describeReplicationGroups(in)
}
