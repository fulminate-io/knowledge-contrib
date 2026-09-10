// SPDX-License-Identifier: Apache-2.0

package main

import "strings"

// derived_images.go — USES_IMAGE, the one family in the 33 that is REACHABLE
// AND VACUOUS on a Kubernetes graph.
//
// WHY IT IS HERE AT ALL. The built-in collector's image-lineage pass builds a
// registry index from the SAME graph it is populating and returns early on an
// empty index. That index is built only from ECR, ACR and Artifact Registry
// repository nodes, which a Kubernetes graph never holds — so on this
// collector's own graph the index is always empty and the family emits nothing.
// It is in the vocabulary because it is in the parity floor, the pass is
// implemented because a graph that ever did carry registry nodes would need it,
// and no test asserts an edge from it.
//
// THE MATCH IS AGAINST THIS COLLECTOR'S OWN RESULT, never against a foreign
// graph: the index is built from registry-repository nodes the same walk
// produced, so nothing here reads the declared foreign context and no edge this
// pass could produce names another graph.

// registryNodeTypes is the set of resource types the registry index is built
// from. A Kubernetes walk produces none of them.
var registryResourceTypes = map[string]bool{
	"aws:ecr:repository":        true,
	"gcp:artifactregistry:repo": true,
	"azure:acr:repository":      true,
}

// buildImageLineageEdges derives USES_IMAGE edges from a walk's own nodes.
//
// It returns nil whenever the registry index is empty, which on a Kubernetes
// graph is always: the two gates are the index being non-empty and the derived
// edge set being non-empty, and the first one closes first.
func buildImageLineageEdges(nodes []Node) []Edge {
	index := buildRegistryIndex(nodes)
	if len(index) == 0 {
		return nil
	}

	var out []Edge
	seen := make(map[string]bool)
	for i := range nodes {
		n := &nodes[i]
		if !workloadResourceTypes[n.Metadata["resource_type"]] {
			continue
		}
		for img := range strings.SplitSeq(n.Metadata["images"], ",") {
			repo := imageRepositoryName(img)
			if repo == "" {
				continue
			}
			target, ok := index[repo]
			if !ok {
				continue
			}
			key := n.ID + "\x00" + target
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Edge{FromID: n.ID, ToID: target, Type: edgeUsesImage})
		}
	}
	return out
}

// buildRegistryIndex maps a repository basename to the id of the registry-repo
// node carrying it, over this walk's own nodes.
func buildRegistryIndex(nodes []Node) map[string]string {
	var index map[string]string
	for i := range nodes {
		n := &nodes[i]
		if !registryResourceTypes[n.Metadata["resource_type"]] {
			continue
		}
		name := n.SymbolName
		if name == "" {
			continue
		}
		if index == nil {
			index = make(map[string]string)
		}
		index[name] = n.ID
	}
	return index
}

// imageRepositoryName reduces a container image reference to its repository
// basename: the last path segment with any tag or digest removed.
//
// "123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v1" yields "api", and so
// does "ghcr.io/org/api@sha256:deadbeef".
func imageRepositoryName(image string) string {
	img := strings.TrimSpace(image)
	if img == "" {
		return ""
	}
	if at := strings.Index(img, "@"); at >= 0 {
		img = img[:at]
	}
	// A colon BEFORE the last slash is a registry port, not a tag.
	if slash := strings.LastIndex(img, "/"); slash >= 0 {
		if colon := strings.LastIndex(img, ":"); colon > slash {
			img = img[:colon]
		}
	} else if colon := strings.LastIndex(img, ":"); colon >= 0 {
		img = img[:colon]
	}
	if slash := strings.LastIndex(img, "/"); slash >= 0 {
		img = img[slash+1:]
	}
	return img
}
