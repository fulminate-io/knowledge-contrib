// SPDX-License-Identifier: Apache-2.0

package k8slogs

// labels.go — THE EMITTED LABEL KEY NAMES, AND WHY THEY ARE THESE NAMES.
//
// The label set is not decoration. It decides three separate things and the
// three pull against each other:
//
//   - STREAM IDENTITY is a hash of the WHOLE set, so a key whose value moves
//     between two collects of the same source splits one stream into two and
//     breaks the graph's carry-forward.
//   - THE SHARED LABEL NODES are the LOW-CARDINALITY half, so a key's value
//     range decides whether it becomes a queryable node or inline metadata.
//   - THE READABLE NAME — the stream node's SymbolName — is derived from the
//     TWO LOWEST-SORTING keys with non-empty values, and from nothing else.
//
// STABILITY: WHAT IS EXCLUDED AND WHY.
//
//   - RESTART COUNT is excluded. It increments under the collector, so a pod
//     that restarts between two collects becomes a second stream carrying the
//     same source's later lines. It looks safe on a healthy cluster precisely
//     because nothing has restarted yet.
//   - EVERY TIMESTAMP is excluded, for the same reason at every collect.
//   - THE POD UID is excluded, and this one is a judgement rather than an
//     obvious call. A UID is stable for a pod's whole life, and where a pod's
//     NAME is generated per rollout the UID adds nothing the name does not
//     already give. Where the name is STABLE — a StatefulSet member, which
//     keeps its name across a delete and recreate — the UID is the one field
//     that changes, so carrying it would split exactly the streams that most
//     need to stay joined.
//   - THE CONTAINER STREAM (stdout versus stderr) is excluded. It is not a
//     property of the source at all: it is a property of the READ, selected by
//     a collect parameter, so carrying it would make stream identity a function
//     of how the operator asked rather than of what was read. It is also not
//     knowable per line when both are read together, which is the default.
//
// DISCRIMINATION: WHY THE POD KEY IS NAMED `container_pod`.
//
// The readable name takes the two lowest-sorting keys BY BYTE ORDER. With the
// conventional spellings — cluster, container, namespace, pod — those two are
// `cluster` and `container`, and both are shared by every pod of one workload:
// two pods running the same image in one namespace get the SAME readable name
// while carrying different ids. The graph is correct and a human reading it
// sees one name for two sources.
//
// Renaming the pod key so it sorts into the first two is the only fix a
// collector can apply, because the derivation is the shared graph's and this
// module does not own it. `container_pod` sorts immediately after `container`
// — a prefix always sorts before an extension of itself — so the two lowest
// keys become `container` and `container_pod` whatever else is present, and the
// name reads as what it is: the pod this container runs in. The remaining keys
// are spelled so they sort BELOW it, which is why the cluster keys carry the
// `kube_` prefix rather than the bare `cluster` the built-in graph uses.
//
// `namespace` KEEPS ITS CONVENTIONAL SPELLING, deliberately, even though it
// sorts below the alias pair. It is the key a cloud resolver matches a log
// stream to a cloud resource on, and renaming it would silently cost this
// module its EMITTED_BY edges while every other assertion still passed.
//
// THREE SPELLINGS ARE FORBIDDEN OUTRIGHT and none appears above: `reason`,
// `app` and `resource_type` each select a provider-shaped arm of the shared
// alias derivation, so emitting one would route a pod-log stream through a
// Kubernetes-event, Loki or Stackdriver naming.
const (
	// LabelContainer is the container's name within its pod.
	LabelContainer = "container"
	// LabelPod is the pod's name. Named to sort immediately after
	// LabelContainer so the two together are the stream's readable name; see
	// this file's opening comment.
	LabelPod = "container_pod"
	// LabelNamespace is the pod's namespace. Spelled conventionally because it
	// is a cloud-resolution key.
	LabelNamespace = "namespace"
	// LabelCluster is the cluster's name, when the kubecontext encodes one.
	LabelCluster = "kube_cluster"
	// LabelProject is the cloud project the cluster belongs to, when the
	// kubecontext encodes one.
	LabelProject = "kube_project"
)

// ForbiddenLabelKeys are the keys this collector must never emit, each because
// it selects a provider-shaped arm of the shared stream-alias derivation that
// does not describe a pod log. It is exported so a test can assert the emitted
// set against it rather than restating the list.
var ForbiddenLabelKeys = []string{"reason", "app", "resource_type", "log_stream", "log_group"}

// StreamLabels builds one stream's label set.
//
// AN EMPTY VALUE IS OMITTED RATHER THAN CARRIED, and every key goes through the
// same skip so the rule has one site rather than one per optional key. An empty
// value contributes nothing to identity, and the alias derivation skips it
// anyway — so carrying it would put a key in the identity hash that distinguishes
// nothing, while a later collect that DID learn the value would produce a
// different hash for the same source.
func StreamLabels(namespace, pod, container, cluster, project string) map[string]string {
	labels := make(map[string]string, 5)
	for _, pair := range [][2]string{
		{LabelContainer, container},
		{LabelPod, pod},
		{LabelNamespace, namespace},
		{LabelCluster, cluster},
		{LabelProject, project},
	} {
		if pair[1] == "" {
			continue
		}
		labels[pair[0]] = pair[1]
	}
	return labels
}
