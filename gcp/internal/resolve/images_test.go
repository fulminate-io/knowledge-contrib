// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// images_test.go — the REGISTRY-KIND axis, which has TWO populated arms.
//
// A module implementing only the Artifact Registry arm silently drops every
// legacy-registry service's edge while the edge type stays in the declared
// vocabulary, so each arm is its own case and the third cell closes the axis.

const (
	arRepoID  = "projects/proj-a/locations/us-central1/repositories/images"
	runSvcID  = "projects/proj-a/locations/us-central1/services/api"
	runSvcID2 = "projects/proj-a/locations/us-central1/services/legacy"
)

func arRepo(t *testing.T) gcpgraph.Resource {
	t.Helper()
	return gcpgraph.Resource{
		ID: arRepoID, Name: "images", ResourceType: gcpgraph.ResourceTypeArtifactRegistryRepo,
		Content: mustMarshal(t, gcpcontent.Generic{Name: "images"}),
	}
}

func runService(t *testing.T, id, name string, images ...string) gcpgraph.Resource {
	t.Helper()
	return gcpgraph.Resource{
		ID: id, Name: name, ResourceType: gcpgraph.ResourceTypeRunService,
		Content: mustMarshal(t, gcpcontent.RunService{Name: name, Images: images}),
	}
}

// The Artifact Registry arm.
func TestCloudRunImagesMatchesAnArtifactRegistryReference(t *testing.T) {
	got, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		runService(t, runSvcID, "api", "us-central1-docker.pkg.dev/proj-a/images/api:v1"),
	}})
	if err != nil {
		t.Fatalf("CloudRunImages: %v", err)
	}
	if !hasEdge(got.Relations, runSvcID, arRepoID, gcpgraph.EdgeUsesImage) {
		t.Fatalf("no USES_IMAGE from the service to the repository: %v", edgeKeys(got.Relations))
	}
	if got.Relations[0].Metadata["image"] != "us-central1-docker.pkg.dev/proj-a/images/api:v1" {
		t.Errorf("edge evidence should carry the image reference: %v", got.Relations[0].Metadata)
	}
	if got.Relations[0].Method != resolve.MethodImageLineage {
		t.Errorf("edge method: got %q, want %q", got.Relations[0].Method, resolve.MethodImageLineage)
	}
}

// The LEGACY container-registry arm, looked up in the SAME repository index.
func TestCloudRunImagesMatchesALegacyRegistryReference(t *testing.T) {
	got, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		runService(t, runSvcID2, "legacy", "gcr.io/proj-a/images:v1"),
	}})
	if err != nil {
		t.Fatalf("CloudRunImages: %v", err)
	}
	if !hasEdge(got.Relations, runSvcID2, arRepoID, gcpgraph.EdgeUsesImage) {
		t.Fatalf("no USES_IMAGE for a legacy registry reference: %v", edgeKeys(got.Relations))
	}
}

// A regional legacy host resolves on the same arm.
func TestCloudRunImagesMatchesARegionalLegacyRegistryHost(t *testing.T) {
	got, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		runService(t, runSvcID2, "legacy", "us.gcr.io/proj-a/images:v1"),
	}})
	if err != nil {
		t.Fatalf("CloudRunImages: %v", err)
	}
	if !hasEdge(got.Relations, runSvcID2, arRepoID, gcpgraph.EdgeUsesImage) {
		t.Errorf("no USES_IMAGE for a regional legacy host: %v", edgeKeys(got.Relations))
	}
}

// The cell that closes the registry-kind axis.
func TestCloudRunImagesIgnoresAnImageOnNeitherRegistry(t *testing.T) {
	got, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		runService(t, runSvcID, "api", "docker.io/library/nginx:latest", "nginx"),
	}})
	if err != nil {
		t.Fatalf("CloudRunImages: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an image on neither registry produced %d edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

// An image on the right registry but in a repository this walk did not collect
// resolves to nothing rather than to a dangling endpoint.
func TestCloudRunImagesIgnoresAnUncollectedRepository(t *testing.T) {
	got, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		runService(t, runSvcID, "api", "us-central1-docker.pkg.dev/proj-a/elsewhere/api:v1"),
	}})
	if err != nil {
		t.Fatalf("CloudRunImages: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an uncollected repository produced %d edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

func TestCloudRunImagesDedupesRepeatedReferences(t *testing.T) {
	got, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		runService(t, runSvcID, "api",
			"us-central1-docker.pkg.dev/proj-a/images/api:v1",
			"us-central1-docker.pkg.dev/proj-a/images/sidecar:v1"),
	}})
	if err != nil {
		t.Fatalf("CloudRunImages: %v", err)
	}
	if len(got.Relations) != 1 {
		t.Errorf("two images in one repository produced %d edges, want 1: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

func TestCloudRunImagesFailsLoudlyOnCorruptContent(t *testing.T) {
	_, err := resolve.CloudRunImages(gcpgraph.Result{Resources: []gcpgraph.Resource{
		arRepo(t),
		{ID: runSvcID, Name: "api", ResourceType: gcpgraph.ResourceTypeRunService,
			Content: []byte("{not json")},
	}})
	if err == nil {
		t.Fatal("a service with undecodable content was skipped silently")
	}
	if !strings.Contains(err.Error(), "api") {
		t.Errorf("error %q does not name the service it could not read", err)
	}
}
