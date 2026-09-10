// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// svc_ec2_network.go — the VPC surface: vpcs, subnets, security groups, network
// ACLs, gateways and endpoints.
//
// THE INTERFACE IS NARROW AND IT IS DECLARED HERE rather than beside the other
// services' — it lists exactly the EC2 operations this package calls, so a test
// supplies a fake without an AWS credential and a reader sees the whole API
// surface this collector touches in one place. It is the idiom the built-in
// collector uses for the same reason, stated at its own acm.go: defining the
// client as an interface lets tests mock the service without AWS credentials.

// ec2API is the subset of the EC2 client surface this collector calls.
type ec2API interface {
	DescribeVpcs(ctx context.Context, in *ec2.DescribeVpcsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeSubnets(ctx context.Context, in *ec2.DescribeSubnetsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeSecurityGroups(ctx context.Context, in *ec2.DescribeSecurityGroupsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSecurityGroupsOutput, error)
	DescribeNetworkAcls(ctx context.Context, in *ec2.DescribeNetworkAclsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNetworkAclsOutput, error)
	DescribeVpcPeeringConnections(ctx context.Context, in *ec2.DescribeVpcPeeringConnectionsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcPeeringConnectionsOutput, error)
	DescribeTransitGateways(ctx context.Context, in *ec2.DescribeTransitGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeTransitGatewaysOutput, error)
	DescribeTransitGatewayAttachments(ctx context.Context, in *ec2.DescribeTransitGatewayAttachmentsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeTransitGatewayAttachmentsOutput, error)
	DescribeVpcEndpoints(ctx context.Context, in *ec2.DescribeVpcEndpointsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVpcEndpointsOutput, error)
	DescribeInternetGateways(ctx context.Context, in *ec2.DescribeInternetGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInternetGatewaysOutput, error)
	DescribeNatGateways(ctx context.Context, in *ec2.DescribeNatGatewaysInput, optFns ...func(*ec2.Options)) (*ec2.DescribeNatGatewaysOutput, error)
	DescribeFlowLogs(ctx context.Context, in *ec2.DescribeFlowLogsInput, optFns ...func(*ec2.Options)) (*ec2.DescribeFlowLogsOutput, error)
	DescribeInstances(ctx context.Context, in *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeVolumes(ctx context.Context, in *ec2.DescribeVolumesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeVolumesOutput, error)
}

// nameTag returns the value of an EC2 resource's Name tag, or empty when it has
// none.
//
// IT IS SPECIALIZED TO THAT ONE TAG rather than taking a key, because that is the
// only tag this collector reads and a general accessor would advertise a
// capability nothing exercises. The Name tag is what an operator calls a
// resource; every EC2 API returns the id instead, so without it every network
// node is named vpc-0a1b2c3d.
func nameTag(tags []ec2types.Tag) string {
	for _, t := range tags {
		if deref(t.Key) == "Name" {
			return deref(t.Value)
		}
	}
	return ""
}

// ec2ResourceARN builds the id for an EC2-family resource in this walk's region
// and account.
func (w *walkContext) ec2ResourceARN(resourceType, id string) string {
	return ec2ARN(w.region, w.account, resourceType, id)
}

func walkVPCs(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeVpcsOutput, error) {
			return w.clients.EC2.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{NextToken: token})
		},
		func(p *ec2.DescribeVpcsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeVpcsOutput) error {
			for _, v := range p.Vpcs {
				id := deref(v.VpcId)
				if id == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "cidr_block", deref(v.CidrBlock))
				put(detail, "state", string(v.State))
				put(detail, "is_default", fmt.Sprintf("%t", deref(v.IsDefault)))
				w.sink.addNode(newNode(resource{
					id:           w.ec2ResourceARN("vpc", id),
					resourceType: ResourceTypeVPC,
					name:         firstNonEmpty(nameTag(v.Tags), id),
					summary:      fmt.Sprintf("VPC %s (%s) in %s", id, deref(v.CidrBlock), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
			}
			return nil
		})
}

func walkSubnets(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeSubnetsOutput, error) {
			return w.clients.EC2.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{NextToken: token})
		},
		func(p *ec2.DescribeSubnetsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeSubnetsOutput) error {
			for _, s := range p.Subnets {
				id := deref(s.SubnetId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("subnet", id)
				detail := map[string]string{}
				put(detail, "cidr_block", deref(s.CidrBlock))
				put(detail, "availability_zone", deref(s.AvailabilityZone))
				put(detail, "vpc_id", deref(s.VpcId))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeSubnet,
					name:         firstNonEmpty(nameTag(s.Tags), id),
					summary:      fmt.Sprintf("Subnet %s (%s) in %s", id, deref(s.CidrBlock), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				// A VPC CONTAINS its subnets. The edge is emitted from the SUBNET
				// walk rather than the VPC walk because the subnet is the side
				// that names its parent; the VPC list carries no subnet ids.
				if vpc := deref(s.VpcId); vpc != "" {
					w.sink.addEdge(w.ec2ResourceARN("vpc", vpc), arn, EdgeContains,
						map[string]string{"via": "subnet.vpc_id"})
				}
			}
			return nil
		})
}

func walkSecurityGroups(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeSecurityGroupsOutput, error) {
			return w.clients.EC2.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{NextToken: token})
		},
		func(p *ec2.DescribeSecurityGroupsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeSecurityGroupsOutput) error {
			for _, g := range p.SecurityGroups {
				id := deref(g.GroupId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("security-group", id)
				detail := map[string]string{}
				put(detail, "group_name", deref(g.GroupName))
				put(detail, "vpc_id", deref(g.VpcId))
				put(detail, "description", deref(g.Description))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeSecurityGroup,
					name:         firstNonEmpty(deref(g.GroupName), id),
					summary:      fmt.Sprintf("Security group %s (%s) in %s", deref(g.GroupName), id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if vpc := deref(g.VpcId); vpc != "" {
					w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork,
						map[string]string{"via": "security_group.vpc_id"})
				}
				// The RULES are not turned into edges here. They are derived in
				// derive_rules.go together with the CIDR sentinel nodes both
				// endpoints need, so one pass owns the whole shape.
				w.recordSecurityGroupRules(arn, g)
			}
			return nil
		})
}

func walkNetworkACLs(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeNetworkAclsOutput, error) {
			return w.clients.EC2.DescribeNetworkAcls(ctx, &ec2.DescribeNetworkAclsInput{NextToken: token})
		},
		func(p *ec2.DescribeNetworkAclsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeNetworkAclsOutput) error {
			for _, a := range p.NetworkAcls {
				id := deref(a.NetworkAclId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("network-acl", id)
				detail := map[string]string{}
				put(detail, "vpc_id", deref(a.VpcId))
				put(detail, "is_default", fmt.Sprintf("%t", deref(a.IsDefault)))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeNetworkACL,
					name:         firstNonEmpty(nameTag(a.Tags), id),
					summary:      fmt.Sprintf("Network ACL %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				// A network ACL PROTECTS the VPC it belongs to, and is
				// ASSOCIATED_WITH each subnet it is attached to. The two are
				// different relationships: the first is membership, the second is
				// where the rules actually take effect.
				if vpc := deref(a.VpcId); vpc != "" {
					w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeProtects,
						map[string]string{"via": "network_acl.vpc_id"})
				}
				for _, assoc := range a.Associations {
					if sub := deref(assoc.SubnetId); sub != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeAssociatedWithSubnet,
							map[string]string{"association_id": deref(assoc.NetworkAclAssociationId)})
					}
				}
				w.recordNetworkACLRules(arn, a)
			}
			return nil
		})
}

func walkInternetGateways(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeInternetGatewaysOutput, error) {
			return w.clients.EC2.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{NextToken: token})
		},
		func(p *ec2.DescribeInternetGatewaysOutput) *string { return p.NextToken },
		func(p *ec2.DescribeInternetGatewaysOutput) error {
			for _, g := range p.InternetGateways {
				id := deref(g.InternetGatewayId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("internet-gateway", id)
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeInternetGateway,
					name:         firstNonEmpty(nameTag(g.Tags), id),
					summary:      fmt.Sprintf("Internet gateway %s in %s", id, w.region),
					region:       w.region,
				}, w.account))
				for _, att := range g.Attachments {
					if vpc := deref(att.VpcId); vpc != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeRoutesTo,
							map[string]string{"state": string(att.State)})
					}
				}
			}
			return nil
		})
}

func walkNATGateways(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeNatGatewaysOutput, error) {
			return w.clients.EC2.DescribeNatGateways(ctx, &ec2.DescribeNatGatewaysInput{NextToken: token})
		},
		func(p *ec2.DescribeNatGatewaysOutput) *string { return p.NextToken },
		func(p *ec2.DescribeNatGatewaysOutput) error {
			for _, g := range p.NatGateways {
				id := deref(g.NatGatewayId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("natgateway", id)
				detail := map[string]string{}
				put(detail, "state", string(g.State))
				put(detail, "vpc_id", deref(g.VpcId))
				put(detail, "subnet_id", deref(g.SubnetId))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeNATGateway,
					name:         firstNonEmpty(nameTag(g.Tags), id),
					summary:      fmt.Sprintf("NAT gateway %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				// The SUBNET routes via the NAT gateway sitting in it, which is
				// the direction a reachability question is asked in.
				if sub := deref(g.SubnetId); sub != "" {
					w.sink.addEdge(w.ec2ResourceARN("subnet", sub), arn, EdgeRoutesVia,
						map[string]string{"via": "nat_gateway.subnet_id"})
				}
			}
			return nil
		})
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
