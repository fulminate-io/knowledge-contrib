// SPDX-License-Identifier: Apache-2.0

package main

// kinds.go — the KIND CLASSIFICATIONS the walk and the derived passes share:
// which kinds are cluster-scoped, and which are workloads.
//
// These are not the same question as "which kinds does this collector
// enumerate", which vocabulary.go answers. A derived pass reads a node's
// resource_type back out of metadata and has to know whether the object it
// describes carries a namespace at all; getting that wrong mints an id with a
// namespace segment for an object that has none, and every edge naming it
// dangles.

// clusterScopedKinds is the set of enumerated kinds whose objects carry no
// namespace. Their ids are "Kind/name".
var clusterScopedKinds = map[string]bool{
	"AdminNetworkPolicy":         true,
	"BaselineAdminNetworkPolicy": true,
	"ClusterRole":                true,
	"ClusterRoleBinding":         true,
	"CustomResourceDefinition":   true,
	"GatewayClass":               true,
	"Namespace":                  true,
	"Node":                       true,
	"PersistentVolume":           true,
	"StorageClass":               true,
}

// workloadResourceTypes is the set of kinds that carry a pod template, and so
// carry container images, a service account reference and volume references.
// Every image-based and identity-based derivation reads this set.
var workloadResourceTypes = map[string]bool{
	"CronJob":     true,
	"DaemonSet":   true,
	"Deployment":  true,
	"Job":         true,
	"Pod":         true,
	"ReplicaSet":  true,
	"StatefulSet": true,
}
