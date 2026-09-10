// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// fixture_ec2_test.go — the EC2 half of the fully-populated fake account.
//
// IT CARRIES THE CROSS-ACCOUNT SHAPES the rest of the suite keys on: a peering
// whose accepter is in another account, and a security-group rule referencing a
// group in that account. Both produce edges whose far endpoint a one-account walk
// never materializes, which is the property the closed-enumeration endpoint
// assertion in parity_test.go is written around.

func fixtureEC2() *fakeEC2 {
	return &fakeEC2{
		describeVpcs: func(*ec2.DescribeVpcsInput) (*ec2.DescribeVpcsOutput, error) {
			return &ec2.DescribeVpcsOutput{Vpcs: []ec2types.Vpc{{
				VpcId:     new(fixtureVPC),
				CidrBlock: new("10.0.0.0/16"),
				State:     ec2types.VpcStateAvailable,
				Tags:      []ec2types.Tag{{Key: new("Name"), Value: new("prod")}},
			}}}, nil
		},
		describeSubnets: func(*ec2.DescribeSubnetsInput) (*ec2.DescribeSubnetsOutput, error) {
			return &ec2.DescribeSubnetsOutput{Subnets: []ec2types.Subnet{{
				SubnetId:         new(fixtureSubnet),
				VpcId:            new(fixtureVPC),
				CidrBlock:        new("10.0.1.0/24"),
				AvailabilityZone: new("us-east-1a"),
			}}}, nil
		},
		describeSecurityGroups: func(*ec2.DescribeSecurityGroupsInput) (*ec2.DescribeSecurityGroupsOutput, error) {
			return &ec2.DescribeSecurityGroupsOutput{SecurityGroups: []ec2types.SecurityGroup{{
				GroupId:   new(fixtureSG),
				GroupName: new("web"),
				VpcId:     new(fixtureVPC),
				IpPermissions: []ec2types.IpPermission{{
					IpProtocol: new("tcp"),
					FromPort:   new(int32(443)),
					ToPort:     new(int32(443)),
					IpRanges:   []ec2types.IpRange{{CidrIp: new("10.0.0.0/8")}},
					// A PEER GROUP IN ANOTHER ACCOUNT. The edge for it names an id
					// this walk never emits, which is the case the endpoint
					// enumeration has to admit deliberately.
					UserIdGroupPairs: []ec2types.UserIdGroupPair{{
						GroupId: new("sg-0peer"),
						UserId:  new(fixturePeerAccount),
					}},
				}},
				IpPermissionsEgress: []ec2types.IpPermission{{
					IpProtocol: new("-1"),
					IpRanges:   []ec2types.IpRange{{CidrIp: new("0.0.0.0/0")}},
				}},
			}}}, nil
		},
		describeNetworkAcls: func(*ec2.DescribeNetworkAclsInput) (*ec2.DescribeNetworkAclsOutput, error) {
			return &ec2.DescribeNetworkAclsOutput{NetworkAcls: []ec2types.NetworkAcl{{
				NetworkAclId: new(fixtureNACL),
				VpcId:        new(fixtureVPC),
				Associations: []ec2types.NetworkAclAssociation{{
					SubnetId:                new(fixtureSubnet),
					NetworkAclAssociationId: new("aclassoc-0aaa"),
				}},
				Entries: []ec2types.NetworkAclEntry{
					// THE SAME RANGE THE SECURITY-GROUP RULE REFERENCES, deliberately:
					// two independent walks naming one CIDR is the case the sink's
					// node dedupe exists for, and a fixture where every range is
					// distinct would never exercise it.
					{
						RuleNumber: new(int32(100)),
						RuleAction: ec2types.RuleActionAllow,
						Protocol:   new("6"),
						CidrBlock:  new("10.0.0.0/8"),
						Egress:     new(false),
					},
					{
						RuleNumber: new(int32(110)),
						RuleAction: ec2types.RuleActionAllow,
						Protocol:   new("-1"),
						CidrBlock:  new("0.0.0.0/0"),
						Egress:     new(true),
					},
					// A DENY ENTRY, which must produce NO edge: an ALLOWS edge for
					// a deny rule inverts the meaning of the graph's most
					// security-relevant relationship.
					{
						RuleNumber: new(int32(120)),
						RuleAction: ec2types.RuleActionDeny,
						Protocol:   new("-1"),
						CidrBlock:  new("192.0.2.0/24"),
						Egress:     new(false),
					},
				},
			}}}, nil
		},
		describePeering: func(*ec2.DescribeVpcPeeringConnectionsInput) (*ec2.DescribeVpcPeeringConnectionsOutput, error) {
			return &ec2.DescribeVpcPeeringConnectionsOutput{
				VpcPeeringConnections: []ec2types.VpcPeeringConnection{{
					VpcPeeringConnectionId: new("pcx-0aaa"),
					Status:                 &ec2types.VpcPeeringConnectionStateReason{Code: ec2types.VpcPeeringConnectionStateReasonCodeActive},
					RequesterVpcInfo: &ec2types.VpcPeeringConnectionVpcInfo{
						VpcId: new(fixtureVPC), OwnerId: new(fixtureAccount),
						Region: new(fixtureRegion), CidrBlock: new("10.0.0.0/16"),
					},
					AccepterVpcInfo: &ec2types.VpcPeeringConnectionVpcInfo{
						VpcId: new(fixturePeerVPC), OwnerId: new(fixturePeerAccount),
						Region: new("us-west-2"), CidrBlock: new("10.9.0.0/16"),
					},
				}},
			}, nil
		},
		describeTransitGateways: func(*ec2.DescribeTransitGatewaysInput) (*ec2.DescribeTransitGatewaysOutput, error) {
			return &ec2.DescribeTransitGatewaysOutput{TransitGateways: []ec2types.TransitGateway{{
				TransitGatewayId:  new("tgw-0aaa"),
				TransitGatewayArn: new("arn:aws:ec2:us-east-1:123456789012:transit-gateway/tgw-0aaa"),
				State:             ec2types.TransitGatewayStateAvailable,
			}}}, nil
		},
		describeTGWAttachments: func(*ec2.DescribeTransitGatewayAttachmentsInput) (*ec2.DescribeTransitGatewayAttachmentsOutput, error) {
			return &ec2.DescribeTransitGatewayAttachmentsOutput{
				TransitGatewayAttachments: []ec2types.TransitGatewayAttachment{{
					TransitGatewayAttachmentId: new("tgw-attach-0aaa"),
					TransitGatewayId:           new("tgw-0aaa"),
					ResourceType:               ec2types.TransitGatewayAttachmentResourceTypeVpc,
					ResourceId:                 new(fixtureVPC),
					State:                      ec2types.TransitGatewayAttachmentStateAvailable,
				}},
			}, nil
		},
		describeVpcEndpoints: func(*ec2.DescribeVpcEndpointsInput) (*ec2.DescribeVpcEndpointsOutput, error) {
			return &ec2.DescribeVpcEndpointsOutput{VpcEndpoints: []ec2types.VpcEndpoint{{
				VpcEndpointId:   new("vpce-0aaa"),
				VpcId:           new(fixtureVPC),
				ServiceName:     new("com.amazonaws.us-east-1.s3"),
				VpcEndpointType: ec2types.VpcEndpointTypeInterface,
				SubnetIds:       []string{fixtureSubnet},
				Groups:          []ec2types.SecurityGroupIdentifier{{GroupId: new(fixtureSG)}},
			}}}, nil
		},
		describeIGWs: func(*ec2.DescribeInternetGatewaysInput) (*ec2.DescribeInternetGatewaysOutput, error) {
			return &ec2.DescribeInternetGatewaysOutput{InternetGateways: []ec2types.InternetGateway{{
				InternetGatewayId: new("igw-0aaa"),
				Attachments: []ec2types.InternetGatewayAttachment{{
					VpcId: new(fixtureVPC), State: ec2types.AttachmentStatusAttached,
				}},
			}}}, nil
		},
		describeNatGateways: func(*ec2.DescribeNatGatewaysInput) (*ec2.DescribeNatGatewaysOutput, error) {
			return &ec2.DescribeNatGatewaysOutput{NatGateways: []ec2types.NatGateway{{
				NatGatewayId: new("nat-0aaa"),
				VpcId:        new(fixtureVPC),
				SubnetId:     new(fixtureSubnet),
				State:        ec2types.NatGatewayStateAvailable,
			}}}, nil
		},
		describeFlowLogs: func(*ec2.DescribeFlowLogsInput) (*ec2.DescribeFlowLogsOutput, error) {
			return &ec2.DescribeFlowLogsOutput{FlowLogs: []ec2types.FlowLog{{
				FlowLogId:          new("fl-0aaa"),
				ResourceId:         new(fixtureVPC),
				TrafficType:        ec2types.TrafficTypeAll,
				LogDestinationType: ec2types.LogDestinationTypeS3,
				LogDestination:     new("arn:aws:s3:::flow-logs"),
			}}}, nil
		},
		describeInstances: func(*ec2.DescribeInstancesInput) (*ec2.DescribeInstancesOutput, error) {
			return &ec2.DescribeInstancesOutput{Reservations: []ec2types.Reservation{{
				Instances: []ec2types.Instance{{
					InstanceId:       new(fixtureInst),
					InstanceType:     ec2types.InstanceTypeT3Micro,
					VpcId:            new(fixtureVPC),
					SubnetId:         new(fixtureSubnet),
					PrivateIpAddress: new("10.0.1.10"),
					State:            &ec2types.InstanceState{Name: ec2types.InstanceStateNameRunning},
					SecurityGroups:   []ec2types.GroupIdentifier{{GroupId: new(fixtureSG)}},
					IamInstanceProfile: &ec2types.IamInstanceProfile{
						Arn: new("arn:aws:iam::123456789012:instance-profile/app"),
					},
					Tags: []ec2types.Tag{{Key: new("Name"), Value: new("api-1")}},
				}},
			}}}, nil
		},
		describeVolumes: func(*ec2.DescribeVolumesInput) (*ec2.DescribeVolumesOutput, error) {
			return &ec2.DescribeVolumesOutput{Volumes: []ec2types.Volume{{
				VolumeId:    new(fixtureVolume),
				VolumeType:  ec2types.VolumeTypeGp3,
				Size:        new(int32(100)),
				Encrypted:   new(true),
				KmsKeyId:    new(fixtureKMSKeyARN),
				State:       ec2types.VolumeStateInUse,
				Attachments: []ec2types.VolumeAttachment{{InstanceId: new(fixtureInst), Device: new("/dev/sda1")}},
			}}}, nil
		},
	}
}
