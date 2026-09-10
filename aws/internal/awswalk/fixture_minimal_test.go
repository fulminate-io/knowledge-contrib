// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/aws/aws-sdk-go-v2/service/cloudtrail"
	cloudtrailtypes "github.com/aws/aws-sdk-go-v2/service/cloudtrail/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fixture_minimal_test.go — the MISSING-EVERY-OPTIONAL-FIELD fixture, and the
// per-service failure injectors the error matrix uses.
//
// EVERY RESOURCE HERE CARRIES ITS IDENTIFIER AND NOTHING ELSE. The AWS SDK models
// almost every field as a pointer, including ones the API populates in practice,
// so this is the shape that finds a walk dereferencing one directly — and a panic
// in a service walk's goroutine takes the whole collector process down rather than
// failing one walk.

func minimalClients() *Clients {
	c := emptyClients()
	c.EC2.(*fakeEC2).describeVpcs = func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
		return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{VpcId: new("vpc-min")}}}, nil
	}
	c.EC2.(*fakeEC2).describeSubnets = func(*ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
		return &ec2.DescribeSubnetsOutput{Subnets: []ec2types.Subnet{{SubnetId: new("subnet-min")}}}, nil
	}
	c.EC2.(*fakeEC2).describeSecurityGroups = func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error) {
		return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{GroupId: new("sg-min")}}}, nil
	}
	c.EC2.(*fakeEC2).describeNetworkAcls = func(*ec2.DescribeNetworkAclsInput) (*ec2.DescribeNetworkAclsOutput, error) {
		// AN ENTRY WITH NO PORT RANGE AND NO PROTOCOL, which is what a
		// protocol-agnostic ACL rule looks like.
		return &ec2.DescribeNetworkAclsOutput{NetworkAcls: []ec2types.NetworkAcl{{
			NetworkAclId: new("acl-min"),
			Entries:      []ec2types.NetworkAclEntry{{RuleAction: ec2types.RuleActionAllow, CidrBlock: new("0.0.0.0/0")}},
		}}}, nil
	}
	c.EC2.(*fakeEC2).describeInstances = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		// A RESERVATION WITH AN INSTANCE CARRYING NO STATE BLOCK and no profile.
		return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{
			Instances: []ec2types.Instance{{InstanceId: new("i-min")}},
		}}}, nil
	}
	c.EC2.(*fakeEC2).describeVolumes = func(*ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
		return &ec2.DescribeVolumesOutput{Volumes: []ec2types.Volume{{VolumeId: new("vol-min")}}}, nil
	}
	c.EC2.(*fakeEC2).describePeering = func(*ec2.DescribeVpcPeeringConnectionsInput) (*ec2.DescribeVpcPeeringConnectionsOutput, error) {
		// NO STATUS BLOCK AND NO VpcInfo BLOCKS: the shape every peering guard is
		// written for, reached through the node path rather than the edge path.
		return &ec2.DescribeVpcPeeringConnectionsOutput{
			VpcPeeringConnections: []ec2types.VpcPeeringConnection{{VpcPeeringConnectionId: new("pcx-min")}},
		}, nil
	}
	c.IAM.(*fakeIam).listRoles = func(*iam.ListRolesInput) (*iam.ListRolesOutput, error) {
		// NO TRUST POLICY AT ALL, which the derived passes must read as "no
		// statements" rather than dereference.
		return &iam.ListRolesOutput{Roles: []iamtypes.Role{{
			Arn: new("arn:aws:iam::123456789012:role/min"), RoleName: new("min"),
		}}}, nil
	}
	c.Lambda.(*fakeLambda).listFunctions = func(*lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
		// NO VpcConfig AND NO DeadLetterConfig: two nested pointers a walk that
		// read through them without a guard would panic on.
		return &lambda.ListFunctionsOutput{Functions: []lambdatypes.FunctionConfiguration{{
			FunctionArn: new("arn:aws:lambda:us-east-1:123456789012:function:min"),
		}}}, nil
	}
	c.S3.(*fakeS3).listBuckets = func(*s3.ListBucketsInput) (*s3.ListBucketsOutput, error) {
		return &s3.ListBucketsOutput{Buckets: []s3types.Bucket{{Name: new("min-bucket")}}}, nil
	}
	c.EKS.(*fakeEks).listClusters = func(*eks.ListClustersInput) (*eks.ListClustersOutput, error) {
		return &eks.ListClustersOutput{Clusters: []string{"min"}}, nil
	}
	c.EKS.(*fakeEks).describeCluster = func(*eks.DescribeClusterInput) (*eks.DescribeClusterOutput, error) {
		// A CLUSTER WITH NO Identity AND NO ResourcesVpcConfig, which is what a
		// cluster mid-creation looks like.
		return &eks.DescribeClusterOutput{Cluster: nil}, nil
	}
	c.DynamoDB.(*fakeDynamoDB).listTables = func(*dynamodb.ListTablesInput) (*dynamodb.ListTablesOutput, error) {
		return &dynamodb.ListTablesOutput{TableNames: []string{"min"}}, nil
	}
	c.DynamoDB.(*fakeDynamoDB).describeTable = func(*dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
		// A DESCRIBE THAT RETURNS NO TABLE, which is what a race against a delete
		// looks like.
		return &dynamodb.DescribeTableOutput{}, nil
	}
	c.CloudTrail.(*fakeCloudTrail).describeTrails = func(*cloudtrail.DescribeTrailsInput) (*cloudtrail.DescribeTrailsOutput, error) {
		return &cloudtrail.DescribeTrailsOutput{TrailList: []cloudtrailtypes.Trail{{
			TrailARN: new("arn:aws:cloudtrail:us-east-1:123456789012:trail/min"),
		}}}, nil
	}
	c.OpenSearch.(*fakeOpenSearch).listDomainNames = func(*opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
		return &opensearch.ListDomainNamesOutput{}, nil
	}
	return c
}

// --- the per-service failure injectors the error matrix uses -----------------
//
// EACH BREAKS EXACTLY ONE OPERATION and leaves the rest of the account intact, so
// the matrix's known positive — that unrelated services still land — means what
// it says.

func failEC2Vpcs(c *Clients, err error) {
	c.EC2.(*fakeEC2).describeVpcs = func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
		return nil, err
	}
}

func failEC2Instances(c *Clients, err error) {
	c.EC2.(*fakeEC2).describeInstances = func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
		return nil, err
	}
}

func failIAMRoles(c *Clients, err error) {
	c.IAM.(*fakeIam).listRoles = func(*iam.ListRolesInput) (*iam.ListRolesOutput, error) { return nil, err }
}

func failLambda(c *Clients, err error) {
	c.Lambda.(*fakeLambda).listFunctions = func(*lambda.ListFunctionsInput) (*lambda.ListFunctionsOutput, error) {
		return nil, err
	}
}

func failS3(c *Clients, err error) {
	c.S3.(*fakeS3).listBuckets = func(*s3.ListBucketsInput) (*s3.ListBucketsOutput, error) { return nil, err }
}

func failEKS(c *Clients, err error) {
	c.EKS.(*fakeEks).listClusters = func(*eks.ListClustersInput) (*eks.ListClustersOutput, error) {
		return nil, err
	}
}

func failDynamoTables(c *Clients, err error) {
	c.DynamoDB.(*fakeDynamoDB).listTables = func(*dynamodb.ListTablesInput) (*dynamodb.ListTablesOutput, error) {
		return nil, err
	}
}

func failCloudTrail(c *Clients, err error) {
	c.CloudTrail.(*fakeCloudTrail).describeTrails = func(*cloudtrail.DescribeTrailsInput) (*cloudtrail.DescribeTrailsOutput, error) {
		return nil, err
	}
}

func failOpenSearch(c *Clients, err error) {
	c.OpenSearch.(*fakeOpenSearch).listDomainNames = func(*opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
		return nil, err
	}
}
