// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/efs"
	efstypes "github.com/aws/aws-sdk-go-v2/service/efs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/opensearch"
	opensearchtypes "github.com/aws/aws-sdk-go-v2/service/opensearch/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/aws/aws-sdk-go-v2/service/redshift"
	redshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
)

// fixture_data_test.go — the data-store half of the fully-populated fake
// account: RDS, Redshift, DynamoDB, ElastiCache, OpenSearch, EFS and KMS. Split
// from fixture_services_test.go for length.

func fixtureRDS() *fakeRds {
	return &fakeRds{
		describeDBInstances: func(*rds.DescribeDBInstancesInput) (*rds.DescribeDBInstancesOutput, error) {
			return &rds.DescribeDBInstancesOutput{DBInstances: []rdstypes.DBInstance{{
				DBInstanceArn:        new("arn:aws:rds:us-east-1:123456789012:db:orders"),
				DBInstanceIdentifier: new("orders"), Engine: new("postgres"),
				EngineVersion: new("16.4"), DBInstanceClass: new("db.t4g.medium"),
				DBInstanceStatus: new("available"), MultiAZ: new(true),
				KmsKeyId: new(fixtureKMSKeyARN),
				DBSubnetGroup: &rdstypes.DBSubnetGroup{
					VpcId:   new(fixtureVPC),
					Subnets: []rdstypes.Subnet{{SubnetIdentifier: new(fixtureSubnet)}},
				},
				VpcSecurityGroups:                []rdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: new(fixtureSG)}},
				ReadReplicaDBInstanceIdentifiers: []string{"orders-replica"},
			}, {
				// THE READ REPLICA ITSELF, and it is in the fixture because the
				// REPLICATES_TO edge above names it. A replica of an instance in
				// this account IS enumerated by this same walk, so the correct
				// assertion for that edge's far end is that it RESOLVES. Leaving
				// it out was what made an "external endpoint" class for the whole
				// rds namespace look necessary, and that class then admitted every
				// typo in it.
				DBInstanceArn:        new("arn:aws:rds:us-east-1:123456789012:db:orders-replica"),
				DBInstanceIdentifier: new("orders-replica"), Engine: new("postgres"),
				EngineVersion: new("16.4"), DBInstanceClass: new("db.t4g.medium"),
				DBInstanceStatus: new("available"), MultiAZ: new(false),
				KmsKeyId: new(fixtureKMSKeyARN),
				DBSubnetGroup: &rdstypes.DBSubnetGroup{
					VpcId:   new(fixtureVPC),
					Subnets: []rdstypes.Subnet{{SubnetIdentifier: new(fixtureSubnet)}},
				},
				VpcSecurityGroups: []rdstypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: new(fixtureSG)}},
			}}}, nil
		},
	}
}

func fixtureRedshift() *fakeRedshift {
	return &fakeRedshift{
		describeClusters: func(*redshift.DescribeClustersInput) (*redshift.DescribeClustersOutput, error) {
			return &redshift.DescribeClustersOutput{Clusters: []redshifttypes.Cluster{{
				ClusterIdentifier: new("analytics"), NodeType: new("ra3.xlplus"),
				ClusterStatus: new("available"), NumberOfNodes: new(int32(2)),
				VpcId: new(fixtureVPC), KmsKeyId: new(fixtureKMSKeyARN),
				VpcSecurityGroups: []redshifttypes.VpcSecurityGroupMembership{{VpcSecurityGroupId: new(fixtureSG)}},
			}}}, nil
		},
	}
}

func fixtureDynamoDB() *fakeDynamoDB {
	return &fakeDynamoDB{
		listTables: func(*dynamodb.ListTablesInput) (*dynamodb.ListTablesOutput, error) {
			return &dynamodb.ListTablesOutput{TableNames: []string{"orders"}}, nil
		},
		describeTable: func(in *dynamodb.DescribeTableInput) (*dynamodb.DescribeTableOutput, error) {
			if deref(in.TableName) != "orders" {
				return &dynamodb.DescribeTableOutput{}, nil
			}
			return &dynamodb.DescribeTableOutput{Table: &dynamodbtypes.TableDescription{
				TableArn: new(fixtureTableARN), TableStatus: dynamodbtypes.TableStatusActive,
				ItemCount:      new(int64(42)),
				SSEDescription: &dynamodbtypes.SSEDescription{KMSMasterKeyArn: new(fixtureKMSKeyARN)},
			}}, nil
		},
		describeContinuousBackups: func(*dynamodb.DescribeContinuousBackupsInput) (*dynamodb.DescribeContinuousBackupsOutput, error) {
			return &dynamodb.DescribeContinuousBackupsOutput{
				ContinuousBackupsDescription: &dynamodbtypes.ContinuousBackupsDescription{
					PointInTimeRecoveryDescription: &dynamodbtypes.PointInTimeRecoveryDescription{
						PointInTimeRecoveryStatus: dynamodbtypes.PointInTimeRecoveryStatusEnabled,
					},
				},
			}, nil
		},
		listBackups: func(*dynamodb.ListBackupsInput) (*dynamodb.ListBackupsOutput, error) {
			return &dynamodb.ListBackupsOutput{BackupSummaries: []dynamodbtypes.BackupSummary{{
				BackupArn:  new(fixtureTableARN + "/backup/01"),
				BackupName: new("orders-nightly"), TableName: new("orders"),
				TableArn:   new(fixtureTableARN),
				BackupType: dynamodbtypes.BackupTypeUser, BackupStatus: dynamodbtypes.BackupStatusAvailable,
			}}}, nil
		},
	}
}

func fixtureElastiCache() *fakeElastiCache {
	return &fakeElastiCache{
		describeCacheClusters: func(*elasticache.DescribeCacheClustersInput) (*elasticache.DescribeCacheClustersOutput, error) {
			return &elasticache.DescribeCacheClustersOutput{CacheClusters: []elasticachetypes.CacheCluster{{
				CacheClusterId: new("sessions-001"), Engine: new("redis"),
				CacheClusterStatus: new("available"), CacheNodeType: new("cache.t4g.micro"),
				SecurityGroups: []elasticachetypes.SecurityGroupMembership{{SecurityGroupId: new(fixtureSG)}},
			}}}, nil
		},
		describeReplicationGroups: func(*elasticache.DescribeReplicationGroupsInput) (*elasticache.DescribeReplicationGroupsOutput, error) {
			return &elasticache.DescribeReplicationGroupsOutput{
				ReplicationGroups: []elasticachetypes.ReplicationGroup{{
					ReplicationGroupId: new("sessions"), ARN: new(fixtureCacheGroupARN),
					Status: new("available"), Description: new("session store"),
					MemberClusters: []string{"sessions-001"},
					KmsKeyId:       new(fixtureKMSKeyARN),
				}},
			}, nil
		},
	}
}

func fixtureOpenSearch() *fakeOpenSearch {
	return &fakeOpenSearch{
		listDomainNames: func(*opensearch.ListDomainNamesInput) (*opensearch.ListDomainNamesOutput, error) {
			return &opensearch.ListDomainNamesOutput{
				DomainNames: []opensearchtypes.DomainInfo{{DomainName: new("logs")}},
			}, nil
		},
		describeDomain: func(in *opensearch.DescribeDomainInput) (*opensearch.DescribeDomainOutput, error) {
			if deref(in.DomainName) != "logs" {
				return &opensearch.DescribeDomainOutput{}, nil
			}
			return &opensearch.DescribeDomainOutput{DomainStatus: &opensearchtypes.DomainStatus{
				ARN:        new("arn:aws:es:us-east-1:123456789012:domain/logs"),
				DomainName: new("logs"), EngineVersion: new("OpenSearch_2.13"),
				VPCOptions: &opensearchtypes.VPCDerivedInfo{
					VPCId: new(fixtureVPC), SubnetIds: []string{fixtureSubnet},
					SecurityGroupIds: []string{fixtureSG},
				},
				EncryptionAtRestOptions: &opensearchtypes.EncryptionAtRestOptions{KmsKeyId: new(fixtureKMSKeyARN)},
			}}, nil
		},
	}
}

func fixtureEFS() *fakeEfs {
	return &fakeEfs{
		describeFileSystems: func(*efs.DescribeFileSystemsInput) (*efs.DescribeFileSystemsOutput, error) {
			return &efs.DescribeFileSystemsOutput{FileSystems: []efstypes.FileSystemDescription{{
				FileSystemArn: new("arn:aws:elasticfilesystem:us-east-1:123456789012:file-system/fs-0aaa"),
				FileSystemId:  new("fs-0aaa"), Name: new("shared"),
				LifeCycleState:  efstypes.LifeCycleStateAvailable,
				PerformanceMode: efstypes.PerformanceModeGeneralPurpose,
				Encrypted:       new(true), KmsKeyId: new(fixtureKMSKeyARN),
			}}}, nil
		},
	}
}

func fixtureKMS() *fakeKms {
	return &fakeKms{
		listKeys: func(*kms.ListKeysInput) (*kms.ListKeysOutput, error) {
			return &kms.ListKeysOutput{Keys: []kmstypes.KeyListEntry{{
				KeyId: new("1111-2222"), KeyArn: new(fixtureKMSKeyARN),
			}}}, nil
		},
		describeKey: func(*kms.DescribeKeyInput) (*kms.DescribeKeyOutput, error) {
			return &kms.DescribeKeyOutput{KeyMetadata: &kmstypes.KeyMetadata{
				Arn: new(fixtureKMSKeyARN), KeyId: new("1111-2222"),
				Description: new("app data key"), KeyManager: kmstypes.KeyManagerTypeCustomer,
				KeyState: kmstypes.KeyStateEnabled, KeyUsage: kmstypes.KeyUsageTypeEncryptDecrypt,
			}}, nil
		},
	}
}
