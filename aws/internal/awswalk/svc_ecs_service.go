// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
)

// svc_ecs_service.go — one ECS service's node and its five relationships, and
// the ECR repository walk the image lineage resolves against.

func (w *walkContext) addECSService(ctx context.Context, clusterARN string, s ecstypes.Service) {
	arn := deref(s.ServiceArn)
	if arn == "" {
		return
	}
	detail := map[string]string{}
	put(detail, "status", deref(s.Status))
	put(detail, "launch_type", string(s.LaunchType))
	put(detail, "task_definition", deref(s.TaskDefinition))
	detail["desired_count"] = fmt.Sprintf("%d", s.DesiredCount)
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeECSService,
		name:         deref(s.ServiceName),
		summary:      fmt.Sprintf("ECS service %s in cluster %s", deref(s.ServiceName), deref(s.ClusterArn)),
		detail:       detail,
		region:       w.region,
	}, w.account))

	w.sink.addEdge(clusterARN, arn, EdgeContains, map[string]string{"via": "service.cluster_arn"})

	if role := deref(s.RoleArn); role != "" {
		w.sink.addEdge(arn, role, EdgeAssumesRole, map[string]string{"via": "service.role_arn"})
	}
	if s.NetworkConfiguration != nil && s.NetworkConfiguration.AwsvpcConfiguration != nil {
		cfg := s.NetworkConfiguration.AwsvpcConfiguration
		for _, sub := range cfg.Subnets {
			w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
		}
		for _, sg := range cfg.SecurityGroups {
			w.sink.addEdge(arn, w.ec2ResourceARN("security-group", sg), EdgeUsesSecurityGroup, nil)
		}
	}
	// A SERVICE IS EXPOSED VIA ITS TARGET GROUPS, and the target group is what
	// the load balancer routes to — so the edge lands on the target group rather
	// than reaching past it to the balancer. The balancer is one hop further and
	// the ELB walk emits that hop, which keeps each edge a fact one API response
	// actually stated.
	for _, lb := range s.LoadBalancers {
		if tg := deref(lb.TargetGroupArn); tg != "" {
			w.sink.addEdge(arn, tg, EdgeExposedVia,
				map[string]string{"container_name": deref(lb.ContainerName)})
		}
	}
	w.addECSTaskImages(ctx, arn, deref(s.TaskDefinition))
}

// addECSTaskImages emits USES_IMAGE from a service to the ECR repositories its
// task definition's containers pull from.
//
// ONE CALL PER SERVICE. The failure does not fail the ECS walk — a task
// definition this collect cannot read costs one service's image edges, where
// returning the error would throw away every other service's — but it DOES reach
// the account's failure set, so the walk asserts INCOMPLETE naming this read.
// The service's own node is already emitted at that point, so the graph keeps
// what it has and the server is told not to delete what it did not see.
//
// AN IMAGE FROM DOCKER HUB OR ANOTHER REGISTRY YIELDS NO EDGE. The reference is
// matched against the ECR host shape, and a public image names no resource in
// this account — an edge to a composed id for it would resolve against nothing
// and assert a repository the account does not have.
func (w *walkContext) addECSTaskImages(ctx context.Context, serviceARN, taskDef string) {
	if taskDef == "" {
		return
	}
	out, err := w.clients.ECS.DescribeTaskDefinition(ctx,
		&ecs.DescribeTaskDefinitionInput{TaskDefinition: &taskDef})
	if err != nil {
		w.recordSubreadFailure("ecs.DescribeTaskDefinition", taskDef, err)
		return
	}
	// A SUCCESSFUL CALL THAT CARRIED NO TASK DEFINITION is a failure too, and it
	// records as one. The alternative is to treat it as an empty definition,
	// which would assert that this service pulls no images and runs under no task
	// role — a claim the walk has no evidence for.
	if out.TaskDefinition == nil {
		w.recordSubreadFailure("ecs.DescribeTaskDefinition", taskDef,
			errors.New("the call succeeded and carried no task definition"))
		return
	}
	td := out.TaskDefinition
	if role := deref(td.TaskRoleArn); role != "" {
		w.sink.addEdge(serviceARN, role, EdgeAssumesRole, map[string]string{"via": "task_definition.task_role_arn"})
	}
	for _, c := range td.ContainerDefinitions {
		image := deref(c.Image)
		repoARN := ecrRepositoryARNForImage(image, w.region, w.account)
		if repoARN == "" {
			continue
		}
		w.sink.addEdge(serviceARN, repoARN, EdgeUsesImage,
			map[string]string{"image": image, "container": deref(c.Name)})
	}
}

// ecrRepositoryARNForImage maps a container image reference onto the ECR
// repository ARN it names, or returns empty when the image is not in ECR.
//
// THE HOST IS WHAT DECIDES IT: an ECR reference is
// <account>.dkr.ecr.<region>.amazonaws.com/<repository>[:tag][@digest], and the
// repository ARN is built from the account and region IN THE REFERENCE rather
// than from this walk's, because a task may legitimately pull from another
// account's registry and the ARN must name where the image actually lives.
func ecrRepositoryARNForImage(image, fallbackRegion, fallbackAccount string) string {
	if image == "" {
		return ""
	}
	host, path, found := strings.Cut(image, "/")
	if !found || !strings.Contains(host, ".dkr.ecr.") {
		return ""
	}
	// Strip the tag or digest, which are not part of the repository name.
	if i := strings.Index(path, "@"); i >= 0 {
		path = path[:i]
	}
	if i := strings.LastIndex(path, ":"); i >= 0 && !strings.Contains(path[i+1:], "/") {
		path = path[:i]
	}
	if path == "" {
		return ""
	}
	account, rest, ok := strings.Cut(host, ".dkr.ecr.")
	if !ok || account == "" {
		account = fallbackAccount
	}
	region, _, ok := strings.Cut(rest, ".")
	if !ok || region == "" {
		region = fallbackRegion
	}
	return fmt.Sprintf("arn:aws:ecr:%s:%s:repository/%s", region, account, path)
}

func walkECR(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*ecr.DescribeRepositoriesOutput, error) {
			return w.clients.ECR.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{NextToken: token})
		},
		func(p *ecr.DescribeRepositoriesOutput) *string { return p.NextToken },
		func(p *ecr.DescribeRepositoriesOutput) error {
			for _, r := range p.Repositories {
				arn := deref(r.RepositoryArn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "uri", deref(r.RepositoryUri))
				if r.ImageScanningConfiguration != nil {
					detail["scan_on_push"] = fmt.Sprintf("%t", r.ImageScanningConfiguration.ScanOnPush)
				}
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeECRRepository,
					name:         deref(r.RepositoryName),
					summary:      fmt.Sprintf("ECR repository %s in %s", deref(r.RepositoryName), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if r.EncryptionConfiguration != nil {
					if key := deref(r.EncryptionConfiguration.KmsKey); key != "" {
						w.sink.addEdge(arn, key, EdgeEncryptsWith,
							map[string]string{"via": "repository.encryption_configuration"})
					}
				}
			}
			return nil
		})
}
