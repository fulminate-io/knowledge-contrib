// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"
	"strings"

	bq "google.golang.org/api/bigquery/v2"
	firestore "google.golang.org/api/firestore/v1"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// analytics.go — the two services whose resources nest: an analytics dataset
// holds tables, and a document database is protected by backup schedules. Both
// are assembled by the production wiring, because the child is a second API call
// per parent and a converter that made one would not be a pure function.

// BigQueryDataset is one analytics dataset with the tables inside it, assembled
// by the production wiring because tables are a second call per dataset.
type BigQueryDataset struct {
	ProjectID  string
	DatasetID  string
	Location   string
	Labels     map[string]string
	KMSKeyName string
	Tables     []*bq.TableListTables
}

// BigQueryDatasets enumerates the project's analytics datasets and tables.
func BigQueryDatasets(list Lister[BigQueryDataset]) Subcollector {
	return New("gcp-bigquery-datasets", list, convertBigQueryDataset)
}

func convertBigQueryDataset(_ string, dataset BigQueryDataset) (gcpgraph.Result, error) {
	if dataset.ProjectID == "" || dataset.DatasetID == "" {
		return gcpgraph.Result{}, fmt.Errorf(
			"a dataset is incomplete: project=%q dataset=%q", dataset.ProjectID, dataset.DatasetID)
	}
	id := "projects/" + dataset.ProjectID + "/datasets/" + dataset.DatasetID
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: dataset.DatasetID, SelfLink: id, Location: dataset.Location,
		Labels: dataset.Labels,
		Fields: nonEmptyFields(map[string]string{"kmsKeyName": dataset.KMSKeyName}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"table_count": strconv.Itoa(len(dataset.Tables))}
	setIfNotEmpty(metadata, "location", dataset.Location)
	for k, v := range dataset.Labels {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: dataset.DatasetID, ResourceType: gcpgraph.ResourceTypeBigQueryDataset,
		Region: dataset.Location, Content: raw, Metadata: metadata,
	}}}
	if dataset.KMSKeyName != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: dataset.KMSKeyName, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	for _, table := range dataset.Tables {
		if table == nil || table.TableReference == nil || table.TableReference.TableId == "" {
			continue
		}
		tableID := id + "/tables/" + table.TableReference.TableId
		tableRaw, err := gcpcontent.Marshal(gcpcontent.Generic{
			Name: table.TableReference.TableId, SelfLink: tableID, Location: dataset.Location,
			Labels: table.Labels,
			Fields: nonEmptyFields(map[string]string{"type": table.Type}),
		})
		if err != nil {
			return gcpgraph.Result{}, err
		}
		tableMeta := map[string]string{}
		setIfNotEmpty(tableMeta, "table_type", table.Type)
		for k, v := range table.Labels {
			tableMeta["label/"+k] = v
		}
		out.Resources = append(out.Resources, gcpgraph.Resource{
			ID: tableID, Name: table.TableReference.TableId,
			ResourceType: gcpgraph.ResourceTypeBigQueryTable,
			Region:       dataset.Location, Content: tableRaw, Metadata: tableMeta,
		})
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: tableID, Type: gcpgraph.EdgeContains,
		})
	}
	return out, nil
}

// FirestoreDatabase is one document database with its backup schedules.
type FirestoreDatabase struct {
	Database  *firestore.GoogleFirestoreAdminV1Database
	Schedules []*firestore.GoogleFirestoreAdminV1BackupSchedule
}

// FirestoreDatabases enumerates the project's document databases and the backup
// schedules protecting them.
func FirestoreDatabases(list Lister[FirestoreDatabase]) Subcollector {
	return New("gcp-firestore-databases", list, convertFirestoreDatabase)
}

func convertFirestoreDatabase(_ string, db FirestoreDatabase) (gcpgraph.Result, error) {
	if db.Database == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil database")
	}
	id := db.Database.Name
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a database with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Location: db.Database.LocationId,
		CreateTime: db.Database.CreateTime,
		Fields: nonEmptyFields(map[string]string{
			"type":             db.Database.Type,
			"concurrencyMode":  db.Database.ConcurrencyMode,
			"keyPrefix":        db.Database.KeyPrefix,
			"cmekKeyName":      cmekKeyName(db.Database),
			"deleteProtection": db.Database.DeleteProtectionState,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"backup_schedule_count": strconv.Itoa(len(db.Schedules))}
	setIfNotEmpty(metadata, "database_type", db.Database.Type)
	setIfNotEmpty(metadata, "location", db.Database.LocationId)

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeFirestoreDatabase,
		Region: db.Database.LocationId, Content: raw, Metadata: metadata,
	}}}
	if key := cmekKeyName(db.Database); key != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: key, Type: gcpgraph.EdgeEncryptsWith,
		})
	}
	for _, schedule := range db.Schedules {
		if schedule == nil || schedule.Name == "" {
			continue
		}
		scheduleRaw, err := gcpcontent.Marshal(gcpcontent.Generic{
			Name: gcpgraph.LastSegment(schedule.Name), SelfLink: schedule.Name,
			CreateTime: schedule.CreateTime,
			Fields:     nonEmptyFields(map[string]string{"retention": schedule.Retention}),
		})
		if err != nil {
			return gcpgraph.Result{}, err
		}
		out.Resources = append(out.Resources, gcpgraph.Resource{
			ID: schedule.Name, Name: gcpgraph.LastSegment(schedule.Name),
			ResourceType: gcpgraph.ResourceTypeFirestoreBackupSchedule,
			Content:      scheduleRaw,
			Metadata:     nonEmptyFields(map[string]string{"retention": schedule.Retention}),
		})
		// The edge runs FROM the database: a database is backed up by a
		// schedule, which is the direction a reader asks the question in.
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: schedule.Name, Type: gcpgraph.EdgeBackedUpBy,
		})
	}
	return out, nil
}

func cmekKeyName(db *firestore.GoogleFirestoreAdminV1Database) string {
	if db.CmekConfig == nil {
		return ""
	}
	return strings.TrimSpace(db.CmekConfig.KmsKeyName)
}
