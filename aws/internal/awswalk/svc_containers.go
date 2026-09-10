// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/eks"
)

// svc_containers.go — EKS, ECS and ECR: where an account runs containers and
// where it keeps their images.

type eksAPI interface {
	ListClusters(ctx context.Context, in *eks.ListClustersInput, optFns ...func(*eks.Options)) (*eks.ListClustersOutput, error)
	DescribeCluster(ctx context.Context, in *eks.DescribeClusterInput, optFns ...func(*eks.Options)) (*eks.DescribeClusterOutput, error)
}

type ecsAPI interface {
	ListClusters(ctx context.Context, in *ecs.ListClustersInput, optFns ...func(*ecs.Options)) (*ecs.ListClustersOutput, error)
	DescribeClusters(ctx context.Context, in *ecs.DescribeClustersInput, optFns ...func(*ecs.Options)) (*ecs.DescribeClustersOutput, error)
	ListServices(ctx context.Context, in *ecs.ListServicesInput, optFns ...func(*ecs.Options)) (*ecs.ListServicesOutput, error)
	DescribeServices(ctx context.Context, in *ecs.DescribeServicesInput, optFns ...func(*ecs.Options)) (*ecs.DescribeServicesOutput, error)
	DescribeTaskDefinition(ctx context.Context, in *ecs.DescribeTaskDefinitionInput, optFns ...func(*ecs.Options)) (*ecs.DescribeTaskDefinitionOutput, error)
}

type ecrAPI interface {
	DescribeRepositories(ctx context.Context, in *ecr.DescribeRepositoriesInput, optFns ...func(*ecr.Options)) (*ecr.DescribeRepositoriesOutput, error)
}

// walkEKS lists the clusters and describes each one.
//
// THE LIST RETURNS NAMES ONLY, so the describe is not optional: the cluster's
// ARN, its VPC and — the reason this walk matters to the rest of the graph — its
// OIDC issuer all come from the describe. The issuer is what turns an IAM role's
// trust condition into a WORKLOAD_IDENTITY edge, so a walk that skipped the
// describe would leave that relationship underivable for the whole account.
func walkEKS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*eks.ListClustersOutput, error) {
			return w.clients.EKS.ListClusters(ctx, &eks.ListClustersInput{NextToken: token})
		},
		func(p *eks.ListClustersOutput) *string { return p.NextToken },
		func(p *eks.ListClustersOutput) error {
			for _, name := range p.Clusters {
				if err := w.describeEKSCluster(ctx, name); err != nil {
					return err
				}
			}
			return nil
		})
}

func (w *walkContext) describeEKSCluster(ctx context.Context, name string) error {
	out, err := w.clients.EKS.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: &name})
	if err != nil {
		return fmt.Errorf("describe cluster %s: %w", name, err)
	}
	c := out.Cluster
	if c == nil {
		return nil
	}
	arn := deref(c.Arn)
	if arn == "" {
		return nil
	}
	detail := map[string]string{}
	put(detail, "version", deref(c.Version))
	put(detail, "status", string(c.Status))
	put(detail, "endpoint", deref(c.Endpoint))
	var issuer string
	if c.Identity != nil && c.Identity.Oidc != nil {
		issuer = deref(c.Identity.Oidc.Issuer)
		put(detail, "oidc_issuer", issuer)
	}
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeEKSCluster,
		name:         name,
		summary:      fmt.Sprintf("EKS cluster %s in %s", name, w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))

	if c.ResourcesVpcConfig != nil {
		if vpc := deref(c.ResourcesVpcConfig.VpcId); vpc != "" {
			w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
		}
		for _, sub := range c.ResourcesVpcConfig.SubnetIds {
			w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
		}
		for _, sg := range c.ResourcesVpcConfig.SecurityGroupIds {
			w.sink.addEdge(arn, w.ec2ResourceARN("security-group", sg), EdgeUsesSecurityGroup, nil)
		}
	}
	if roleARN := deref(c.RoleArn); roleARN != "" {
		w.sink.addEdge(arn, roleARN, EdgeAssumesRole, map[string]string{"via": "cluster.role_arn"})
	}
	// THE ISSUER IS RECORDED WITHOUT ITS SCHEME, because that is the form an IAM
	// trust condition key uses: `oidc.eks.<region>.amazonaws.com/id/<id>:sub`.
	// Recording the full URL would make every comparison in derive_irsa.go miss.
	w.recordEKSCluster(arn, strings.TrimPrefix(strings.TrimPrefix(issuer, "https://"), "http://"))
	return nil
}

// walkECS lists clusters, describes them, then lists and describes each
// cluster's services.
//
// THE DESCRIBE CALLS TAKE BATCHES OF UP TO TEN, which is the API's own limit, so
// the batching below is the contract rather than a tuning choice: a call carrying
// more is refused.
func walkECS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ecs.ListClustersOutput, error) {
			return w.clients.ECS.ListClusters(ctx, &ecs.ListClustersInput{NextToken: token})
		},
		func(p *ecs.ListClustersOutput) *string { return p.NextToken },
		func(p *ecs.ListClustersOutput) error {
			for _, batch := range chunk(p.ClusterArns, ecsDescribeBatch) {
				if err := w.describeECSClusters(ctx, batch); err != nil {
					return err
				}
			}
			return nil
		})
}

// ecsDescribeBatch is the ECS API's own maximum for a describe call.
const ecsDescribeBatch = 10

func (w *walkContext) describeECSClusters(ctx context.Context, arns []string) error {
	out, err := w.clients.ECS.DescribeClusters(ctx, &ecs.DescribeClustersInput{Clusters: arns})
	if err != nil {
		return fmt.Errorf("describe clusters: %w", err)
	}
	for _, c := range out.Clusters {
		arn := deref(c.ClusterArn)
		if arn == "" {
			continue
		}
		detail := map[string]string{}
		put(detail, "status", deref(c.Status))
		if c.RunningTasksCount != 0 {
			detail["running_tasks"] = fmt.Sprintf("%d", c.RunningTasksCount)
		}
		w.sink.addNode(newNode(resource{
			id:           arn,
			resourceType: ResourceTypeECSCluster,
			name:         deref(c.ClusterName),
			summary:      fmt.Sprintf("ECS cluster %s in %s", deref(c.ClusterName), w.region),
			detail:       detail,
			region:       w.region,
		}, w.account))
		if err := w.walkECSServices(ctx, arn); err != nil {
			return err
		}
	}
	return nil
}

func (w *walkContext) walkECSServices(ctx context.Context, clusterARN string) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ecs.ListServicesOutput, error) {
			return w.clients.ECS.ListServices(ctx, &ecs.ListServicesInput{Cluster: &clusterARN, NextToken: token})
		},
		func(p *ecs.ListServicesOutput) *string { return p.NextToken },
		func(p *ecs.ListServicesOutput) error {
			for _, batch := range chunk(p.ServiceArns, ecsDescribeBatch) {
				out, err := w.clients.ECS.DescribeServices(ctx,
					&ecs.DescribeServicesInput{Cluster: &clusterARN, Services: batch})
				if err != nil {
					return fmt.Errorf("describe services in %s: %w", clusterARN, err)
				}
				for _, s := range out.Services {
					w.addECSService(ctx, clusterARN, s)
				}
			}
			return nil
		})
}

// chunk splits a slice into batches of at most size.
func chunk[T any](in []T, size int) [][]T {
	if size <= 0 || len(in) == 0 {
		return nil
	}
	var out [][]T
	for i := 0; i < len(in); i += size {
		end := min(i+size, len(in))
		out = append(out, in[i:end])
	}
	return out
}
