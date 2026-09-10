// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// linkage.go — THE EDGE TYPES THAT NAME AN ENDPOINT IN ANOTHER GRAPH, emitted
// through the contract's two graph-family fields against the declared foreign
// context.
//
// === WHY THIS COLLECTOR OWES THEM ===
//
// The client's post-collect linker walks the code and cloud graphs and matches
// them against each other. Kubernetes-collected nodes are the only SOURCE
// predicate it has: a Helm label, a ServiceAccount's identity binding. The
// built-in Kubernetes collector writes into the CLOUD family, so the linker's
// cloud scan finds its nodes. This collector's registration name is its family,
// so its nodes land in family "k8s", which no linker read touches. Replace the
// built-in with this collector and these edges stop existing — not because
// anyone decided to drop them, but because the scan looks somewhere else.
//
// === WHICH FIELD A SHAPE USES IS ITS DIRECTION, NOT A DETAIL ===
//
// The contract carries two graph-family fields and EXACTLY ONE IS SET PER EDGE.
// TargetGraph names the family ToID lives in; SourceGraph names the family
// FromID lives in. Either way the client enumerates that family's loaded graphs,
// locates the FOREIGN endpoint among them, materializes a proxy and links the
// edge into the linkage graph — so the field a shape sets is the statement of
// which of its two ends is the foreign one. Setting both is refused by the
// collect: one resolution reaches one foreign family.
//
// THREE SHAPES PUT THE K8S NODE ON THE NEAR SIDE AND USE TargetGraph: the three
// WORKLOAD_IDENTITY shapes, each running from a ServiceAccount to a cloud
// identity.
//
// DEPLOYS IS THE ONE THAT RUNS THE OTHER WAY AND USES SourceGraph. The built-in
// linker emits it FROM the Chart.yaml file node TO the Kubernetes node, so its
// foreign endpoint is the FROM. It was computed and held while the contract had
// only the target-graph field, because both ways of forcing it were wrong —
// reversing the direction asserts a relationship the built-in does not, and
// emitting it with no family lands an in-graph edge to a chart id nothing in the
// k8s graph resolves. The mirror field is the third option and this collector
// now takes it. See [deploysEdges].
//
// FOUR SHAPES OVER TWO EDGE TYPES: DEPLOYS is one, and WORKLOAD_IDENTITY is
// THREE — one per provider, reading three DIFFERENT metadata keys. A single
// WORKLOAD_IDENTITY implementation passes for one provider and silently drops
// two.
//
// TWO OF THE FOUR NEED NO FOREIGN INPUT AT ALL. The IRSA and GCP identity
// shapes compose their target from the ServiceAccount's OWN metadata — the role
// ARN is used directly as the target id, and the GCP target is built from the
// service-account email. DEPLOYS and the Azure shape resolve against a foreign
// graph, so a rule of the form "no context block means no linkage edges" is
// wrong for two of the four.
//
// A WORKLOAD-TO-REPOSITORY SHAPE IS NOT AMONG THEM. Its far endpoint would be a
// code graph's NAME, and the client resolves a foreign endpoint by looking for
// a NODE carrying that id: a code graph holds no node denoting a repository, so
// the endpoint resolves against nothing and the resolver fails the whole
// collect rather than dropping one edge.

// The graph FAMILIES this collector's linkage edges point into. They are
// families rather than instances by the contract's own rule: the client
// enumerates a family's loaded graphs and locates the endpoint among them.
//
// THE THREE PROVIDER FAMILIES WERE ONE `cloud` FAMILY AND ARE NOW THREE, which
// is a sharpening rather than a rename. Each workload-identity shape resolves
// against a DIFFERENT provider's object — an AWS IAM role, a GCP service
// account, an Azure managed identity — and pointing all three into one family
// was only possible while a single built-in collector produced all three. They
// are contrib collectors now, each registering its own graph type, so an edge
// names the family whose collector actually emits the object it points at; a
// wrong family here is an edge whose endpoint is looked for in a graph that
// could never hold it.
const (
	graphFamilyCode = "code"
	familyAWS       = "aws"
	familyGCP       = "gcp"
	familyAzure     = "azure"
)

// chartFileNodes maps a Helm chart NAME to the Chart.yaml file node declaring
// it, over the declared code slice.
//
// THE NAME IS PARSED OUT OF THE FILE BODY, not taken from the path: a chart
// directory is often named for the release rather than the chart, and the
// `name:` key in Chart.yaml is what a Helm label matches. A declaration
// carrying paths alone could not produce this map, which is why this
// collector's entry asks for the file node's id, path AND content.
func chartFileNodes(fc framework.ForeignContext) map[string]framework.ForeignNode {
	charts := map[string]framework.ForeignNode{}
	for _, g := range fc.Graphs(framework.FamilyCode) {
		for _, n := range g.Nodes {
			if !isChartFile(n.FilePath) {
				continue
			}
			if name := chartNameFromBody(n.Content); name != "" {
				charts[name] = n
			}
		}
	}
	return charts
}

func isChartFile(path string) bool {
	base := path
	if i := strings.LastIndex(path, "/"); i >= 0 {
		base = path[i+1:]
	}
	return base == "Chart.yaml" || base == "Chart.yml"
}

// chartNameFromBody reads the `name:` key out of a Chart.yaml body.
//
// IT IS A LINE SCAN RATHER THAN A YAML PARSE, and deliberately: a Chart.yaml's
// name is a top-level scalar, and adding a YAML dependency for one key would
// widen this module's dependency lock for nothing.
//
// WHAT SKIPS A NESTED `name:` IS CutPrefix's ANCHORING, not a leading-character
// test. An earlier version carried an explicit indent-and-comment guard beside
// it; the mutation pass showed that guard could be deleted with every test
// still green, because CutPrefix already requires the line to BEGIN with
// "name:" and an indented or commented line therefore never matches. It is gone
// rather than kept as an unobserved liability, and this comment now names the
// mechanism that actually does the work.
func chartNameFromBody(body string) string {
	for line := range strings.SplitSeq(body, "\n") {
		rest, ok := strings.CutPrefix(line, "name:")
		if !ok {
			continue
		}
		return strings.Trim(strings.TrimSpace(rest), `"'`)
	}
	return ""
}

// cloudIdentitiesByClientID indexes the declared azure slice by the client id
// its nodes carry, which is the only key an Azure workload-identity annotation
// gives us to resolve against.
//
// IT READS THE FAMILY THIS COLLECTOR'S OWN ENTRY DECLARES. The block used to
// carry a built-in `cloud` arm; the families a client can supply are now the
// graph types the operator registered, and the shape this predicate needs is a
// managed identity, which is Azure's. The declaration and this read name the
// same family, and declaredForeignContext is the one place that name is written.
func cloudIdentitiesByClientID(fc framework.ForeignContext) map[string]framework.ForeignNode {
	out := map[string]framework.ForeignNode{}
	for _, g := range fc.Graphs(familyAzure) {
		for _, n := range g.Nodes {
			if id := n.Metadata["client_id"]; id != "" {
				out[id] = n
			}
		}
	}
	return out
}

// buildLinkageEdges returns the contract edges this collector emits into
// another graph.
//
// ALL FOUR SHAPES REACH THE RESULT, each carrying its family in the field that
// matches WHICH END IS FOREIGN: three in TargetGraph, and DEPLOYS in
// SourceGraph. Both fields are copied across rather than one, so a shape's
// direction is decided where the shape is computed and this loop stays a
// pass-through.
func buildLinkageEdges(nodes []Node, fc framework.ForeignContext) []Edge {
	var out []Edge
	for _, e := range computeLinkageEdges(nodes, fc) {
		out = append(out, Edge{
			FromID:      e.FromID,
			ToID:        e.ToID,
			Type:        e.Type,
			Method:      e.Method,
			Evidence:    e.Evidence,
			Confidence:  e.Confidence,
			SourceGraph: e.SourceGraph,
			TargetGraph: e.TargetGraph,
		})
	}
	return out
}

// linkageEdge is one cross-graph edge this collector computed, carrying the
// graph family its far endpoint lives in.
type linkageEdge struct {
	FromID      string
	ToID        string
	Type        string
	SourceGraph string
	TargetGraph string
	Method      string
	Evidence    string
	Confidence  float64
}

// computeLinkageEdges runs all four shapes. It is kept separate from
// buildLinkageEdges so the predicate rows can assert what each shape DECIDES
// without going through the contract-edge conversion.
func computeLinkageEdges(nodes []Node, fc framework.ForeignContext) []linkageEdge {
	var out []linkageEdge
	out = append(out, deploysEdges(nodes, fc)...)
	out = append(out, workloadIdentityEdges(nodes, fc)...)
	return out
}

// deploysEdges is shape 1: a Helm-labeled node matching a Chart.yaml.
//
// IT IS THE ONE SHAPE WHOSE FOREIGN ENDPOINT IS THE FROM, which is why it names
// its family in SourceGraph and leaves TargetGraph empty. The built-in linker
// emits it FROM the chart file node TO the Kubernetes node, and that direction
// is the relationship: reversing it to fit TargetGraph would assert something
// the built-in does not, and emitting it with neither field would land an
// in-graph edge to a chart id nothing in the k8s graph resolves. Setting the
// mirror field is the third option, and it is the one the contract now has.
//
// TWO LABELS, AND THE SECOND NEEDS STRIPPING. app.kubernetes.io/name is the
// chart name directly; helm.sh/chart is "<name>-<version>" and matches only
// after the version suffix is removed.
func deploysEdges(nodes []Node, fc framework.ForeignContext) []linkageEdge {
	charts := chartFileNodes(fc)
	if len(charts) == 0 {
		return nil
	}

	var out []linkageEdge
	seen := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		if n.Type == nodeTypeProxy {
			continue
		}
		for chart, evidence := range helmChartCandidates(n, charts) {
			file := charts[chart]
			key := file.ID + "\x00" + n.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, linkageEdge{
				// FROM the chart file node, TO this Kubernetes node — the
				// built-in linker's direction. The chart file is the FOREIGN
				// endpoint, so the code family goes in SourceGraph and
				// TargetGraph stays empty; setting the latter instead would send
				// the client looking for a Kubernetes node in a code graph.
				FromID:      file.ID,
				ToID:        n.ID,
				Type:        edgeDeploys,
				SourceGraph: graphFamilyCode,
				Method:      "tier1-helm",
				Evidence:    evidence,
				Confidence:  0.85,
			})
		}
	}
	return out
}

// helmChartCandidates returns the charts a node's labels name, mapped to the
// evidence naming which label matched.
func helmChartCandidates(n *Node, charts map[string]framework.ForeignNode) map[string]string {
	candidates := map[string]string{}
	if name := n.Metadata["label/app.kubernetes.io/name"]; name != "" {
		if _, ok := charts[name]; ok {
			candidates[name] = "label app.kubernetes.io/name=" + name
		}
	}
	if chart := n.Metadata["label/helm.sh/chart"]; chart != "" {
		stripped := stripChartVersion(chart)
		if _, ok := charts[stripped]; ok {
			candidates[stripped] = "label helm.sh/chart=" + chart
		}
	}
	return candidates
}

// stripChartVersion removes the trailing "-<version>" from a helm.sh/chart
// label value. A chart name may itself contain hyphens, so the split is at the
// LAST hyphen followed by something that starts like a version.
func stripChartVersion(chart string) string {
	idx := strings.LastIndex(chart, "-")
	if idx <= 0 || idx == len(chart)-1 {
		return chart
	}
	rest := chart[idx+1:]
	if rest[0] >= '0' && rest[0] <= '9' {
		return chart[:idx]
	}
	return chart
}

// workloadIdentityEdges is shapes 2, 3 and 4: the three provider bindings on a
// ServiceAccount.
//
// THE THREE READ THREE DIFFERENT METADATA KEYS, and TWO OF THEM NEED NO FOREIGN
// INPUT: the IRSA role ARN is used DIRECTLY as the target id, and the GCP
// target is composed from the service account's own email. Only Azure resolves
// a bare client id against the declared cloud slice, because a client id names
// nothing on its own.
//
// ALL THREE FIT THE CONTRACT'S DIRECTION: the ServiceAccount is the FROM and
// the cloud identity is the TO.
func workloadIdentityEdges(nodes []Node, fc framework.ForeignContext) []linkageEdge {
	identities := cloudIdentitiesByClientID(fc)

	var out []linkageEdge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "ServiceAccount" {
			continue
		}

		// Shape 2 — IRSA. No foreign input.
		if arn := n.Metadata[metaKeyIRSARoleARN]; arn != "" {
			out = append(out, linkageEdge{
				FromID:      n.ID,
				ToID:        arn,
				Type:        edgeWorkloadIdentity,
				TargetGraph: familyAWS,
				Method:      "irsa",
				Evidence:    "serviceaccount annotation " + annotationIRSARoleARN,
				Confidence:  1.0,
			})
		}

		// Shape 3 — GCP workload identity. No foreign input.
		if email := n.Metadata[metaKeyGCPServiceAccount]; email != "" {
			out = append(out, linkageEdge{
				FromID:      n.ID,
				ToID:        "projects/" + gcpProjectFromEmail(email) + "/serviceAccounts/" + email,
				Type:        edgeWorkloadIdentity,
				TargetGraph: familyGCP,
				Method:      "gcp-workload-identity",
				Evidence:    "serviceaccount annotation " + annotationGCPServiceAcct,
				Confidence:  1.0,
			})
		}

		// Shape 4 — Azure. THE ONE SHAPE THAT READS A FOREIGN GRAPH.
		if clientID := n.Metadata[metaKeyAzureClientID]; clientID != "" {
			if identity, ok := identities[clientID]; ok {
				out = append(out, linkageEdge{
					FromID:      n.ID,
					ToID:        identity.ID,
					Type:        edgeWorkloadIdentity,
					TargetGraph: familyAzure,
					Method:      "azure-workload-identity",
					Evidence:    "serviceaccount annotation " + annotationAzureClientID,
					Confidence:  0.9,
				})
			}
		}
	}
	return out
}
