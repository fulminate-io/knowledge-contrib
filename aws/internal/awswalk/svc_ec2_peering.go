// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// svc_ec2_peering.go — VPC peering, transit gateways and VPC endpoints: the
// three ways traffic leaves one VPC for somewhere else.

func walkVPCPeering(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeVpcPeeringConnectionsOutput, error) {
			return w.clients.EC2.DescribeVpcPeeringConnections(ctx, &ec2.DescribeVpcPeeringConnectionsInput{NextToken: token})
		},
		func(p *ec2.DescribeVpcPeeringConnectionsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeVpcPeeringConnectionsOutput) error {
			for _, c := range p.VpcPeeringConnections {
				id := deref(c.VpcPeeringConnectionId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("vpc-peering-connection", id)
				detail := map[string]string{}
				if c.Status != nil {
					put(detail, "status", string(c.Status.Code))
				}
				if c.RequesterVpcInfo != nil {
					put(detail, "requester_vpc_id", deref(c.RequesterVpcInfo.VpcId))
					put(detail, "requester_account", deref(c.RequesterVpcInfo.OwnerId))
					put(detail, "requester_cidr", deref(c.RequesterVpcInfo.CidrBlock))
				}
				if c.AccepterVpcInfo != nil {
					put(detail, "accepter_vpc_id", deref(c.AccepterVpcInfo.VpcId))
					put(detail, "accepter_account", deref(c.AccepterVpcInfo.OwnerId))
					put(detail, "accepter_cidr", deref(c.AccepterVpcInfo.CidrBlock))
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeVPCPeeringConnection,
					name:         firstNonEmpty(nameTag(c.Tags), id),
					summary:      fmt.Sprintf("VPC peering connection %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				w.recordPeering(c)
			}
			return nil
		})
}

func walkTransitGateways(ctx context.Context, w *walkContext) error {
	err := paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeTransitGatewaysOutput, error) {
			return w.clients.EC2.DescribeTransitGateways(ctx, &ec2.DescribeTransitGatewaysInput{NextToken: token})
		},
		func(p *ec2.DescribeTransitGatewaysOutput) *string { return p.NextToken },
		func(p *ec2.DescribeTransitGatewaysOutput) error {
			for _, g := range p.TransitGateways {
				id := deref(g.TransitGatewayId)
				if id == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "state", string(g.State))
				put(detail, "owner_id", deref(g.OwnerId))
				put(detail, "description", deref(g.Description))
				// THE API RETURNS AN ARN HERE, so it is used unmodified; the
				// composed form is the fallback for a response that omits it.
				w.sink.addNode(newNode(resource{
					id:           firstNonEmpty(deref(g.TransitGatewayArn), w.ec2ResourceARN("transit-gateway", id)),
					resourceType: ResourceTypeTransitGateway,
					name:         firstNonEmpty(nameTag(g.Tags), id),
					summary:      fmt.Sprintf("Transit gateway %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
			}
			return nil
		})
	if err != nil {
		return err
	}
	return walkTransitGatewayAttachments(ctx, w)
}

func walkTransitGatewayAttachments(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeTransitGatewayAttachmentsOutput, error) {
			return w.clients.EC2.DescribeTransitGatewayAttachments(ctx, &ec2.DescribeTransitGatewayAttachmentsInput{NextToken: token})
		},
		func(p *ec2.DescribeTransitGatewayAttachmentsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeTransitGatewayAttachmentsOutput) error {
			for _, a := range p.TransitGatewayAttachments {
				id := deref(a.TransitGatewayAttachmentId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("transit-gateway-attachment", id)
				detail := map[string]string{}
				put(detail, "state", string(a.State))
				put(detail, "resource_type", string(a.ResourceType))
				put(detail, "resource_id", deref(a.ResourceId))
				put(detail, "transit_gateway_id", deref(a.TransitGatewayId))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeTransitGatewayAttach,
					name:         firstNonEmpty(nameTag(a.Tags), id),
					summary:      fmt.Sprintf("Transit gateway attachment %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if tgw := deref(a.TransitGatewayId); tgw != "" {
					w.sink.addEdge(w.ec2ResourceARN("transit-gateway", tgw), arn, EdgeContains,
						map[string]string{"via": "attachment.transit_gateway_id"})
				}
				// The attachment's RESOURCE is a VPC in the ordinary case. The
				// edge is emitted only for that case: a VPN or peering attachment
				// names something this walk does not emit, and an edge to an id
				// composed as if it were a VPC would be wrong rather than
				// dangling.
				if a.ResourceType == "vpc" {
					if vpc := deref(a.ResourceId); vpc != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork,
							map[string]string{"via": "attachment.resource_id"})
					}
				}
			}
			return nil
		})
}

func walkVPCEndpoints(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeVpcEndpointsOutput, error) {
			return w.clients.EC2.DescribeVpcEndpoints(ctx, &ec2.DescribeVpcEndpointsInput{NextToken: token})
		},
		func(p *ec2.DescribeVpcEndpointsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeVpcEndpointsOutput) error {
			for _, e := range p.VpcEndpoints {
				id := deref(e.VpcEndpointId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("vpc-endpoint", id)
				detail := map[string]string{}
				put(detail, "service_name", deref(e.ServiceName))
				put(detail, "endpoint_type", string(e.VpcEndpointType))
				put(detail, "state", string(e.State))
				put(detail, "vpc_id", deref(e.VpcId))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeVPCEndpoint,
					name:         firstNonEmpty(nameTag(e.Tags), id),
					summary:      fmt.Sprintf("VPC endpoint %s for %s in %s", id, deref(e.ServiceName), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if vpc := deref(e.VpcId); vpc != "" {
					w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork,
						map[string]string{"via": "vpc_endpoint.vpc_id"})
				}
				for _, sub := range e.SubnetIds {
					w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
				}
				for _, g := range e.Groups {
					if gid := deref(g.GroupId); gid != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("security-group", gid), EdgeUsesSecurityGroup, nil)
					}
				}
			}
			return nil
		})
}

func walkFlowLogs(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ec2.DescribeFlowLogsOutput, error) {
			return w.clients.EC2.DescribeFlowLogs(ctx, &ec2.DescribeFlowLogsInput{NextToken: token})
		},
		func(p *ec2.DescribeFlowLogsOutput) *string { return p.NextToken },
		func(p *ec2.DescribeFlowLogsOutput) error {
			for _, f := range p.FlowLogs {
				id := deref(f.FlowLogId)
				if id == "" {
					continue
				}
				arn := w.ec2ResourceARN("vpc-flow-log", id)
				detail := map[string]string{}
				put(detail, "resource_id", deref(f.ResourceId))
				put(detail, "traffic_type", string(f.TrafficType))
				put(detail, "destination_type", string(f.LogDestinationType))
				put(detail, "destination", deref(f.LogDestination))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeFlowLog,
					name:         id,
					summary:      fmt.Sprintf("Flow log %s for %s in %s", id, deref(f.ResourceId), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				// WHERE THE FLOW LOG LANDS is the question this edge answers, and
				// the destination is already an ARN — an S3 bucket or a CloudWatch
				// log group — so it is used unmodified rather than composed.
				if dest := deref(f.LogDestination); dest != "" {
					w.sink.addEdge(arn, dest, EdgeSinksTo,
						map[string]string{"destination_type": string(f.LogDestinationType)})
				}
				if grp := deref(f.LogGroupName); grp != "" && deref(f.LogDestination) == "" {
					w.sink.addEdge(arn, logGroupARN(w.region, w.account, grp), EdgeSinksTo,
						map[string]string{"log_group_name": grp})
				}
			}
			return nil
		})
}
