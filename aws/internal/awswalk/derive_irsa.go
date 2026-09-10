// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"encoding/json"
	"strings"
)

// derive_irsa.go — WORKLOAD_IDENTITY, and USES_SA beside it.
//
// IRSA (IAM Roles for Service Accounts) is how a Kubernetes workload in an EKS
// cluster becomes an AWS principal, and it is invisible from the Kubernetes side
// alone: the binding lives in the IAM role's trust policy, as a condition on the
// cluster's OIDC issuer keyed `<issuer-host>:sub` whose value is
// `system:serviceaccount:<namespace>:<name>`.
//
// THE FROM-ENDPOINT IS A KUBERNETES NODE ID, and it is composed to the k8s
// collector's own shape rather than to anything AWS-shaped. That is the
// no-cascade rule: this collect does not spawn a Kubernetes collect, so the edge
// names an id that RESOLVES when that cluster is collected on its own and dangles
// until then. Composing an AWS-shaped id instead would make the edge resolve
// against nothing, ever.
//
// A WILDCARD SUBJECT IS A DIFFERENT CLAIM and gets a different endpoint. A trust
// condition using StringLike with `system:serviceaccount:ns:*` admits ANY service
// account in a namespace, which names no single Kubernetes object; collapsing it
// onto a concrete ServiceAccount id would assert a binding that does not exist.

// irsaSubjectPrefix is the prefix an IRSA subject condition value carries.
const irsaSubjectPrefix = "system:serviceaccount:"

// conditionSubjects extracts every `*:sub` condition value from a statement's
// Condition block.
//
// THE BLOCK IS OPERATOR to KEY to VALUE-OR-LIST, three levels deep, and the
// operator matters: StringEquals means one exact subject, StringLike admits a
// pattern. Both are read; the caller decides what the value means.
func conditionSubjects(raw json.RawMessage) map[string][]string {
	out := map[string][]string{}
	if len(raw) == 0 {
		return out
	}
	var byOperator map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &byOperator); err != nil {
		return out
	}
	// THE OPERATORS ARE READ IN A FIXED ORDER for the same reason the principal
	// keys are: a map ranged in place orders the emitted edges by Go's hash seed.
	for _, op := range []string{"StringEquals", "StringLike"} {
		keys, ok := byOperator[op]
		if !ok {
			continue
		}
		for _, key := range sortedKeys(keys) {
			if !strings.HasSuffix(key, ":sub") {
				continue
			}
			var one string
			if err := json.Unmarshal(keys[key], &one); err == nil {
				out[key] = append(out[key], one)
				continue
			}
			var many []string
			if err := json.Unmarshal(keys[key], &many); err == nil {
				out[key] = append(out[key], many...)
			}
		}
	}
	return out
}

// sortedKeys returns a map's keys in sorted order, so a caller iterating them
// produces the same sequence on every run.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// A small insertion sort rather than a slices.Sort import: the maps here hold
	// a handful of condition keys, and the cost of the dependency is the reason.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// deriveWorkloadIdentity emits WORKLOAD_IDENTITY from each bound Kubernetes
// ServiceAccount to the IAM role it assumes, and USES_SA from the EKS cluster to
// the same ServiceAccount.
//
// THE TWO EDGES ANSWER DIFFERENT QUESTIONS and neither implies the other: the
// first is "what AWS permissions does this workload have", the second is "which
// workloads in this cluster have any". A consumer asking the second from a
// cluster node cannot get there through the first, which starts at a Kubernetes
// id the cluster does not own.
func (w *walkContext) deriveWorkloadIdentity() {
	if len(w.derived.eksClusters) == 0 {
		return
	}
	for _, r := range w.derived.roles {
		doc := decodePolicy(r.assumeRolePolicy)
		for _, st := range doc.Statement {
			if !strings.EqualFold(st.Effect, "Allow") {
				continue
			}
			for _, key := range sortedKeys(conditionSubjects(st.Condition)) {
				w.emitIRSAForCondition(r.arn, key, conditionSubjects(st.Condition)[key])
			}
		}
	}
}

// emitIRSAForCondition emits the edges for one `<issuer-host>:sub` condition.
func (w *walkContext) emitIRSAForCondition(roleARN, conditionKey string, subjects []string) {
	issuerHost := strings.TrimSuffix(conditionKey, ":sub")
	cluster := w.clusterForIssuer(issuerHost)
	if cluster == "" {
		// A CONDITION NAMING AN ISSUER NO CLUSTER IN THIS ACCOUNT SERVES is not
		// an error and not an edge: the role may be bound to a cluster in another
		// account, which this walk cannot see and must not invent a node for.
		return
	}
	for _, subject := range subjects {
		ns, name, wildcard := parseIRSASubject(subject)
		if ns == "" {
			continue
		}
		var saID string
		if wildcard {
			saID = irsaWildcardID(cluster, subject)
		} else {
			saID = irsaServiceAccountID(ns, name)
		}
		w.sink.addEdge(saID, roleARN, EdgeWorkloadIdentity, map[string]string{
			"cluster": cluster,
			"subject": subject,
		})
		w.sink.addEdge(cluster, saID, EdgeUsesSA, map[string]string{"subject": subject})
	}
}

// clusterForIssuer maps an OIDC issuer host onto the EKS cluster ARN serving it.
func (w *walkContext) clusterForIssuer(issuerHost string) string {
	for _, c := range w.derived.eksClusters {
		if c.issuerHost == issuerHost {
			return c.arn
		}
	}
	return ""
}

// parseIRSASubject splits an IRSA subject into its namespace and name, and
// reports whether it is a wildcard.
//
// A SUBJECT THAT IS NOT AN IRSA SUBJECT yields an empty namespace, and the caller
// emits nothing: a trust condition on some other `:sub` claim is a different
// mechanism, and reading it as a ServiceAccount binding would invent one.
func parseIRSASubject(subject string) (namespace, name string, wildcard bool) {
	rest, ok := strings.CutPrefix(subject, irsaSubjectPrefix)
	if !ok {
		return "", "", false
	}
	ns, nm, found := strings.Cut(rest, ":")
	if !found || ns == "" {
		return "", "", false
	}
	if nm == "*" || strings.Contains(nm, "*") || ns == "*" {
		return ns, nm, true
	}
	return ns, nm, false
}
