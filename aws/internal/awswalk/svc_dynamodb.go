// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// svc_dynamodb.go — tables and the two shapes of backup they carry.
//
// THREE RESOURCE TYPES COME OUT OF ONE SERVICE, and the reason is that two of
// them are not API objects. A table is; an on-demand backup is; but
// POINT-IN-TIME RECOVERY is a table SETTING with no identity of its own. It gets
// a node anyway, with a composed id, because "which tables can be restored to a
// point in time" is a question the graph should answer by traversal rather than
// by re-reading every table's content blob.

func walkDynamoDB(ctx context.Context, w *walkContext) error {
	if err := w.walkDynamoTables(ctx); err != nil {
		return err
	}
	return w.walkDynamoBackups(ctx)
}

func (w *walkContext) walkDynamoTables(ctx context.Context) error {
	// DYNAMODB PAGES BY LAST EVALUATED NAME rather than by an opaque token: the
	// next page's request carries the last name seen. The adapter is the same
	// shape as IAM's marker, and getting it wrong reads one page of a hundred
	// tables.
	return paginate(ctx,
		func(ctx context.Context, token *string) (*dynamodb.ListTablesOutput, error) {
			return w.clients.DynamoDB.ListTables(ctx, &dynamodb.ListTablesInput{ExclusiveStartTableName: token})
		},
		func(p *dynamodb.ListTablesOutput) *string { return p.LastEvaluatedTableName },
		func(p *dynamodb.ListTablesOutput) error {
			for _, name := range p.TableNames {
				if err := w.describeDynamoTable(ctx, name); err != nil {
					return err
				}
			}
			return nil
		})
}

func (w *walkContext) describeDynamoTable(ctx context.Context, name string) error {
	out, err := w.clients.DynamoDB.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: &name})
	if err != nil {
		return fmt.Errorf("describe table %s: %w", name, err)
	}
	t := out.Table
	if t == nil {
		return nil
	}
	arn := deref(t.TableArn)
	if arn == "" {
		return nil
	}
	detail := map[string]string{}
	put(detail, "status", string(t.TableStatus))
	if t.ItemCount != nil {
		detail["item_count"] = fmt.Sprintf("%d", *t.ItemCount)
	}
	w.sink.addNode(newNode(resource{
		id:           arn,
		resourceType: ResourceTypeDynamoDBTable,
		name:         name,
		summary:      fmt.Sprintf("DynamoDB table %s in %s", name, w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))

	if t.SSEDescription != nil {
		if key := deref(t.SSEDescription.KMSMasterKeyArn); key != "" {
			w.sink.addEdge(arn, key, EdgeEncryptsWith, map[string]string{"via": "table.sse_description"})
		}
	}
	w.addDynamoPITR(ctx, arn, name)
	return nil
}

// addDynamoPITR emits the point-in-time-recovery node and its BACKED_UP_BY edge
// when the table has it enabled.
//
// THE FAILURE DOES NOT PROPAGATE AND IS NOT SWALLOWED: one table's
// continuous-backup setting going unread costs that table's PITR node, so the
// read records into the account's failure set and the account asserts INCOMPLETE
// naming this operation. Failing the whole DynamoDB walk would cost every other
// table. NOTHING IS EMITTED WHEN PITR IS DISABLED — a node asserting a recovery
// capability the table does not have would be worse than its absence.
//
// A DISABLED SETTING IS NOT A FAILURE. The distinction matters here more than at
// most reads: "the table has no point-in-time recovery" is an answer, and
// recording it as a failed read would mark every account with an unprotected
// table incomplete forever.
func (w *walkContext) addDynamoPITR(ctx context.Context, tableARN, tableName string) {
	out, err := w.clients.DynamoDB.DescribeContinuousBackups(ctx,
		&dynamodb.DescribeContinuousBackupsInput{TableName: &tableName})
	if err != nil {
		w.recordSubreadFailure("dynamodb.DescribeContinuousBackups", tableName, err)
		return
	}
	if out.ContinuousBackupsDescription == nil {
		w.recordSubreadFailure("dynamodb.DescribeContinuousBackups", tableName,
			errors.New("the call succeeded and carried no continuous-backups description"))
		return
	}
	pitr := out.ContinuousBackupsDescription.PointInTimeRecoveryDescription
	if pitr == nil || pitr.PointInTimeRecoveryStatus != "ENABLED" {
		return
	}
	id := dynamoPITRID(tableName)
	detail := map[string]string{"table": tableName}
	if pitr.EarliestRestorableDateTime != nil {
		// THE EARLIEST RESTORABLE TIME IS DELIBERATELY NOT CARRIED. It moves with
		// the wall clock, so a node holding it would differ between two collects
		// of an unchanged table and the carry-forward diff would write a row for
		// it on every collect.
		detail["has_restore_window"] = "true"
	}
	w.sink.addNode(newNode(resource{
		id:           id,
		resourceType: ResourceTypeDynamoDBPITR,
		name:         tableName + " point-in-time recovery",
		summary:      fmt.Sprintf("Point-in-time recovery enabled for DynamoDB table %s in %s", tableName, w.region),
		detail:       detail,
		region:       w.region,
	}, w.account))
	w.sink.addEdge(tableARN, id, EdgeBackedUpBy, map[string]string{"kind": "point-in-time-recovery"})
}

func (w *walkContext) walkDynamoBackups(ctx context.Context) error {
	return paginate(ctx,
		func(ctx context.Context, token *string) (*dynamodb.ListBackupsOutput, error) {
			return w.clients.DynamoDB.ListBackups(ctx, &dynamodb.ListBackupsInput{ExclusiveStartBackupArn: token})
		},
		func(p *dynamodb.ListBackupsOutput) *string { return p.LastEvaluatedBackupArn },
		func(p *dynamodb.ListBackupsOutput) error {
			for _, b := range p.BackupSummaries {
				arn := deref(b.BackupArn)
				if arn == "" {
					continue
				}
				detail := map[string]string{}
				put(detail, "table_name", deref(b.TableName))
				put(detail, "status", string(b.BackupStatus))
				put(detail, "backup_type", string(b.BackupType))
				w.sink.addNode(newNode(resource{
					id:           arn,
					resourceType: ResourceTypeDynamoDBBackup,
					name:         deref(b.BackupName),
					summary: fmt.Sprintf("DynamoDB backup %s of table %s in %s",
						deref(b.BackupName), deref(b.TableName), w.region),
					detail: detail,
					region: w.region,
				}, w.account))
				if table := deref(b.TableArn); table != "" {
					w.sink.addEdge(table, arn, EdgeBackedUpBy, map[string]string{"kind": "on-demand-backup"})
				}
			}
			return nil
		})
}
