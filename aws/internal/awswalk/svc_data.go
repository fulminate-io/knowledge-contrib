// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
)

// svc_data.go — relational and key-value stores: RDS, Redshift and DynamoDB.

type rdsAPI interface {
	DescribeDBInstances(ctx context.Context, in *rds.DescribeDBInstancesInput, optFns ...func(*rds.Options)) (*rds.DescribeDBInstancesOutput, error)
}

type redshiftAPI interface {
	DescribeClusters(ctx context.Context, in *redshift.DescribeClustersInput, optFns ...func(*redshift.Options)) (*redshift.DescribeClustersOutput, error)
}

type dynamoDBAPI interface {
	ListTables(ctx context.Context, in *dynamodb.ListTablesInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error)
	DescribeTable(ctx context.Context, in *dynamodb.DescribeTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error)
	DescribeContinuousBackups(ctx context.Context, in *dynamodb.DescribeContinuousBackupsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DescribeContinuousBackupsOutput, error)
	ListBackups(ctx context.Context, in *dynamodb.ListBackupsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListBackupsOutput, error)
}

func walkRDS(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*rds.DescribeDBInstancesOutput, error) {
			return w.clients.RDS.DescribeDBInstances(ctx, &rds.DescribeDBInstancesInput{Marker: token})
		},
		func(p *rds.DescribeDBInstancesOutput) *string { return p.Marker },
		func(p *rds.DescribeDBInstancesOutput) error {
			for _, db := range p.DBInstances {
				w.addDBInstance(db)
			}
			return nil
		})
}

// addDBInstance emits one database and its five relationships.
//
// IT IS SPLIT FROM THE PAGE LOOP because an RDS instance carries more nested
// optional blocks than any other resource here — a subnet group holding subnets,
// a security-group membership list, an encryption key, a replica list — and each
// is a nil check. Folding them into the page visitor put the whole walk over the
// complexity cap, which is the cap doing its job.
func (w *walkContext) addDBInstance(db rdstypes.DBInstance) {
	arn := deref(db.DBInstanceArn)
	if arn == "" {
		return
	}
	detail := map[string]string{}
	put(detail, "engine", deref(db.Engine))
	put(detail, "engine_version", deref(db.EngineVersion))
	put(detail, "instance_class", deref(db.DBInstanceClass))
	put(detail, "status", deref(db.DBInstanceStatus))
	put(detail, "multi_az", fmt.Sprintf("%t", deref(db.MultiAZ)))
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeRDSInstance,
		name:         deref(db.DBInstanceIdentifier),
		summary: fmt.Sprintf("RDS %s instance %s in %s",
			deref(db.Engine), deref(db.DBInstanceIdentifier), w.region),
		detail: detail,
		region: w.region,
	}, w.account))

	if db.DBSubnetGroup != nil {
		if vpc := deref(db.DBSubnetGroup.VpcId); vpc != "" {
			w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
		}
		for _, sn := range db.DBSubnetGroup.Subnets {
			if sub := deref(sn.SubnetIdentifier); sub != "" {
				w.sink.addEdge(arn, w.ec2ResourceARN("subnet", sub), EdgeUsesSubnet, nil)
			}
		}
	}
	for _, sg := range db.VpcSecurityGroups {
		if gid := deref(sg.VpcSecurityGroupId); gid != "" {
			w.sink.addEdge(arn, w.ec2ResourceARN("security-group", gid), EdgeUsesSecurityGroup, nil)
		}
	}
	if key := deref(db.KmsKeyId); key != "" {
		w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "db_instance.kms_key_id"})
	}
	// A READ REPLICA IS A REPLICATION TARGET, and the source names it: the edge
	// runs source to replica, which is the direction data moves.
	for _, replica := range db.ReadReplicaDBInstanceIdentifiers {
		w.sink.addEdge(arn, rdsInstanceARN(w.region, w.account, replica), EdgeReplicatesTo,
			map[string]string{"via": "db_instance.read_replica_identifiers"})
	}
}

// rdsInstanceARN composes an RDS instance ARN from its identifier. The
// read-replica list carries identifiers rather than ARNs, so the edge's far end
// has to be composed; every other RDS id in this walk is the ARN the API
// returned.
func rdsInstanceARN(region, account, identifier string) string {
	return fmt.Sprintf("arn:aws:rds:%s:%s:db:%s", region, account, identifier)
}

func walkRedshift(ctx context.Context, w *walkContext) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*redshift.DescribeClustersOutput, error) {
			return w.clients.Redshift.DescribeClusters(ctx, &redshift.DescribeClustersInput{Marker: token})
		},
		func(p *redshift.DescribeClustersOutput) *string { return p.Marker },
		func(p *redshift.DescribeClustersOutput) error {
			for _, c := range p.Clusters {
				id := deref(c.ClusterIdentifier)
				if id == "" {
					continue
				}
				arn := fmt.Sprintf("arn:aws:redshift:%s:%s:cluster:%s", w.region, w.account, id)
				detail := map[string]string{}
				put(detail, "node_type", deref(c.NodeType))
				put(detail, "status", deref(c.ClusterStatus))
				put(detail, "vpc_id", deref(c.VpcId))
				detail["number_of_nodes"] = fmt.Sprintf("%d", deref(c.NumberOfNodes))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeRedshiftCluster,
					name:         id,
					summary:      fmt.Sprintf("Redshift cluster %s in %s", id, w.region),
					detail:       detail,
					region:       w.region,
				}, w.account))
				if vpc := deref(c.VpcId); vpc != "" {
					w.sink.addEdge(arn, w.ec2ResourceARN("vpc", vpc), EdgeUsesNetwork, nil)
				}
				for _, sg := range c.VpcSecurityGroups {
					if gid := deref(sg.VpcSecurityGroupId); gid != "" {
						w.sink.addEdge(arn, w.ec2ResourceARN("security-group", gid), EdgeUsesSecurityGroup, nil)
					}
				}
				if key := deref(c.KmsKeyId); key != "" {
					w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "cluster.kms_key_id"})
				}
			}
			return nil
		})
}
