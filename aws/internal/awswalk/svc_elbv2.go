// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

// svc_elbv2.go — application and network load balancers, their target groups,
// and what those groups actually point at.

type elbv2API interface {
	DescribeLoadBalancers(ctx context.Context, in *elbv2.DescribeLoadBalancersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeLoadBalancersOutput, error)
	DescribeTargetGroups(ctx context.Context, in *elbv2.DescribeTargetGroupsInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetGroupsOutput, error)
	DescribeTargetHealth(ctx context.Context, in *elbv2.DescribeTargetHealthInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeTargetHealthOutput, error)
	DescribeListeners(ctx context.Context, in *elbv2.DescribeListenersInput, optFns ...func(*elbv2.Options)) (*elbv2.DescribeListenersOutput, error)
}

func walkELBv2(ctx context.Context, w *walkContext) error {
	if err := w.walkLoadBalancers(ctx); err != nil {
		return err
	}
	return w.walkTargetGroups(ctx)
}

func (w *walkContext) walkLoadBalancers(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*elbv2.DescribeLoadBalancersOutput, error) {
			return w.clients.ELBv2.DescribeLoadBalancers(ctx, &elbv2.DescribeLoadBalancersInput{Marker: token})
		},
		func(p *elbv2.DescribeLoadBalancersOutput) *string { return p.NextMarker },
		func(p *elbv2.DescribeLoadBalancersOutput) error {
			for _, lb := range p.LoadBalancers {
				arn := deref(lb.LoadBalancerArn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "scheme", string(lb.Scheme))
				put(detail, "lb_type", string(lb.Type))
				put(detail, "dns_name", deref(lb.DNSName))
				put(detail, "vpc_id", deref(lb.VpcId))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeELBv2LoadBalancer,
					name:         deref(lb.LoadBalancerName),
					summary:      fmt.Sprintf("%s load balancer %s in %s", string(lb.Type), deref(lb.LoadBalancerName), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))

				if vpc := deref(lb.VpcId); vpc != "" {
					w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
				}
				for _, az := range lb.AvailabilityZones {
					if sub := deref(az.SubnetId); sub != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
					}
				}
				for _, sg := range lb.SecurityGroups {
					w.sink.addEdge(arn, w.ec2ResourceARN("security-group", sg), EdgeUsesSecurityGroup, nil)
				}
				w.walkListenerCertificates(ctx, arn)
			}
			return nil
		})
}

// walkListenerCertificates emits USES_CERT from a balancer to the ACM
// certificates its HTTPS listeners present.
//
// THE FAILURE DOES NOT ABORT THE ELB WALK, AND IT IS NOT SWALLOWED EITHER. One
// balancer's listeners going unread costs that balancer's certificate edges,
// where failing the whole ELB walk would throw away every other balancer's. So
// the read records into the account's failure set: the result keeps what it read
// and the account asserts INCOMPLETE naming this operation, which is what stops
// the server's full-replace deletion from removing the edges this call did not
// get to name.
//
// IT PAGINATES. DescribeListenersOutput carries a NextMarker, so a balancer with
// more listeners than one page returns a truncated set that looks exactly like a
// balancer with few listeners.
func (w *walkContext) walkListenerCertificates(ctx context.Context, lbARN string) {
	err := paginate(ctx,
		func(ctx context.Context, token *string) (*elbv2.DescribeListenersOutput, error) {
			return w.clients.ELBv2.DescribeListeners(ctx,
				&elbv2.DescribeListenersInput{LoadBalancerArn: &lbARN, Marker: token})
		},
		func(p *elbv2.DescribeListenersOutput) *string { return p.NextMarker },
		func(p *elbv2.DescribeListenersOutput) error {
			for _, l := range p.Listeners {
				for _, c := range l.Certificates {
					if arn := deref(c.CertificateArn); arn != "" {
						w.sink.addEdge(lbARN, arn, EdgeUsesCert,
							map[string]string{"listener": deref(l.ListenerArn)})
					}
				}
			}
			return nil
		})
	w.recordSubreadFailure("elbv2.DescribeListeners", lbARN, err)
}

func (w *walkContext) walkTargetGroups(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*elbv2.DescribeTargetGroupsOutput, error) {
			return w.clients.ELBv2.DescribeTargetGroups(ctx, &elbv2.DescribeTargetGroupsInput{Marker: token})
		},
		func(p *elbv2.DescribeTargetGroupsOutput) *string { return p.NextMarker },
		func(p *elbv2.DescribeTargetGroupsOutput) error {
			for _, tg := range p.TargetGroups {
				arn := deref(tg.TargetGroupArn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "protocol", string(tg.Protocol))
				put(detail, "target_type", string(tg.TargetType))
				put(detail, "vpc_id", deref(tg.VpcId))
				if tg.Port != nil {
					detail["port"] = fmt.Sprintf("%d", *tg.Port)
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeELBv2TargetGroup,
					name:         deref(tg.TargetGroupName),
					summary:      fmt.Sprintf("Target group %s in %s", deref(tg.TargetGroupName), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))

				// A BALANCER EXPOSES ITS TARGET GROUP. The group carries the
				// balancer ARNs rather than the other way round, so this is the
				// side that can state the relationship without a second call.
				for _, lbARN := range tg.LoadBalancerArns {
					w.sink.addEdge(lbARN, arn, EdgeExposedVia,
						map[string]string{"via": "target_group.load_balancer_arns"})
				}
				w.walkTargetHealth(ctx, arn, string(tg.TargetType))
			}
			return nil
		})
}

// walkTargetHealth emits TARGETS from a group to each registered target.
//
// THE TARGET'S ID DEPENDS ON THE GROUP'S TYPE, and reading it wrong is how this
// edge lands on nothing: an `instance` group's target id is an instance id that
// has to be composed into an ARN, while a `lambda` or `alb` group's target id IS
// already an ARN. An `ip` target names no AWS resource at all and yields no edge,
// because an address is not something the graph holds.
//
// A FAILED READ RECORDS rather than returning silently: unread targets are edges
// the account does not carry, and a COMPLETE assertion over them is what lets the
// server delete the ones a previous collect did name.
func (w *walkContext) walkTargetHealth(ctx context.Context, tgARN, targetType string) {
	out, err := w.clients.ELBv2.DescribeTargetHealth(ctx,
		&elbv2.DescribeTargetHealthInput{TargetGroupArn: &tgARN})
	if err != nil {
		w.recordSubreadFailure("elbv2.DescribeTargetHealth", tgARN, err)
		return
	}
	for _, h := range out.TargetHealthDescriptions {
		if h.Target == nil {
			continue
		}
		id := deref(h.Target.Id)
		if id == "" {
			continue
		}
		var to string
		switch targetType {
		case "instance":
			to = w.ec2ResourceARN("instance", id)
		case "lambda", "alb":
			to = id
		default:
			continue
		}
		w.sink.addEdge(tgARN, to, EdgeTargets, map[string]string{"target_type": targetType})
	}
}
