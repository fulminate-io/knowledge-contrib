// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// declaration.go — THE FOREIGN-GRAPH CONTEXT THIS COLLECTOR DECLARES.
//
// WHY IT DID NOT EXIST, AND WHAT THAT COST. This collector READS a foreign
// context on every collect: internal/collect's walk builds a cloud context from
// it, resolves stream labels against it, finds correlations with it, and emits
// the proxy nodes, EMITTED_BY edges and CORRELATES_WITH edges that come out of
// those resolutions. It declared nothing, and a collector receives only what its
// entry declared, so the read was of an empty block on every collect an operator
// has ever run. The cross-graph half of this collector was dead by construction
// rather than by a wrong string — a different cause from the sibling collectors
// whose declarations named node types nothing emits, and the same consequence.
//
// THE FIELD SET IS THIS MODULE'S OWN AND IS NOT INTERCHANGEABLE. matchableNames
// in internal/logpipe indexes each declared node under three values: its
// `symbol_name`, its `name` metadata key and its `service` metadata key. A
// declaration copied from the k8s-logs collector would carry that module's keys
// — namespace, cluster_name, resource_type, region, provider — and omit `name`
// and `service`, and the client's projection drops an undeclared key rather than
// carrying it empty. Every candidate would then be indexed under its symbol name
// alone, and a resource whose name lives in metadata would resolve nothing: an
// empty result from a declaration that reads as thorough.
//
// THE MATCHING IS BY NAME AND IS PROVIDER-AGNOSTIC, which is what decides the
// family list. Nothing here ranks on a provider or on a resource type, so any
// inventory family whose resources carry a name a Loki label can equal is worth
// declaring. Three do. `gcp` is the absence with a reason: the gcp collector
// emits its resource type AS the node type — forty-nine `gcp:<service>:<kind>`
// values plus a `gcp-cidr-block` sentinel — and a declaration selects a type only
// by naming it exactly, at one node read per type per graph per collect. The only
// other admissible spelling today is the family with no node types, which yields
// the graph names and no nodes: zero candidates, exactly as declaring nothing
// does, plus a refusal for every operator running no gcp collector. So gcp waits
// for an explicit family-level selector, and the README says so.

// The node types the three declared inventory collectors emit, spelled here
// because each is a separate Go module this one cannot import.
//
// THEY ARE THREE CONSTANTS AND NOT ONE, even though the strings are equal today:
// they are facts about three different collectors, and sharing one would make a
// rename on any side invisible on the others. The cross-module census in the
// root module holds each against its own module's constant and reds BY NAME on a
// declared type the family's collector does not emit.
const (
	awsNodeTypeCloudResource   = "cloud-resource"
	azureNodeTypeCloudResource = "cloud-resource"
	k8sNodeTypeCloudResource   = "cloud-resource"
)

// The families this collector declares. Each is the name a provider collector is
// registered under, which is also the graph family its nodes land in.
const (
	familyAWS   = "aws"
	familyAzure = "azure"
	familyK8s   = "k8s"
)

// The metadata keys the resolver matches on, beside the node's symbol name.
// They are the two a cloud collector conventionally writes a resource's name
// into, and they are DECLARED rather than assumed: an undeclared key arrives
// absent, and a candidate indexed under its symbol name alone matches nothing a
// label names by its metadata.
const (
	metaName    = "name"
	metaService = "service"
)

// cloudResourceReason is the reason the three declared families share: the
// matching rule is one rule and does not vary by provider.
const cloudResourceReason = "EMITTED_BY and confirmed CORRELATES_WITH: a log stream's " +
	"service-identifying label is resolved to a cloud resource by NAME — the resource's symbol " +
	"name, or its `name` or `service` metadata — and the edges are what CONFIRM a temporal " +
	"correlation between two templates, so a declaration without them yields correlations this " +
	"collector cannot stand behind."

// DeclaredForeignContext is what this collector declares it needs from other
// graphs, keyed by family in the shape the client's config loader decodes.
// IT IS A LITERAL MAP AND NOT A LOOP, deliberately. A declaration whose node
// type is computed rather than written is invisible to a source-reading census —
// which is exactly how four sibling declarations shipped a type string no
// collector emits without a single literal in the tree to find. Spelling each
// entry out is what lets the cross-module census hold every declared string
// against the emitters' own constants.
func DeclaredForeignContext() framework.ForeignContextDeclaration {
	return framework.ForeignContextDeclaration{
		familyAWS: {
			NodeTypes:    []string{awsNodeTypeCloudResource},
			NodeFields:   []string{"id", "symbol_name"},
			MetadataKeys: []string{metaName, metaService},
			EdgeFields:   []string{"from_id", "to_id"},
			Reason:       cloudResourceReason,
		},
		familyAzure: {
			NodeTypes:    []string{azureNodeTypeCloudResource},
			NodeFields:   []string{"id", "symbol_name"},
			MetadataKeys: []string{metaName, metaService},
			EdgeFields:   []string{"from_id", "to_id"},
			Reason:       cloudResourceReason,
		},
		familyK8s: {
			NodeTypes:    []string{k8sNodeTypeCloudResource},
			NodeFields:   []string{"id", "symbol_name"},
			MetadataKeys: []string{metaName, metaService},
			EdgeFields:   []string{"from_id", "to_id"},
			Reason:       cloudResourceReason,
		},
	}
}

// contextBlockIndent is the indentation the README's worked entry sits at: the
// `context` key is two levels inside `collectors` → `loki`, and this module's
// documented entry is indented two spaces per level.
const contextBlockIndent = "      "

// RenderedContextBlock is the exact `context` fragment this module's README must
// carry, indentation included.
//
// THE README IS THE ONLY ARTIFACT THAT CARRIES THIS ENTRY. This module renders
// no config entry in Go — there is no ExampleEntry to drift from — so nothing
// but a generator and a pin can keep the documented block honest, and nothing
// but the client's own loader can say whether it loads. Both exist: this
// fragment is pinned against the README here, and the client's README-loads test
// drives the whole documented file through the real loader.
func RenderedContextBlock() (string, error) {
	body, err := json.MarshalIndent(DeclaredForeignContext(), contextBlockIndent, "  ")
	if err != nil {
		return "", fmt.Errorf("loki: rendering the declared context block: %w", err)
	}
	return contextBlockIndent + `"context": ` + string(body), nil
}
