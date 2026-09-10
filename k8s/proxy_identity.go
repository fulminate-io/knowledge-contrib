// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"strings"
)

// proxy_identity.go — the three remaining proxy families: ASSUMES_IDENTITY,
// USES_DISK and CONNECTS_TO.
//
// THE FIRST TWO ARE THE FAMILIES WITH TWO ID-MINTING ARMS. When the account is
// known the proxy id is "proxy:cloud:<account>:<id>"; when it is not, it is the
// dangling "proxy:cloud::<id>". A family collapsed to one arm is wrong in one
// of two ways and green either way if the test only counts edges: taking the
// account arm always produces no id at all for an unknown account, and taking
// the dangling arm always produces the WRONG id when the account is known —
// which later collides with the enriched proxy the account-bearing pass mints.
//
// THE THIRD CARRIES A SECURITY INVARIANT and it is stated at its own function.

// === ASSUMES_IDENTITY ===

// buildAssumesIdentityEdges renders ASSUMES_IDENTITY from a ServiceAccount to
// the cloud IAM identity it is bound to, on all three providers.
//
// THE ACCOUNT IS KNOWABLE FOR TWO OF THE THREE and not for the third, which is
// what makes both id-minting arms reachable in one family: an IRSA role ARN
// carries the AWS account in the ARN itself, a GCP service-account email
// carries the project after the "@", and an Azure client id is a bare UUID that
// names no subscription at all.
//
// A ServiceAccount with none of the three annotations emits nothing.
func buildAssumesIdentityEdges(nodes []Node, acc *proxyAccumulator) []Edge {
	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "ServiceAccount" {
			continue
		}
		for _, target := range identityTargets(n.Metadata) {
			proxyID, ok := acc.proxy(target)
			if !ok {
				continue
			}
			out = append(out, Edge{
				FromID: n.ID,
				ToID:   proxyID,
				Type:   edgeAssumesIdentity,
				Method: target.Provider,
			})
		}
	}
	return out
}

// identityTargets resolves a ServiceAccount's identity metadata to cloud IAM
// targets. A ServiceAccount may legitimately carry more than one.
func identityTargets(meta map[string]string) []proxyTarget {
	var out []proxyTarget

	if arn := meta[metaKeyIRSARoleARN]; arn != "" {
		out = append(out, proxyTarget{
			Account:      arnAccount(arn),
			ID:           arn,
			ResourceType: "aws:iam:role",
			Provider:     "aws",
			SymbolName:   arn[strings.LastIndex(arn, "/")+1:],
		})
	}
	if email := meta[metaKeyGCPServiceAccount]; email != "" {
		project := gcpProjectFromEmail(email)
		out = append(out, proxyTarget{
			Account:      project,
			ID:           "projects/" + project + "/serviceAccounts/" + email,
			ResourceType: "gcp:iam:serviceAccount",
			Provider:     "gcp",
			SymbolName:   email,
		})
	}
	if clientID := meta[metaKeyAzureClientID]; clientID != "" {
		// NO ACCOUNT IS DERIVABLE. A client id is a bare UUID: the
		// subscription that owns the managed identity is not in it, and this
		// collector has no way to look it up. This is the DANGLING ARM, and it
		// is the ordinary case for Azure rather than a failure.
		out = append(out, proxyTarget{
			ID:           clientID,
			ResourceType: "azure:managedidentity",
			Provider:     "azure",
			SymbolName:   clientID,
		})
	}
	return out
}

// arnAccount pulls the account out of an AWS ARN:
// arn:aws:iam::<account>:role/<name>.
func arnAccount(arn string) string {
	parts := strings.Split(arn, ":")
	if len(parts) < 5 {
		return ""
	}
	return parts[4]
}

// gcpProjectFromEmail pulls the project out of a GCP service-account email:
// <name>@<project>.iam.gserviceaccount.com.
func gcpProjectFromEmail(email string) string {
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return ""
	}
	project, _, _ := strings.Cut(domain, ".")
	return project
}

// === USES_DISK ===

// buildUsesDiskEdges renders USES_DISK from a PersistentVolume to the cloud
// disk backing it.
//
// THE SAME TWO ARMS AS ASSUMES_IDENTITY, reached through a DIFFERENT PARSE: a
// GCE persistent disk name and an Azure disk URI carry their project and
// subscription, and a bare EBS volume id does not. The two families are
// separate functions rather than one generic pass because the source shapes
// have nothing in common; a single implementation over one of them leaves the
// other's parse untested.
func buildUsesDiskEdges(nodes []Node, acc *proxyAccumulator) []Edge {
	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "PersistentVolume" {
			continue
		}
		target, ok := parseDiskHandle(n.Metadata["volume_handle"], n.Metadata["volume_driver"])
		if !ok {
			continue
		}
		proxyID, ok := acc.proxy(target)
		if !ok {
			continue
		}
		out = append(out, Edge{
			FromID: n.ID,
			ToID:   proxyID,
			Type:   edgeUsesDisk,
			Method: target.Provider,
		})
	}
	return out
}

// parseDiskHandle resolves a volume handle to the cloud disk it names.
//
//	projects/<p>/zones/<z>/disks/<d>   GCE, account = project
//	/subscriptions/<s>/.../disks/<d>   Azure, account = subscription
//	vol-0123456789abcdef0             EBS, NO ACCOUNT — the dangling arm
func parseDiskHandle(handle, driver string) (proxyTarget, bool) {
	if handle == "" {
		return proxyTarget{}, false
	}
	name := handle[strings.LastIndex(handle, "/")+1:]

	switch {
	case strings.HasPrefix(handle, "projects/"):
		parts := strings.Split(handle, "/")
		if len(parts) < 2 {
			return proxyTarget{}, false
		}
		return proxyTarget{
			Account:      parts[1],
			ID:           handle,
			ResourceType: "gcp:compute:disk",
			Provider:     "gcp",
			SymbolName:   name,
		}, true

	case strings.HasPrefix(strings.ToLower(handle), "/subscriptions/"):
		return proxyTarget{
			Account:      azureSubscription(handle),
			ID:           handle,
			ResourceType: "azure:compute:disk",
			Provider:     "azure",
			SymbolName:   name,
		}, true

	case strings.HasPrefix(handle, "vol-"):
		return proxyTarget{
			ID:           handle,
			ResourceType: "aws:ebs:volume",
			Provider:     "aws",
			SymbolName:   handle,
		}, true

	default:
		// A driver this collector does not recognize backs the volume with
		// something that is not a cloud disk — a local path, an NFS export, a
		// third-party CSI plugin. There is nothing to point at.
		_ = driver
		return proxyTarget{}, false
	}
}

// === CONNECTS_TO ===

// externalPattern names a class of external service endpoint and the shape that
// identifies one.
type externalPattern struct {
	Name         string
	Provider     string
	ResourceType string
	Re           *regexp.Regexp
}

// externalPatterns is the set of endpoint shapes this collector recognizes in a
// workload's configuration.
var externalPatterns = []externalPattern{
	{"aws-rds", "aws", "aws:rds:instance", regexp.MustCompile(`[a-z0-9-]+\.[a-z0-9]+\.[a-z0-9-]+\.rds\.amazonaws\.com`)},
	{"aws-elasticache", "aws", "aws:elasticache:cluster", regexp.MustCompile(`[a-z0-9-]+\.[a-z0-9]+\.cache\.amazonaws\.com`)},
	{"aws-s3", "aws", "aws:s3:bucket", regexp.MustCompile(`[a-z0-9.-]+\.s3[.a-z0-9-]*\.amazonaws\.com`)},
	{"aws-sqs", "aws", "aws:sqs:queue", regexp.MustCompile(`sqs\.[a-z0-9-]+\.amazonaws\.com/[0-9]+/[A-Za-z0-9_-]+`)},
	{"gcp-sql", "gcp", "gcp:sql:instance", regexp.MustCompile(`[a-z0-9-]+:[a-z0-9-]+:[a-z0-9-]+\.sql\.goog`)},
	{"gcp-storage", "gcp", "gcp:storage:bucket", regexp.MustCompile(`[a-z0-9._-]+\.storage\.googleapis\.com`)},
	{"azure-blob", "azure", "azure:storage:blob", regexp.MustCompile(`[a-z0-9]+\.blob\.core\.windows\.net`)},
}

// buildConnectsToEdges renders CONNECTS_TO from a workload to an external cloud
// service its configuration names.
//
// === THE SECURITY INVARIANT, AND IT IS THIS FUNCTION'S REAL SUBJECT ===
//
// THE EVIDENCE CARRIES ONLY THE SUBSTRING THE PATTERN MATCHED, NEVER THE VALUE
// IT WAS FOUND IN. The prefix is built from METADATA ALONE — the container
// name, the environment variable name, the reference name, the key name — and
// never from a Secret or ConfigMap value. A connection string is exactly the
// kind of value that carries a password beside the hostname, and this graph is
// summarized, embedded and synced. An implementation that built the evidence
// from the raw value would write an operator's credential into a replicated
// store, AND EVERY OTHER ASSERTION ABOUT THIS FUNCTION WOULD STILL PASS: the
// edge would exist, its endpoints would be right, its count would be right.
// That is why the test for this family asserts on what is ABSENT from the
// evidence.
//
// THE DEDUPE: two matches to the same target from one workload collapse to one
// edge. A workload naming the same database in a URL and in a hostname variable
// has one dependency, not two.
func buildConnectsToEdges(nodes []Node, acc *proxyAccumulator) []Edge {
	var out []Edge
	seen := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		if !workloadResourceTypes[n.Metadata["resource_type"]] {
			continue
		}
		for _, ref := range externalReferences(n) {
			proxyID, ok := acc.proxy(proxyTarget{
				ID:           ref.matched,
				ResourceType: ref.pattern.ResourceType,
				Provider:     ref.pattern.Provider,
				SymbolName:   ref.matched,
			})
			if !ok {
				continue
			}
			key := n.ID + "\x00" + proxyID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Edge{
				FromID: n.ID,
				ToID:   proxyID,
				Type:   edgeConnectsTo,
				Method: ref.pattern.Name,
				// prefix is metadata-only; matched is the pattern's own match.
				// Neither is the value the match was found in.
				Evidence: ref.prefix + " pattern=" + ref.pattern.Name + " matched=" + ref.matched,
			})
		}
	}
	return out
}

// externalReference is one recognized endpoint found on a workload.
type externalReference struct {
	pattern externalPattern
	// matched is the SUBSTRING THE REGEX MATCHED, never the whole value.
	matched string
	// prefix is built from metadata names only.
	prefix string
}

// externalReferences scans a workload node's own metadata for endpoints.
//
// IT SCANS METADATA VALUES THIS COLLECTOR PUT THERE, not Secret or ConfigMap
// bodies: this collector strips Secret values before they ever reach a node, so
// there is no secret value in scope here to leak. The prefix names the metadata
// KEY the match came from, which is a name rather than a value.
func externalReferences(n *Node) []externalReference {
	var out []externalReference
	keys := sortedUnique(mapKeys(n.Metadata))
	for _, key := range keys {
		value := n.Metadata[key]
		if value == "" {
			continue
		}
		for _, p := range externalPatterns {
			matched := p.Re.FindString(value)
			if matched == "" {
				continue
			}
			out = append(out, externalReference{
				pattern: p,
				matched: matched,
				prefix:  "workload=" + n.SymbolName + " key=" + key,
			})
		}
	}
	return out
}
