// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"
	"strings"

	containerpb "cloud.google.com/go/container/apiv1/containerpb"
	functionspb "cloud.google.com/go/functions/apiv2/functionspb"
	runpb "cloud.google.com/go/run/apiv2/runpb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// compute_platform.go — the three places code runs: Kubernetes clusters, Cloud
// Run services and Cloud Functions.
//
// A CLUSTER IS A NODE HERE AND NOTHING MORE. Its workloads live in a Kubernetes
// graph, which is a different collector against a different source; a cluster
// collected here does not reach inside itself. The relationships that DO belong
// here are the ones visible from this project: the network it sits on and the
// identity federation it enables.

// GKEClusters enumerates the project's Kubernetes clusters.
func GKEClusters(list Lister[*containerpb.Cluster]) Subcollector {
	return New("gcp-gke-clusters", list, convertGKECluster)
}

func convertGKECluster(projectID string, cluster *containerpb.Cluster) (gcpgraph.Result, error) {
	if cluster == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil cluster")
	}
	if cluster.GetName() == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a cluster with no name")
	}
	location := cluster.GetLocation()
	// The cluster's own self-link is a URL of a different shape from compute's;
	// the RELATIVE RESOURCE NAME is the id, which is what every other
	// non-compute resource in this graph uses and what a Kubernetes-side
	// reference can be built from.
	id := fmt.Sprintf("projects/%s/locations/%s/clusters/%s", projectID, location, cluster.GetName())

	fields := nonEmptyFields(map[string]string{
		"network":        cluster.GetNetwork(),
		"subnetwork":     cluster.GetSubnetwork(),
		"endpoint":       cluster.GetEndpoint(),
		"workloadPool":   cluster.GetWorkloadIdentityConfig().GetWorkloadPool(),
		"releaseChannel": cluster.GetReleaseChannel().GetChannel().String(),
	})
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: cluster.GetName(), SelfLink: cluster.GetSelfLink(),
		Description: cluster.GetDescription(), State: cluster.GetStatus().String(),
		Location: location, CreateTime: cluster.GetCreateTime(),
		Labels: cluster.GetResourceLabels(), Fields: fields,
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{
		"node_pool_count": strconv.Itoa(len(cluster.GetNodePools())),
		"node_count":      strconv.Itoa(int(cluster.GetCurrentNodeCount())),
	}
	setIfNotEmpty(metadata, "status", cluster.GetStatus().String())
	setIfNotEmpty(metadata, "cluster_version", cluster.GetCurrentMasterVersion())
	setIfNotEmpty(metadata, "location", location)
	for k, v := range cluster.GetResourceLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: cluster.GetName(), ResourceType: gcpgraph.ResourceTypeGKECluster,
		Region: location, Content: raw, Metadata: metadata,
	}}}
	// The network and subnetwork the API reports are NAMES, not self-links, so
	// they are rebuilt into the self-link form the compute enumeration uses as
	// its node id. Emitting the bare name would produce an edge that can never
	// join, which is worse than no edge.
	if network := cluster.GetNetwork(); network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: globalComputeSelfLink(projectID, "networks", network),
			Type: gcpgraph.EdgeUsesNetwork,
		})
	}
	if subnet := cluster.GetSubnetwork(); subnet != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: regionalComputeSelfLink(projectID, regionOf(location), "subnetworks", subnet),
			Type: gcpgraph.EdgeUsesSubnet,
		})
	}
	// Workload identity is the federation that lets a Kubernetes identity act as
	// a Google one. The pool is the project's, so the edge names the project.
	if pool := cluster.GetWorkloadIdentityConfig().GetWorkloadPool(); pool != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ProjectResourceName(projectID),
			Type:     gcpgraph.EdgeWorkloadIdentity,
			Metadata: map[string]string{"workload_pool": pool},
		})
	}
	// The node pools' own service account is the identity the cluster's nodes
	// run as, which is what makes a cluster reachable from an identity question.
	seen := map[string]bool{}
	for _, pool := range cluster.GetNodePools() {
		email := pool.GetConfig().GetServiceAccount()
		if email == "" || email == "default" || seen[email] {
			continue
		}
		seen[email] = true
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ServiceAccountResourceName(projectID, email),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	return out, nil
}

// RunServices enumerates the project's Cloud Run services.
func RunServices(list Lister[*runpb.Service]) Subcollector {
	return New("gcp-run-services", list, convertRunService)
}

func convertRunService(projectID string, svc *runpb.Service) (gcpgraph.Result, error) {
	if svc == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil service")
	}
	// A Cloud Run service's Name IS its relative resource name, so it is the id
	// unmodified.
	id := svc.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a service with no resource name")
	}

	content := gcpcontent.RunService{
		Name:       gcpgraph.LastSegment(id),
		URI:        svc.GetUri(),
		Ingress:    svc.GetIngress().String(),
		ServiceAcc: svc.GetTemplate().GetServiceAccount(),
	}
	for _, container := range svc.GetTemplate().GetContainers() {
		if image := container.GetImage(); image != "" {
			content.Images = append(content.Images, image)
		}
	}
	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{"image_count": strconv.Itoa(len(content.Images))}
	setIfNotEmpty(metadata, "uri", content.URI)
	setIfNotEmpty(metadata, "ingress", content.Ingress)
	setIfNotEmpty(metadata, "location", locationOfResourceName(id))
	for k, v := range svc.GetLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: content.Name, ResourceType: gcpgraph.ResourceTypeRunService,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	if email := content.ServiceAcc; email != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ServiceAccountResourceName(projectID, email),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	// A secret mounted as a volume and a secret read into an environment
	// variable are the same dependency, so both are walked.
	for _, volume := range svc.GetTemplate().GetVolumes() {
		if secret := volume.GetSecret().GetSecret(); secret != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: secretResourceName(projectID, secret), Type: gcpgraph.EdgeMountsSecret,
			})
		}
	}
	for _, container := range svc.GetTemplate().GetContainers() {
		for _, env := range container.GetEnv() {
			secret := env.GetValueSource().GetSecretKeyRef().GetSecret()
			if secret == "" {
				continue
			}
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: secretResourceName(projectID, secret), Type: gcpgraph.EdgeMountsSecret,
			})
		}
	}
	return out, nil
}

// CloudFunctions enumerates the project's Cloud Functions.
func CloudFunctions(list Lister[*functionspb.Function]) Subcollector {
	return New("gcp-cloud-functions", list, convertCloudFunction)
}

func convertCloudFunction(projectID string, fn *functionspb.Function) (gcpgraph.Result, error) {
	if fn == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil function")
	}
	id := fn.GetName()
	if id == "" {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a function with no resource name")
	}

	fields := nonEmptyFields(map[string]string{
		"runtime":     fn.GetBuildConfig().GetRuntime(),
		"entryPoint":  fn.GetBuildConfig().GetEntryPoint(),
		"eventType":   fn.GetEventTrigger().GetEventType(),
		"pubsubTopic": fn.GetEventTrigger().GetPubsubTopic(),
		"uri":         fn.GetServiceConfig().GetUri(),
	})
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: gcpgraph.LastSegment(id), SelfLink: fn.GetServiceConfig().GetUri(),
		Description: fn.GetDescription(), State: fn.GetState().String(),
		Location: locationOfResourceName(id), Labels: fn.GetLabels(), Fields: fields,
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	metadata := map[string]string{}
	setIfNotEmpty(metadata, "state", fn.GetState().String())
	setIfNotEmpty(metadata, "runtime", fn.GetBuildConfig().GetRuntime())
	setIfNotEmpty(metadata, "location", locationOfResourceName(id))
	for k, v := range fn.GetLabels() {
		metadata["label/"+k] = v
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: gcpgraph.LastSegment(id), ResourceType: gcpgraph.ResourceTypeCloudFunction,
		Region: locationOfResourceName(id), Content: raw, Metadata: metadata,
	}}}
	if email := fn.GetServiceConfig().GetServiceAccountEmail(); email != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: gcpgraph.ServiceAccountResourceName(projectID, email),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	for _, env := range fn.GetServiceConfig().GetSecretEnvironmentVariables() {
		if secret := env.GetSecret(); secret != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: secretResourceName(projectID, secret), Type: gcpgraph.EdgeMountsSecret,
			})
		}
	}
	for _, volume := range fn.GetServiceConfig().GetSecretVolumes() {
		if secret := volume.GetSecret(); secret != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: secretResourceName(projectID, secret), Type: gcpgraph.EdgeMountsSecret,
			})
		}
	}
	// The event source that invokes this function. The edge runs FROM the
	// source: a topic triggers a function, not the other way round.
	if topic := fn.GetEventTrigger().GetPubsubTopic(); topic != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: topic, To: id, Type: gcpgraph.EdgeTriggers,
			Metadata: map[string]string{"event_type": fn.GetEventTrigger().GetEventType()},
		})
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: id, To: topic, Type: gcpgraph.EdgeSubscribesTo,
		})
	}
	return out, nil
}

// secretResourceName normalizes a secret reference, which the APIs give either
// as a bare name or as a full resource name, onto the id the Secret Manager
// enumeration emits.
func secretResourceName(projectID, secret string) string {
	if strings.HasPrefix(secret, "projects/") {
		return secret
	}
	return "projects/" + projectID + "/secrets/" + secret
}

// locationOfResourceName reads the location out of a relative resource name of
// the form projects/{p}/locations/{loc}/....
func locationOfResourceName(name string) string {
	parts := strings.Split(name, "/")
	for i, seg := range parts {
		if seg == "locations" && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// regionOf turns a zone into its region; a location that is already a region is
// returned unchanged.
func regionOf(location string) string {
	parts := strings.Split(location, "-")
	if len(parts) == 3 {
		return parts[0] + "-" + parts[1]
	}
	return location
}

func globalComputeSelfLink(projectID, kind, name string) string {
	if strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "projects/") {
		return name
	}
	return "https://www.googleapis.com/compute/v1/projects/" + projectID + "/global/" + kind + "/" + name
}

func regionalComputeSelfLink(projectID, region, kind, name string) string {
	if strings.HasPrefix(name, "https://") || strings.HasPrefix(name, "projects/") {
		return name
	}
	return "https://www.googleapis.com/compute/v1/projects/" + projectID +
		"/regions/" + region + "/" + kind + "/" + name
}
