// SPDX-License-Identifier: Apache-2.0

package gcpgraph

import "strings"

// ids.go — THE NODE-ID SPELLINGS, which are the join keys for every edge.
//
// Two families and three sentinels, carried forward verbatim:
//
//   - Compute-API resources use the full selfLink URL as the node id.
//   - Everything else uses the API RELATIVE RESOURCE NAME
//     (projects/{project}/topics/{name}), with Cloud Storage using gs://{bucket}.
//   - Three synthetic prefixes name things GCP has no resource for: a firewall
//     CIDR block, an external BGP peer, and the uncollected proxy targets a
//     collector references but does not enumerate.
//
// AN ID IS NEVER NORMALIZED ON THE WAY IN. An edge endpoint is whatever the API
// gave the referencing resource, so a helper that trimmed, lower-cased or
// re-based one would dangle every edge joined on it while every node still
// looked right.

// CIDRSentinelID names the synthetic node standing for one firewall CIDR block.
func CIDRSentinelID(cidr string) string { return "gcp:cidr:" + cidr }

// BGPPeerID names the synthetic node standing for one external BGP peer, which
// has no GCP-managed counterpart to collect.
func BGPPeerID(peerIP string) string { return "gcp:bgp-peer:" + peerIP }

// ServiceAccountResourceName builds the IAM service-account resource name from
// the project and the account's email. It is the id an instance's USES_SA edge
// points at, so it must match the id the IAM enumeration emits exactly.
func ServiceAccountResourceName(projectID, email string) string {
	return "projects/" + projectID + "/serviceAccounts/" + email
}

// ProjectResourceName is the resource name of the project itself, which is the
// target of the derived cross-project TRUSTS edge.
func ProjectResourceName(projectID string) string { return "projects/" + projectID }

// LastSegment returns the final path segment of a URL-like string, which is how
// a zone, a region or a machine type is read out of a selfLink.
func LastSegment(urlPath string) string {
	if i := strings.LastIndex(urlPath, "/"); i >= 0 {
		return urlPath[i+1:]
	}
	return urlPath
}

// ProjectFromSelfLink extracts the project id from a compute selfLink of the
// form https://www.googleapis.com/compute/v1/projects/{PROJECT}/... It returns
// empty for anything with no /projects/ segment, which is what makes a
// same-project comparison distinguishable from an unparseable link.
func ProjectFromSelfLink(selfLink string) string {
	const marker = "/projects/"
	_, after, ok := strings.Cut(selfLink, marker)
	if !ok {
		return ""
	}
	if before, _, ok := strings.Cut(after, "/"); ok {
		return before
	}
	return after
}

// ProjectFromServiceAccountEmail extracts the project id from a service-account
// email of the form {name}@{project}.iam.gserviceaccount.com. A user account or
// any other email yields empty, which is what keeps a human principal out of the
// cross-project trust derivation.
func ProjectFromServiceAccountEmail(email string) string {
	_, after, ok := strings.Cut(email, "@")
	if !ok {
		return ""
	}
	project, _, ok := strings.Cut(after, ".iam.gserviceaccount.com")
	if !ok {
		return ""
	}
	return project
}

// ProjectFromServiceAccountResourceName extracts the project id from a service
// account's own resource name.
func ProjectFromServiceAccountResourceName(name string) string {
	const prefix = "projects/"
	if !strings.HasPrefix(name, prefix) {
		return ""
	}
	before, _, ok := strings.Cut(strings.TrimPrefix(name, prefix), "/")
	if !ok {
		return ""
	}
	return before
}

// ParseArtifactRegistryResourceID splits an Artifact Registry repository
// resource name — projects/{project}/locations/{loc}/repositories/{repo} — into
// its project and repository. Either result is empty when the name does not
// carry that segment, and the caller decides what an incomplete parse means.
func ParseArtifactRegistryResourceID(id string) (project, repo string) {
	parts := strings.Split(id, "/")
	for i, seg := range parts {
		if i+1 >= len(parts) {
			break
		}
		switch seg {
		case "projects":
			project = parts[i+1]
		case "repositories":
			repo = parts[i+1]
		}
	}
	return project, repo
}
