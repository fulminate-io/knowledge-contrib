// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"

	artifactregistrypb "cloud.google.com/go/artifactregistry/apiv1/artifactregistrypb"
	cloudscheduler "google.golang.org/api/cloudscheduler/v1"
	cloudtasks "google.golang.org/api/cloudtasks/v2"
	dns "google.golang.org/api/dns/v1"
	vpcaccess "google.golang.org/api/vpcaccess/v1"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// services.go — the remaining managed services: container repositories, serverless
// VPC connectors, DNS, task queues, scheduled jobs, analytics datasets and
// document databases.

// ArtifactRepositories enumerates the project's container and package
// repositories, and the UPSTREAM a remote repository proxies.
func ArtifactRepositories(list Lister[*artifactregistrypb.Repository]) Subcollector {
	return New("gcp-artifact-repositories", list, convertArtifactRepository)
}

func convertArtifactRepository(_ string, repo *artifactregistrypb.Repository) (gcpgraph.Result, error) {
	if repo == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil repository")
	}
	id := repo.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a repository with no resource name")
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Description: repo.GetDescription(),
		Location: locationOfResourceName(id), Labels: repo.GetLabels(),
		Fields: nonEmptyFields(map[string]string{
			"format":      repo.GetFormat().String(),
			"mode":        repo.GetMode().String(),
			"kmsKeyName":  repo.GetKmsKeyName(),
			"registryUri": repo.GetRegistryUri(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"size_bytes": strconv.FormatInt(repo.GetSizeBytes(), 10)}
	setIfNotEmpty(metadata, "format", repo.GetFormat().String())
	setIfNotEmpty(metadata, "mode", repo.GetMode().String())
	setIfNotEmpty(metadata, "location", locationOfResourceName(id))
	for k, v := range repo.GetLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id),
		ResourceType: gcpgraph.ResourceTypeArtifactRegistryRepo,
		Region:       locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	if key := repo.GetKmsKeyName(); key != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: key, Type: gcpgraph.EdgeEncryptsWith,
		})
	}

	// A REMOTE repository is a cache in front of somebody else's registry. The
	// upstream is not a resource in this project — it is a public endpoint or
	// another organization's registry — so it gets a node of its own kind that
	// says so, rather than a dangling reference or nothing at all.
	if upstream := remoteUpstream(repo); upstream != "" {
		upstreamID := "gcp:ar-remote:" + upstream
		out.Resources = append(out.Resources, gcpgraph.Resource{
			ID: upstreamID, Name: upstream, ResourceType: gcpgraph.ResourceTypeARRemote,
			Summary: gcpgraph.ResourceTypeARRemote + " " + upstream,
			Metadata: map[string]string{
				"collected":        "false",
				"collected_reason": "upstream registry outside this project",
				"upstream":         upstream,
			},
		})
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: upstreamID, Type: gcpgraph.EdgeProxiesFrom,
		})
	}
	return out, nil
}

// remoteUpstream reads the upstream a remote repository proxies. The API models
// it as a per-format oneof, so each supported format is read explicitly; a
// format this collector does not know yields empty rather than a guess.
func remoteUpstream(repo *artifactregistrypb.Repository) string {
	config := repo.GetRemoteRepositoryConfig()
	if config == nil {
		return ""
	}
	switch {
	case config.GetDockerRepository() != nil:
		if custom := config.GetDockerRepository().GetCustomRepository().GetUri(); custom != "" {
			return custom
		}
		return config.GetDockerRepository().GetPublicRepository().String()
	case config.GetMavenRepository() != nil:
		if custom := config.GetMavenRepository().GetCustomRepository().GetUri(); custom != "" {
			return custom
		}
		return config.GetMavenRepository().GetPublicRepository().String()
	case config.GetNpmRepository() != nil:
		if custom := config.GetNpmRepository().GetCustomRepository().GetUri(); custom != "" {
			return custom
		}
		return config.GetNpmRepository().GetPublicRepository().String()
	case config.GetPythonRepository() != nil:
		if custom := config.GetPythonRepository().GetCustomRepository().GetUri(); custom != "" {
			return custom
		}
		return config.GetPythonRepository().GetPublicRepository().String()
	default:
		return ""
	}
}

// VPCConnectors enumerates the project's serverless VPC access connectors.
func VPCConnectors(list Lister[*vpcaccess.Connector]) Subcollector {
	return New("gcp-vpc-connectors", list, convertVPCConnector)
}

func convertVPCConnector(projectID string, connector *vpcaccess.Connector) (gcpgraph.Result, error) {
	if connector == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil VPC connector")
	}
	id := connector.Name
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a VPC connector with no resource name")
	}
	subnet := ""
	if connector.Subnet != nil {
		subnet = connector.Subnet.Name
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, State: connector.State,
		Location: locationOfResourceName(id),
		Fields: nonEmptyFields(map[string]string{
			"network":     connector.Network,
			"subnet":      subnet,
			"ipCidrRange": connector.IpCidrRange,
			"machineType": connector.MachineType,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", connector.State)
	setIfNotEmpty(metadata, "ip_cidr_range", connector.IpCidrRange)
	setIfNotEmpty(metadata, "machine_type", connector.MachineType)

	region := locationOfResourceName(id)
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id),
		ResourceType: gcpgraph.ResourceTypeVPCAccessConnector,
		Region:       region, Content: raw, Metadata: metadata,
	}}}
	// The API reports the network and subnet as NAMES; they are rebuilt into the
	// self-link the compute enumeration uses as its node id, or the edges could
	// never join.
	if connector.Network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: globalComputeSelfLink(projectID, "networks", connector.Network),
			Type: gcpgraph.EdgeUsesNetwork,
		})
	}
	if subnet != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: regionalComputeSelfLink(projectID, region, "subnetworks", subnet),
			Type: gcpgraph.EdgeUsesSubnet,
		})
	}
	return out, nil
}

// DNSZone is one managed zone together with the record sets inside it. The
// records are a second API call per zone, so the production wiring assembles
// them and the converter stays pure.
type DNSZone struct {
	Zone    *dns.ManagedZone
	Records []*dns.ResourceRecordSet
}

// DNSZones enumerates the project's DNS zones and their records.
//
// IT EMITS NO ROUTING EDGE. A record's data is an address, not a node id, and an
// edge naming an address is a dangling edge. The resolver that runs after the
// walk joins those addresses onto the resources holding them, so a routing edge
// enters the graph already resolved or not at all.
func DNSZones(list Lister[DNSZone]) Subcollector {
	return New("gcp-dns-zones", list, convertDNSZone)
}

func convertDNSZone(projectID string, zone DNSZone) (gcpgraph.Result, error) {
	if zone.Zone == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil managed zone")
	}
	if zone.Zone.Name == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a managed zone with no name")
	}
	zoneID := "projects/" + projectID + "/managedZones/" + zone.Zone.Name
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: zone.Zone.Name, SelfLink: zoneID, Description: zone.Zone.Description,
		CreateTime: zone.Zone.CreationTime, Labels: zone.Zone.Labels,
		Fields: nonEmptyFields(map[string]string{
			"dnsName":    zone.Zone.DnsName,
			"visibility": zone.Zone.Visibility,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"record_count": strconv.Itoa(len(zone.Records))}
	setIfNotEmpty(metadata, "dns_name", zone.Zone.DnsName)
	setIfNotEmpty(metadata, "visibility", zone.Zone.Visibility)
	for k, v := range zone.Zone.Labels {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: zoneID, Name: zone.Zone.Name, ResourceType: gcpgraph.ResourceTypeDNSManagedZone,
		Content: raw, Metadata: metadata,
	}}}
	// A private zone is visible on named networks, which is a real dependency.
	if zone.Zone.PrivateVisibilityConfig != nil {
		for _, network := range zone.Zone.PrivateVisibilityConfig.Networks {
			if network != nil && network.NetworkUrl != "" {
				out.Relations = append(out.Relations, gcpgraph.Relation{
					From: zoneID, To: network.NetworkUrl, Type: gcpgraph.EdgeUsesNetwork,
				})
			}
		}
	}

	for _, record := range zone.Records {
		if record == nil || record.Name == "" || record.Type == "" {
			continue
		}
		// The record's own id carries the zone, the name and the type, because a
		// zone holds one record set per (name, type) pair and nothing narrower.
		recordID := zoneID + "/rrsets/" + record.Name + "/" + record.Type
		content := gcpcontent.RecordSet{
			Name: record.Name, Type: record.Type, TTL: record.Ttl,
			Rrdatas: record.Rrdatas, Zone: zone.Zone.Name,
		}
		recordRaw, err := gcpcontent.Marshal(content)
		if err != nil {
			return gcpgraph.Result{}, err
		}
		out.Resources = append(out.Resources, gcpgraph.Resource{
			ID: recordID, Name: record.Name, ResourceType: gcpgraph.ResourceTypeDNSRecordSet,
			Content: recordRaw,
			Metadata: map[string]string{
				"record_type": record.Type,
				"ttl":         strconv.FormatInt(record.Ttl, 10),
				"zone":        zone.Zone.Name,
			},
		})
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: zoneID, To: recordID, Type: gcpgraph.EdgeContains,
		})
	}
	return out, nil
}

// TaskQueues enumerates the project's task queues.
func TaskQueues(list Lister[*cloudtasks.Queue]) Subcollector {
	return New("gcp-task-queues", list, convertTaskQueue)
}

func convertTaskQueue(_ string, queue *cloudtasks.Queue) (gcpgraph.Result, error) {
	if queue == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil task queue")
	}
	id := queue.Name
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a task queue with no resource name")
	}
	fields := map[string]string{}
	if queue.RateLimits != nil {
		fields["maxDispatchesPerSecond"] = strconv.FormatFloat(
			queue.RateLimits.MaxDispatchesPerSecond, 'f', -1, 64)
	}
	if queue.RetryConfig != nil {
		fields["maxAttempts"] = strconv.FormatInt(queue.RetryConfig.MaxAttempts, 10)
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, State: queue.State,
		Location: locationOfResourceName(id), Fields: nonEmptyFields(fields),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", queue.State)
	setIfNotEmpty(metadata, "location", locationOfResourceName(id))

	return gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeCloudTasksQueue,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}, nil
}

// ScheduledJobs enumerates the project's scheduled jobs.
func ScheduledJobs(list Lister[*cloudscheduler.Job]) Subcollector {
	return New("gcp-scheduled-jobs", list, convertScheduledJob)
}

func convertScheduledJob(projectID string, job *cloudscheduler.Job) (gcpgraph.Result, error) {
	if job == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil scheduled job")
	}
	id := job.Name
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a scheduled job with no resource name")
	}
	var target, serviceAccount string
	switch {
	case job.PubsubTarget != nil:
		target = job.PubsubTarget.TopicName
	case job.HttpTarget != nil:
		target = job.HttpTarget.Uri
		if job.HttpTarget.OidcToken != nil {
			serviceAccount = job.HttpTarget.OidcToken.ServiceAccountEmail
		}
		if job.HttpTarget.OauthToken != nil && serviceAccount == "" {
			serviceAccount = job.HttpTarget.OauthToken.ServiceAccountEmail
		}
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: id, Description: job.Description,
		State: job.State, Location: locationOfResourceName(id),
		Fields: nonEmptyFields(map[string]string{
			"schedule":       job.Schedule,
			"timeZone":       job.TimeZone,
			"target":         target,
			"serviceAccount": serviceAccount,
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", job.State)
	setIfNotEmpty(metadata, "schedule", job.Schedule)

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeSchedulerJob,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	if target != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: target, Type: gcpgraph.EdgeTargets,
		})
	}
	if serviceAccount != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ServiceAccountResourceName(projectID, serviceAccount),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	return out, nil
}
