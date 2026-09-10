// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// fakes_ec2_test.go — the EC2 fake.
//
// EVERY METHOD DEFAULTS TO AN EMPTY SUCCESSFUL RESPONSE, and that is what makes
// the whole suite affordable: a test sets only the operations it is about, and
// every other service walk in the same run returns nothing without the test
// saying so. It also gives one of the five response classes — "the service
// returned nothing" — for free on every operation, which is the class a real
// account hits first in a fresh region.
//
// THE FAKE IS A STRUCT OF FUNCTIONS rather than a table of canned responses,
// because three of the five classes need to see the REQUEST: pagination has to
// answer differently on the second call, the error class has to fail on a chosen
// call, and the per-resource reads have to answer for the id they were asked
// about.

type fakeEC2 struct {
	describeVpcs            func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error)
	describeSubnets         func(*ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error)
	describeSecurityGroups  func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error)
	describeNetworkAcls     func(*ec2.DescribeNetworkAclsInput) (*ec2.DescribeNetworkAclsOutput, error)
	describePeering         func(*ec2.DescribeVpcPeeringConnectionsInput) (*ec2.DescribeVpcPeeringConnectionsOutput, error)
	describeTransitGateways func(*ec2.DescribeTransitGatewaysInput) (*ec2.DescribeTransitGatewaysOutput, error)
	describeTGWAttachments  func(*ec2.DescribeTransitGatewayAttachmentsInput) (*ec2.DescribeTransitGatewayAttachmentsOutput, error)
	describeVpcEndpoints    func(*ec2.DescribeVpcEndpointsInput) (*ec2.DescribeVpcEndpointsOutput, error)
	describeIGWs            func(*ec2.DescribeInternetGatewaysInput) (*ec2.DescribeInternetGatewaysOutput, error)
	describeNatGateways     func(*ec2.DescribeNatGatewaysInput) (*ec2.DescribeNatGatewaysOutput, error)
	describeFlowLogs        func(*ec2.DescribeFlowLogsInput) (*ec2.DescribeFlowLogsOutput, error)
	describeInstances       func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error)
	describeVolumes         func(*ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error)
}

func (f *fakeEC2) DescribeVpcs(_ context.Context, in *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if f.describeVpcs == nil {
		return &ec2.DescribeVpcsOutput{}, nil
	}
	return f.describeVpcs(in)
}

func (f *fakeEC2) DescribeSubnets(_ context.Context, in *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if f.describeSubnets == nil {
		return &ec2.DescribeSubnetsOutput{}, nil
	}
	return f.describeSubnets(in)
}

func (f *fakeEC2) DescribeSecurityGroups(_ context.Context, in *ec2.DescribeSecurityGroupsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error) {
	if f.describeSecurityGroups == nil {
		return &ec2.DescribeSecurityGroupsOutput{}, nil
	}
	return f.describeSecurityGroups(in)
}

func (f *fakeEC2) DescribeNetworkAcls(_ context.Context, in *ec2.DescribeNetworkAclsInput, _ ...func(*ec2.Options)) (*ec2.DescribeNetworkAclsOutput, error) {
	if f.describeNetworkAcls == nil {
		return &ec2.DescribeNetworkAclsOutput{}, nil
	}
	return f.describeNetworkAcls(in)
}

func (f *fakeEC2) DescribeVpcPeeringConnections(_ context.Context, in *ec2.DescribeVpcPeeringConnectionsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcPeeringConnectionsOutput, error) {
	if f.describePeering == nil {
		return &ec2.DescribeVpcPeeringConnectionsOutput{}, nil
	}
	return f.describePeering(in)
}

func (f *fakeEC2) DescribeTransitGateways(_ context.Context, in *ec2.DescribeTransitGatewaysInput, _ ...func(*ec2.Options)) (*ec2.DescribeTransitGatewaysOutput, error) {
	if f.describeTransitGateways == nil {
		return &ec2.DescribeTransitGatewaysOutput{}, nil
	}
	return f.describeTransitGateways(in)
}

func (f *fakeEC2) DescribeTransitGatewayAttachments(_ context.Context, in *ec2.DescribeTransitGatewayAttachmentsInput, _ ...func(*ec2.Options)) (*ec2.DescribeTransitGatewayAttachmentsOutput, error) {
	if f.describeTGWAttachments == nil {
		return &ec2.DescribeTransitGatewayAttachmentsOutput{}, nil
	}
	return f.describeTGWAttachments(in)
}

func (f *fakeEC2) DescribeVpcEndpoints(_ context.Context, in *ec2.DescribeVpcEndpointsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error) {
	if f.describeVpcEndpoints == nil {
		return &ec2.DescribeVpcEndpointsOutput{}, nil
	}
	return f.describeVpcEndpoints(in)
}

func (f *fakeEC2) DescribeInternetGateways(_ context.Context, in *ec2.DescribeInternetGatewaysInput, _ ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error) {
	if f.describeIGWs == nil {
		return &ec2.DescribeInternetGatewaysOutput{}, nil
	}
	return f.describeIGWs(in)
}

func (f *fakeEC2) DescribeNatGateways(_ context.Context, in *ec2.DescribeNatGatewaysInput, _ ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error) {
	if f.describeNatGateways == nil {
		return &ec2.DescribeNatGatewaysOutput{}, nil
	}
	return f.describeNatGateways(in)
}

func (f *fakeEC2) DescribeFlowLogs(_ context.Context, in *ec2.DescribeFlowLogsInput, _ ...func(*ec2.Options)) (*ec2.DescribeFlowLogsOutput, error) {
	if f.describeFlowLogs == nil {
		return &ec2.DescribeFlowLogsOutput{}, nil
	}
	return f.describeFlowLogs(in)
}

func (f *fakeEC2) DescribeInstances(_ context.Context, in *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	if f.describeInstances == nil {
		return &ec2.DescribeInstancesOutput{}, nil
	}
	return f.describeInstances(in)
}

func (f *fakeEC2) DescribeVolumes(_ context.Context, in *ec2.DescribeVolumesInput, _ ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error) {
	if f.describeVolumes == nil {
		return &ec2.DescribeVolumesOutput{}, nil
	}
	return f.describeVolumes(in)
}
