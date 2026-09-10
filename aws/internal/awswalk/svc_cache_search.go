// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/efs"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
)

// svc_cache_search.go — ElastiCache, OpenSearch and EFS: the three stateful
// services attached to a VPC that are neither relational nor object storage.

type elastiCacheAPI interface {
	DescribeCacheClusters(ctx context.Context, in *elasticache.DescribeCacheClustersInput, optFns ...func(*elasticache.Options)) (*elasticache.DescribeCacheClustersOutput, error)
	DescribeReplicationGroups(ctx context.Context, in *elasticache.DescribeReplicationGroupsInput, optFns ...func(*elasticache.Options)) (*elasticache.DescribeReplicationGroupsOutput, error)
}

type openSearchAPI interface {
	ListDomainNames(ctx context.Context, in *opensearch.ListDomainNamesInput, optFns ...func(*opensearch.Options)) (*opensearch.ListDomainNamesOutput, error)
	DescribeDomain(ctx context.Context, in *opensearch.DescribeDomainInput, optFns ...func(*opensearch.Options)) (*opensearch.DescribeDomainOutput, error)
}

type efsAPI interface {
	DescribeFileSystems(ctx context.Context, in *efs.DescribeFileSystemsInput, optFns ...func(*efs.Options)) (*efs.DescribeFileSystemsOutput, error)
}

func walkElastiCache(ctx context.Context, w *walkContext) error {
	if err := w.walkCacheClusters(ctx); err != nil {
		return err
	}
	return w.walkReplicationGroups(ctx)
}

func (w *walkContext) walkCacheClusters(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*elasticache.DescribeCacheClustersOutput, error) {
			return w.clients.ElastiCache.DescribeCacheClusters(ctx,
				&elasticache.DescribeCacheClustersInput{Marker: token})
		},
		func(p *elasticache.DescribeCacheClustersOutput) *string { return p.Marker },
		func(p *elasticache.DescribeCacheClustersOutput) error {
			for _, c := range p.CacheClusters {
				id := deref(c.CacheClusterId)
				if id == "" {
					continue
				}
				arn := firstNonEmpty(deref(c.ARN),
					fmt.Sprintf("arn:aws:elasticache:%s:%s:cluster:%s", w.region, w.account, id))
				detail := map[string]string{}
				put(detail, "engine", deref(c.Engine))
				put(detail, "engine_version", deref(c.EngineVersion))
				put(detail, "status", deref(c.CacheClusterStatus))
				put(detail, "node_type", deref(c.CacheNodeType))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeElastiCacheCluster,
					name:         id,
					summary:      fmt.Sprintf("ElastiCache %s cluster %s in %s", deref(c.Engine), id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				for _, sg := range c.SecurityGroups {
					if gid := deref(sg.SecurityGroupId); gid != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("security-group", gid), EdgeUsesSecurityGroup, nil)
					}
				}
			}
			return nil
		})
}

// walkReplicationGroups emits the group and REPLICATES_TO each member cluster.
//
// THE GROUP IS THE REPLICATION UNIT and the member clusters are its copies, so
// the edge runs group to member: that is the direction that answers "what is this
// group's data replicated onto".
func (w *walkContext) walkReplicationGroups(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*elasticache.DescribeReplicationGroupsOutput, error) {
			return w.clients.ElastiCache.DescribeReplicationGroups(ctx,
				&elasticache.DescribeReplicationGroupsInput{Marker: token})
		},
		func(p *elasticache.DescribeReplicationGroupsOutput) *string { return p.Marker },
		func(p *elasticache.DescribeReplicationGroupsOutput) error {
			for _, g := range p.ReplicationGroups {
				id := deref(g.ReplicationGroupId)
				if id == "" {
					continue
				}
				arn := firstNonEmpty(deref(g.ARN),
					fmt.Sprintf("arn:aws:elasticache:%s:%s:replicationgroup:%s", w.region, w.account, id))
				detail := map[string]string{}
				put(detail, "status", deref(g.Status))
				put(detail, "description", deref(g.Description))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeElastiCacheReplicationGrp,
					name:         id,
					summary:      fmt.Sprintf("ElastiCache replication group %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				for _, member := range g.MemberClusters {
					w.sink.addEdge(arn,
						fmt.Sprintf("arn:aws:elasticache:%s:%s:cluster:%s", w.region, w.account, member),
						EdgeReplicatesTo, map[string]string{"member_cluster": member})
				}
				if key := deref(g.KmsKeyId); key != "" {
					w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "replication_group.kms_key_id"})
				}
			}
			return nil
		})
}

// walkOpenSearch lists the domains and describes each.
//
// THE LIST RETURNS NAMES ONLY, so the describe is not optional: the ARN, the VPC
// attachment and the encryption key all come from it.
func walkOpenSearch(ctx context.Context, w *walkContext) error {
	out, err := w.clients.OpenSearch.ListDomainNames(ctx, &opensearch.ListDomainNamesInput{})
	if err != nil {
		return fmt.Errorf("list domain names: %w", err)
	}
	for _, d := range out.DomainNames {
		name := deref(d.DomainName)
		if name == "" {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := w.describeOpenSearchDomain(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func (w *walkContext) describeOpenSearchDomain(ctx context.Context, name string) error {
	out, err := w.clients.OpenSearch.DescribeDomain(ctx, &opensearch.DescribeDomainInput{DomainName: &name})
	if err != nil {
		return fmt.Errorf("describe domain %s: %w", name, err)
	}
	d := out.DomainStatus
	if d == nil {
		return nil
	}
	arn := deref(d.ARN)
	if arn == "" {
		return nil
	}
	detail := map[string]string{}
	put(detail, "engine_version", deref(d.EngineVersion))
	put(detail, "endpoint", deref(d.Endpoint))
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeOpenSearchDomain,
		name:         name,
		summary:      fmt.Sprintf("OpenSearch domain %s in %s", name, w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))
	if d.VPCOptions != nil {
		if vpc := deref(d.VPCOptions.VPCId); vpc != "" {
			w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
		}
		for _, sub := range d.VPCOptions.SubnetIds {
			w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
		}
		for _, sg := range d.VPCOptions.SecurityGroupIds {
			w.sink.addEdge(arn, w.ec2ResourceARN("security-group", sg), EdgeUsesSecurityGroup, nil)
		}
	}
	if d.EncryptionAtRestOptions != nil {
		if key := deref(d.EncryptionAtRestOptions.KmsKeyId); key != "" {
			w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "domain.encryption_at_rest"})
		}
	}
	return nil
}

func walkEFS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*efs.DescribeFileSystemsOutput, error) {
			return w.clients.EFS.DescribeFileSystems(ctx, &efs.DescribeFileSystemsInput{Marker: token})
		},
		func(p *efs.DescribeFileSystemsOutput) *string { return p.NextMarker },
		func(p *efs.DescribeFileSystemsOutput) error {
			for _, fs := range p.FileSystems {
				arn := deref(fs.FileSystemArn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "life_cycle_state", string(fs.LifeCycleState))
				put(detail, "performance_mode", string(fs.PerformanceMode))
				put(detail, "encrypted", fmt.Sprintf("%t", deref(fs.Encrypted)))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeEFSFileSystem,
					name:         firstNonEmpty(deref(fs.Name), deref(fs.FileSystemId)),
					summary:      fmt.Sprintf("EFS file system %s in %s", deref(fs.FileSystemId), w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if key := deref(fs.KmsKeyId); key != "" {
					w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "file_system.kms_key_id"})
				}
			}
			return nil
		})
}
