// SPDX-License-Identifier: Apache-2.0

package gcpclients

import (
	"context"
	"fmt"

	bq "google.golang.org/api/bigquery/v2"
	cloudidentity "google.golang.org/api/cloudidentity/v1"
	cloudscheduler "google.golang.org/api/cloudscheduler/v1"
	cloudtasks "google.golang.org/api/cloudtasks/v2"
	dataflow "google.golang.org/api/dataflow/v1b3"
	dns "google.golang.org/api/dns/v1"
	sqladmin "google.golang.org/api/sqladmin/v1beta4"
	vpcaccess "google.golang.org/api/vpcaccess/v1"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
)

// rest_listers.go — the enumerations whose service speaks HTTP rather than gRPC.
//
// THEY PAGE BY CALLBACK. The generated clients hand each page to a function
// rather than returning an iterator, so each one accumulates into a slice and
// returns what it collected; the shape is different from the gRPC arm and the
// contract with the converter is identical.

func (c *clients) restSubcollectors() []collect.Subcollector {
	return []collect.Subcollector{
		collect.SQLInstances(func(ctx context.Context, project string) ([]*sqladmin.DatabaseInstance, error) {
			var out []*sqladmin.DatabaseInstance
			var unread unread
			err := c.sqladmin.Instances.List(project).Pages(ctx,
				func(page *sqladmin.InstancesListResponse) error {
					out = append(out, page.Items...)
					unread.add(sqlUnreadRegions(page))
					return nil
				})
			return out, unread.join(err)
		}),
		collect.VPCConnectors(func(ctx context.Context, project string) ([]*vpcaccess.Connector, error) {
			parent := fmt.Sprintf("projects/%s/locations/%s", project, allLocations)
			var out []*vpcaccess.Connector
			err := c.vpcaccess.Projects.Locations.Connectors.List(parent).Pages(ctx,
				func(page *vpcaccess.ListConnectorsResponse) error {
					out = append(out, page.Connectors...)
					return nil
				})
			return out, err
		}),
		collect.TaskQueues(func(ctx context.Context, project string) ([]*cloudtasks.Queue, error) {
			parent := fmt.Sprintf("projects/%s/locations/%s", project, allLocations)
			var out []*cloudtasks.Queue
			err := c.tasks.Projects.Locations.Queues.List(parent).Pages(ctx,
				func(page *cloudtasks.ListQueuesResponse) error {
					out = append(out, page.Queues...)
					return nil
				})
			return out, err
		}),
		collect.ScheduledJobs(func(ctx context.Context, project string) ([]*cloudscheduler.Job, error) {
			parent := fmt.Sprintf("projects/%s/locations/%s", project, allLocations)
			var out []*cloudscheduler.Job
			err := c.scheduler.Projects.Locations.Jobs.List(parent).Pages(ctx,
				func(page *cloudscheduler.ListJobsResponse) error {
					out = append(out, page.Jobs...)
					return nil
				})
			return out, err
		}),
		collect.DataflowJobs(func(ctx context.Context, project string) ([]*dataflow.Job, error) {
			var out []*dataflow.Job
			var unread unread
			err := c.dataflow.Projects.Jobs.Aggregated(project).Pages(ctx,
				func(page *dataflow.ListJobsResponse) error {
					out = append(out, page.Jobs...)
					unread.add(dataflowUnreadLocations(page))
					return nil
				})
			return out, unread.join(err)
		}),
		collect.DNSZones(c.listDNSZones),
		collect.BigQueryDatasets(c.listBigQueryDatasets),
		collect.FirestoreDatabases(c.listFirestoreDatabases),
	}
}

// listDNSZones reads each zone's record sets, which is a second call per zone.
func (c *clients) listDNSZones(ctx context.Context, project string) ([]collect.DNSZone, error) {
	var zones []*dns.ManagedZone
	if err := c.dns.ManagedZones.List(project).Pages(ctx,
		func(page *dns.ManagedZonesListResponse) error {
			zones = append(zones, page.ManagedZones...)
			return nil
		}); err != nil {
		return nil, err
	}

	out := make([]collect.DNSZone, 0, len(zones))
	for _, zone := range zones {
		entry := collect.DNSZone{Zone: zone}
		// A zone whose records cannot be read is still a zone. Losing its
		// records costs the routing derivation, and that is reported by the
		// enumeration's own failure rather than silently.
		if err := c.dns.ResourceRecordSets.List(project, zone.Name).Pages(ctx,
			func(page *dns.ResourceRecordSetsListResponse) error {
				entry.Records = append(entry.Records, page.Rrsets...)
				return nil
			}); err != nil {
			return out, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// listBigQueryDatasets reads each dataset's tables and its encryption
// configuration, both of which are per-dataset calls.
func (c *clients) listBigQueryDatasets(ctx context.Context, project string) ([]collect.BigQueryDataset, error) {
	var (
		listed []*bq.DatasetListDatasets
		unread unread
	)
	if err := c.bigquery.Datasets.List(project).Pages(ctx,
		func(page *bq.DatasetList) error {
			listed = append(listed, page.Datasets...)
			unread.add(page.Unreachable)
			return nil
		}); err != nil {
		return nil, unread.join(err)
	}

	out := make([]collect.BigQueryDataset, 0, len(listed))
	for _, entry := range listed {
		if entry == nil || entry.DatasetReference == nil {
			continue
		}
		datasetID := entry.DatasetReference.DatasetId
		dataset := collect.BigQueryDataset{
			ProjectID: project, DatasetID: datasetID,
			Location: entry.Location, Labels: entry.Labels,
		}
		// THE DETAIL READ IS NOT BEST-EFFORT. What it carries is the dataset's
		// encryption relationship, and a dataset emitted without one is
		// indistinguishable from a dataset that has none — the same reason the
		// topic and subscription config reads state for themselves. A failure
		// here is returned, classified by the caller, rather than dropping the
		// edge while the walk still claims a complete read.
		detail, err := c.bigquery.Datasets.Get(project, datasetID).Context(ctx).Do()
		if err != nil {
			return out, fmt.Errorf("reading the detail of dataset %s: %w", datasetID, err)
		}
		if detail.DefaultEncryptionConfiguration != nil {
			dataset.KMSKeyName = detail.DefaultEncryptionConfiguration.KmsKeyName
		}
		if detail.Location != "" {
			dataset.Location = detail.Location
		}
		if err := c.bigquery.Tables.List(project, datasetID).Pages(ctx,
			func(page *bq.TableList) error {
				dataset.Tables = append(dataset.Tables, page.Tables...)
				return nil
			}); err != nil {
			return out, err
		}
		out = append(out, dataset)
	}
	return out, unread.err()
}

// listFirestoreDatabases reads each database's backup schedules.
func (c *clients) listFirestoreDatabases(ctx context.Context, project string) ([]collect.FirestoreDatabase, error) {
	resp, err := c.firestore.Projects.Databases.List("projects/" + project).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	// The list names the locations it could not reach on its own field rather
	// than as an error.
	unreadLocations := partialRead(resp.Unreachable)
	out := make([]collect.FirestoreDatabase, 0, len(resp.Databases))
	for _, db := range resp.Databases {
		if db == nil || db.Name == "" {
			continue
		}
		entry := collect.FirestoreDatabase{Database: db}
		schedules, err := c.firestore.Projects.Databases.BackupSchedules.
			List(db.Name).Context(ctx).Do()
		if err != nil {
			return out, err
		}
		entry.Schedules = schedules.BackupSchedules
		out = append(out, entry)
	}
	return out, unreadLocations
}

// listIdentityGroups reads the directory groups this project's policies can name
// and the memberships of each, which is a second call per group.
//
// IT SEARCHES BY THE PROJECT'S OWN CUSTOMER. The groups API is a directory API
// rather than a project one, so there is no "this project's groups": the query
// below asks for every group in the customer the caller belongs to, which is
// what the IAM bindings can reference.
func (c *clients) listIdentityGroups(ctx context.Context, _ string) ([]collect.IdentityGroup, error) {
	var groups []*cloudidentity.Group
	err := c.cloudIdentity.Groups.List().
		Parent("customers/my_customer").
		View("FULL").
		Pages(ctx, func(page *cloudidentity.ListGroupsResponse) error {
			groups = append(groups, page.Groups...)
			return nil
		})
	if err != nil {
		return nil, err
	}

	out := make([]collect.IdentityGroup, 0, len(groups))
	for _, group := range groups {
		if group == nil || group.Name == "" {
			continue
		}
		entry := collect.IdentityGroup{Group: group}
		if err := c.cloudIdentity.Groups.Memberships.List(group.Name).
			Pages(ctx, func(page *cloudidentity.ListMembershipsResponse) error {
				entry.Members = append(entry.Members, page.Memberships...)
				return nil
			}); err != nil {
			return out, err
		}
		out = append(out, entry)
	}
	return out, nil
}

// sqlUnreadRegions reads the regions this service could not read.
//
// It reports them as a WARNING on the page rather than as an error, so a caller
// reading only the items sees a short list as a complete one.
func sqlUnreadRegions(page *sqladmin.InstancesListResponse) []string {
	var out []string
	for _, warning := range page.Warnings {
		if warning != nil && warning.Region != "" {
			out = append(out, warning.Region)
		}
	}
	return out
}

// dataflowUnreadLocations reads the locations this service could not read, which
// it reports on its own page field rather than as an error.
func dataflowUnreadLocations(page *dataflow.ListJobsResponse) []string {
	var out []string
	for _, failed := range page.FailedLocation {
		if failed != nil && failed.Name != "" {
			out = append(out, failed.Name)
		}
	}
	return out
}
