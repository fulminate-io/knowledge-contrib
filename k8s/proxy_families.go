// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
)

// proxy_families.go — the SIX PROXY FAMILIES, one function each, with the gate
// that suppresses each one stated at its own gate.
//
// EVERY ONE OF THE SIX DEDUPES OR GATES, and the six do it for four different
// reasons. Reading them together is how a reader sees that a family emitting
// "the right number of edges" is not the same as a family emitting the right
// EDGES: three of them can emit an edge to a WRONG id and still count
// correctly.

// === EXPOSED_BY ===

// buildExposedByEdges renders EXPOSED_BY from a Service or Ingress to the cloud
// load balancer its status names.
//
// THE DEDUPE IS THE INTERESTING PART. A Service can carry SEVERAL load-balancer
// ingress entries that resolve to the SAME load balancer — an IP and a hostname
// for one resource is the ordinary case on every cloud. Without a dedupe on
// (source, proxy) every such Service emits two identical edges, and a graph
// that counts edges reports a fan-out that does not exist.
//
// THE EVIDENCE NAMES WHICH LOOKUP KEY MATCHED, so an operator auditing a
// mis-linkage can see whether the ip or the hostname produced it without
// re-running anything.
func buildExposedByEdges(nodes []Node, acc *proxyAccumulator) []Edge {
	var out []Edge
	seen := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		kind := n.Metadata["resource_type"]
		if kind != "Service" && kind != "Ingress" && kind != "Gateway" {
			continue
		}
		addresses := n.Metadata["lb_ingress"]
		if addresses == "" {
			continue
		}
		for entry := range strings.SplitSeq(addresses, ",") {
			key, value, ok := strings.Cut(entry, "=")
			if !ok || value == "" {
				continue
			}
			proxyID, ok := acc.proxy(proxyTarget{
				ID:           value,
				ResourceType: "cloud:loadbalancer",
				SymbolName:   value,
			})
			if !ok {
				continue
			}
			dedupe := n.ID + "\x00" + proxyID
			if seen[dedupe] {
				continue
			}
			seen[dedupe] = true
			out = append(out, Edge{
				FromID:   n.ID,
				ToID:     proxyID,
				Type:     edgeExposedBy,
				Method:   key,
				Evidence: entry,
			})
		}
	}
	return out
}

// === RUNS_IN_CLUSTER ===

// buildRunsInClusterEdges renders RUNS_IN_CLUSTER from every resource to the
// managed cluster it lives in.
//
// THERE IS NO ALLOWLIST OF KINDS, AND THAT IS DELIBERATE: every resource
// depends on the control plane — Namespaces, Services, ConfigMaps, CRDs, the
// lot — so the family fans out over the whole enumeration.
//
// WHAT IT DOES HAVE IS TWO SKIPS, and both are needed. The cluster proxy must
// not run in itself, and NO proxy may gain a RUNS_IN_CLUSTER edge: a proxy
// stands for a resource in another graph, which has its own cluster
// relationship over there. A loop over the result slice without both guards
// emits a self-edge and one proxy-to-proxy edge per proxy the other five
// families minted.
//
// AN UNRESOLVABLE CONTEXT YIELDS NO CLUSTER AND ZERO EDGES. That arm is also
// what proves a fixture reached this function at all.
func buildRunsInClusterEdges(nodes []Node, contextName string, acc *proxyAccumulator) []Edge {
	cluster, ok := parseManagedCluster(contextName)
	if !ok {
		return nil
	}
	proxyID, ok := acc.proxy(cluster)
	if !ok {
		return nil
	}

	out := make([]Edge, 0, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		if n.ID == proxyID {
			continue
		}
		if n.Type == nodeTypeProxy {
			continue
		}
		out = append(out, Edge{FromID: n.ID, ToID: proxyID, Type: edgeRunsInCluster})
	}
	return out
}

// parseManagedCluster resolves a kubeconfig context name to the managed cluster
// it names, on the three providers whose context names carry one.
//
// A CONTEXT THIS DOES NOT RECOGNIZE IS NOT AN ERROR. A kubeconfig context is a
// local alias an operator chose; a self-managed cluster, or a renamed context,
// names no cloud resource, and the family simply produces nothing.
func parseManagedCluster(contextName string) (proxyTarget, bool) {
	switch {
	case strings.HasPrefix(contextName, "gke_"):
		// gke_<project>_<location>_<cluster>
		parts := strings.Split(contextName, "_")
		if len(parts) != 4 {
			return proxyTarget{}, false
		}
		project, location, name := parts[1], parts[2], parts[3]
		return proxyTarget{
			Account:      project,
			ID:           "projects/" + project + "/locations/" + location + "/clusters/" + name,
			ResourceType: "gcp:container:cluster",
			Provider:     "gcp",
			Region:       location,
			SymbolName:   name,
		}, true

	case strings.HasPrefix(contextName, "arn:aws:eks:"):
		// arn:aws:eks:<region>:<account>:cluster/<name>
		parts := strings.Split(contextName, ":")
		if len(parts) < 6 {
			return proxyTarget{}, false
		}
		region, account := parts[3], parts[4]
		name := strings.TrimPrefix(parts[5], "cluster/")
		if name == "" || account == "" {
			return proxyTarget{}, false
		}
		return proxyTarget{
			Account:      account,
			ID:           contextName,
			ResourceType: "aws:eks:cluster",
			Provider:     "aws",
			Region:       region,
			SymbolName:   name,
		}, true

	default:
		return proxyTarget{}, false
	}
}

// === BACKED_BY_VM ===

// buildBackedByVMEdges renders BACKED_BY_VM from a Node to the cloud VM its
// providerID names.
//
// THE GATE IS THAT BOTH THE ACCOUNT AND THE ID MUST PARSE OUT. A providerID
// that yields an id but no account would mint "proxy:cloud::<id>" — the
// dangling form — for a resource whose account is knowable but was not read,
// and a later pass that learns the account then finds two proxies for one VM.
// So this family requires BOTH and emits nothing otherwise, which is the
// opposite choice from the two identity families below and is deliberate: a VM
// always belongs to a determinable account, an identity binding does not.
func buildBackedByVMEdges(nodes []Node, acc *proxyAccumulator) []Edge {
	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "Node" {
			continue
		}
		target, ok := parseProviderID(n.Metadata["provider_id"])
		if !ok {
			continue
		}
		if target.Account == "" || target.ID == "" {
			continue
		}
		proxyID, ok := acc.proxy(target)
		if !ok {
			continue
		}
		out = append(out, Edge{FromID: n.ID, ToID: proxyID, Type: edgeBackedByVM})
	}
	return out
}

// parseProviderID resolves a Kubernetes providerID to the cloud VM it names.
//
//	gce://<project>/<zone>/<instance>
//	aws:///<zone>/<instance-id>
//	azure:///subscriptions/<sub>/resourceGroups/<rg>/.../virtualMachines/<name>
//
// THE AWS FORM CARRIES NO ACCOUNT, which is why this returns a target with an
// empty account for it and the caller's gate then suppresses the family. That
// is the measured behavior of the built-in collector too: on EKS it recovers
// the account from the cluster ARN, and this collector reaches the same outcome
// through the cluster proxy rather than by re-parsing a graph name.
func parseProviderID(providerID string) (proxyTarget, bool) {
	scheme, rest, ok := strings.Cut(providerID, "://")
	if !ok {
		return proxyTarget{}, false
	}
	switch scheme {
	case "gce":
		parts := strings.Split(rest, "/")
		if len(parts) != 3 {
			return proxyTarget{}, false
		}
		project, zone, instance := parts[0], parts[1], parts[2]
		return proxyTarget{
			Account:      project,
			ID:           "projects/" + project + "/zones/" + zone + "/instances/" + instance,
			ResourceType: "gcp:compute:instance",
			Provider:     "gcp",
			Region:       zone,
			SymbolName:   instance,
		}, true
	case "aws":
		parts := strings.Split(strings.TrimPrefix(rest, "/"), "/")
		if len(parts) != 2 || parts[1] == "" {
			return proxyTarget{}, false
		}
		return proxyTarget{
			ID:           parts[1],
			ResourceType: "aws:ec2:instance",
			Provider:     "aws",
			Region:       parts[0],
			SymbolName:   parts[1],
		}, true
	case "azure":
		id := "/" + strings.TrimPrefix(rest, "/")
		name := id[strings.LastIndex(id, "/")+1:]
		account := azureSubscription(id)
		if name == "" {
			return proxyTarget{}, false
		}
		return proxyTarget{
			Account:      account,
			ID:           id,
			ResourceType: "azure:compute:virtualMachine",
			Provider:     "azure",
			SymbolName:   name,
		}, true
	default:
		return proxyTarget{}, false
	}
}

// azureSubscription pulls the subscription id out of an Azure resource id,
// which is the account an Azure cloud graph is keyed by.
func azureSubscription(id string) string {
	parts := strings.Split(strings.TrimPrefix(id, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if strings.EqualFold(parts[i], "subscriptions") {
			return parts[i+1]
		}
	}
	return ""
}
