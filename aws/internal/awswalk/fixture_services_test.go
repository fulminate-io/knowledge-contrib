// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

// fixture_services_test.go — the compute, data and load-balancing half of the
// fully-populated fake account.

const (
	fixtureKMSKeyARN      = "arn:aws:kms:us-east-1:123456789012:key/1111-2222"
	fixtureLambdaARN      = "arn:aws:lambda:us-east-1:123456789012:function:worker"
	fixtureQueueARN       = "arn:aws:sqs:us-east-1:123456789012:jobs"
	fixtureTopicARN       = "arn:aws:sns:us-east-1:123456789012:alerts"
	fixtureTableARN       = "arn:aws:dynamodb:us-east-1:123456789012:table/orders"
	fixtureECSClusterARN  = "arn:aws:ecs:us-east-1:123456789012:cluster/apps"
	fixtureECSServiceARN  = "arn:aws:ecs:us-east-1:123456789012:service/apps/api"
	fixtureECRRepoARN     = "arn:aws:ecr:us-east-1:123456789012:repository/api"
	fixtureTargetGrpARN   = "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api/1111"
	fixtureIPTargetGrpARN = "arn:aws:elasticloadbalancing:us-east-1:123456789012:targetgroup/api-ip/5555"
	fixtureLBARN          = "arn:aws:elasticloadbalancing:us-east-1:123456789012:loadbalancer/app/api/2222"
	fixtureCertARN        = "arn:aws:acm:us-east-1:123456789012:certificate/3333"
	fixtureCacheGroupARN  = "arn:aws:elasticache:us-east-1:123456789012:replicationgroup:sessions"
)

func fixtureLambda() *fakeLambda {
	return &fakeLambda{
		listFunctions: func(*lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
			return &lambda.ListFunctionsOutput{Functions: []lambdatypes.FunctionConfiguration{{
				FunctionArn:  new(fixtureLambdaARN),
				FunctionName: new("worker"),
				Runtime:      lambdatypes.RuntimeProvidedal2023,
				Handler:      new("bootstrap"),
				MemorySize:   new(int32(512)),
				Role:         new(fixtureRoleARN),
				KMSKeyArn:    new(fixtureKMSKeyARN),
				VpcConfig: &lambdatypes.VpcConfigResponse{
					VpcId:            new(fixtureVPC),
					SubnetIds:        []string{fixtureSubnet},
					SecurityGroupIds: []string{fixtureSG},
				},
				DeadLetterConfig: &lambdatypes.DeadLetterConfig{TargetArn: new(fixtureQueueARN)},
			}}}, nil
		},
	}
}

func fixtureECS() *fakeEcs {
	return &fakeEcs{
		listClusters: func(*ecs.ListClustersInput) (*ecs.ListClustersOutput, error) {
			return &ecs.ListClustersOutput{ClusterArns: []string{fixtureECSClusterARN}}, nil
		},
		describeClusters: func(*ecs.DescribeClustersInput) (*ecs.DescribeClustersOutput, error) {
			return &ecs.DescribeClustersOutput{Clusters: []ecstypes.Cluster{{
				ClusterArn: new(fixtureECSClusterARN), ClusterName: new("apps"),
				Status: new("ACTIVE"), RunningTasksCount: 3,
			}}}, nil
		},
		listServices: func(*ecs.ListServicesInput) (*ecs.ListServicesOutput, error) {
			return &ecs.ListServicesOutput{ServiceArns: []string{fixtureECSServiceARN}}, nil
		},
		describeServices: func(*ecs.DescribeServicesInput) (*ecs.DescribeServicesOutput, error) {
			return &ecs.DescribeServicesOutput{Services: []ecstypes.Service{{
				ServiceArn: new(fixtureECSServiceARN), ServiceName: new("api"),
				ClusterArn: new(fixtureECSClusterARN), Status: new("ACTIVE"),
				LaunchType: ecstypes.LaunchTypeFargate, DesiredCount: 2,
				TaskDefinition: new("arn:aws:ecs:us-east-1:123456789012:task-definition/api:7"),
				RoleArn:        new(fixtureRoleARN),
				NetworkConfiguration: &ecstypes.NetworkConfiguration{
					AwsvpcConfiguration: &ecstypes.AwsVpcConfiguration{
						Subnets: []string{fixtureSubnet}, SecurityGroups: []string{fixtureSG},
					},
				},
				LoadBalancers: []ecstypes.LoadBalancer{{
					TargetGroupArn: new(fixtureTargetGrpARN), ContainerName: new("api"),
				}},
			}}}, nil
		},
		describeTaskDefinition: func(*ecs.DescribeTaskDefinitionInput) (*ecs.DescribeTaskDefinitionOutput, error) {
			return &ecs.DescribeTaskDefinitionOutput{TaskDefinition: &ecstypes.TaskDefinition{
				TaskRoleArn: new(fixtureRoleARN),
				ContainerDefinitions: []ecstypes.ContainerDefinition{
					{Name: new("api"), Image: new("123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v3")},
					// A PUBLIC IMAGE, which must yield NO edge: it names no
					// repository in this account, and an edge composed for it would
					// assert one that does not exist.
					{Name: new("sidecar"), Image: new("docker.io/library/nginx:1.27")},
				},
			}}, nil
		},
	}
}

func fixtureECR() *fakeEcr {
	return &fakeEcr{
		describeRepositories: func(*ecr.DescribeRepositoriesInput) (*ecr.DescribeRepositoriesOutput, error) {
			return &ecr.DescribeRepositoriesOutput{Repositories: []ecrtypes.Repository{{
				RepositoryArn: new(fixtureECRRepoARN), RepositoryName: new("api"),
				RepositoryUri:              new("123456789012.dkr.ecr.us-east-1.amazonaws.com/api"),
				ImageScanningConfiguration: &ecrtypes.ImageScanningConfiguration{ScanOnPush: true},
				EncryptionConfiguration:    &ecrtypes.EncryptionConfiguration{KmsKey: new(fixtureKMSKeyARN)},
			}}}, nil
		},
	}
}

func fixtureELBv2() *fakeElbv2 {
	return &fakeElbv2{
		describeLoadBalancers: func(*elbv2.DescribeLoadBalancersInput) (*elbv2.DescribeLoadBalancersOutput, error) {
			return &elbv2.DescribeLoadBalancersOutput{LoadBalancers: []elbv2types.LoadBalancer{{
				LoadBalancerArn: new(fixtureLBARN), LoadBalancerName: new("api"),
				Type: elbv2types.LoadBalancerTypeEnumApplication, Scheme: elbv2types.LoadBalancerSchemeEnumInternetFacing,
				DNSName: new("api-1.us-east-1.elb.amazonaws.com"), VpcId: new(fixtureVPC),
				SecurityGroups:    []string{fixtureSG},
				AvailabilityZones: []elbv2types.AvailabilityZone{{SubnetId: new(fixtureSubnet)}},
			}}}, nil
		},
		describeListeners: func(*elbv2.DescribeListenersInput) (*elbv2.DescribeListenersOutput, error) {
			return &elbv2.DescribeListenersOutput{Listeners: []elbv2types.Listener{{
				ListenerArn:  new(fixtureLBARN + "/listener/4444"),
				Certificates: []elbv2types.Certificate{{CertificateArn: new(fixtureCertARN)}},
			}}}, nil
		},
		describeTargetGroups: func(*elbv2.DescribeTargetGroupsInput) (*elbv2.DescribeTargetGroupsOutput, error) {
			// TWO GROUPS OF DIFFERENT TARGET TYPES, because the target id's MEANING
			// depends on the group's type: an instance group's id is an instance id
			// that has to be composed into an ARN, and an ip group's is an address
			// that names no AWS resource at all. One group could not exercise both.
			return &elbv2.DescribeTargetGroupsOutput{TargetGroups: []elbv2types.TargetGroup{
				{
					TargetGroupArn: new(fixtureTargetGrpARN), TargetGroupName: new("api"),
					Protocol: elbv2types.ProtocolEnumHttps, Port: new(int32(443)),
					VpcId: new(fixtureVPC), TargetType: elbv2types.TargetTypeEnumInstance,
					LoadBalancerArns: []string{fixtureLBARN},
				},
				{
					TargetGroupArn: new(fixtureIPTargetGrpARN), TargetGroupName: new("api-ip"),
					Protocol: elbv2types.ProtocolEnumHttps, Port: new(int32(443)),
					VpcId: new(fixtureVPC), TargetType: elbv2types.TargetTypeEnumIp,
					LoadBalancerArns: []string{fixtureLBARN},
				},
			}}, nil
		},
		describeTargetHealth: func(in *elbv2.DescribeTargetHealthInput) (*elbv2.DescribeTargetHealthOutput, error) {
			if deref(in.TargetGroupArn) == fixtureIPTargetGrpARN {
				// AN IP TARGET, which names no AWS resource and must yield no edge:
				// an address is not something the graph holds.
				return &elbv2.DescribeTargetHealthOutput{
					TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
						{Target: &elbv2types.TargetDescription{Id: new("10.0.1.55")}},
					},
				}, nil
			}
			return &elbv2.DescribeTargetHealthOutput{
				TargetHealthDescriptions: []elbv2types.TargetHealthDescription{
					{Target: &elbv2types.TargetDescription{Id: new(fixtureInst)}},
				},
			}, nil
		},
	}
}
