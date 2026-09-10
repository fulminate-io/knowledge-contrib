// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"sort"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// labels_test.go — the three properties the emitted key names must have, each
// asserted through the derivation that actually consumes them rather than
// against a restatement of the names.

// TestTwoPodsGetDifferentReadableNames is the discrimination requirement. It
// goes red the moment the pod key is renamed so it sorts below `container`.
func TestTwoPodsGetDifferentReadableNames(t *testing.T) {
	a := &logpipe.Stream{Labels: StreamLabels("dev", "api-1", "api", "main", "proj")}
	b := &logpipe.Stream{Labels: StreamLabels("dev", "api-2", "api", "main", "proj")}

	nameA, nameB := logpipe.AliasFor(a), logpipe.AliasFor(b)
	if nameA == "" || nameB == "" {
		t.Fatalf("a stream derived no readable name: %q, %q", nameA, nameB)
	}
	if nameA == nameB {
		t.Fatalf("two pods of one container in one namespace share the readable name %q. "+
			"The name is derived from the TWO LOWEST-SORTING label keys, so a pod key that sorts below "+
			"the container and cluster keys never reaches it — and a custom graph has no query-time "+
			"de-collision layer to repair that", nameA)
	}
}

// TestClusterKeysDoNotDisplaceThePodFromTheName is the cell that makes the
// `kube_` prefix load-bearing: with a bare `cluster` key the pair would become
// (cluster, container) and collide again.
func TestClusterKeysDoNotDisplaceThePodFromTheName(t *testing.T) {
	for _, tc := range []struct{ name, cluster, project string }{
		{"gke context", "main", "fulminate-services"},
		{"no cluster at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := logpipe.AliasFor(&logpipe.Stream{Labels: StreamLabels("dev", "api-1", "api", tc.cluster, tc.project)})
			b := logpipe.AliasFor(&logpipe.Stream{Labels: StreamLabels("dev", "api-2", "api", tc.cluster, tc.project)})
			if a == b {
				t.Fatalf("two pods share the readable name %q with cluster=%q project=%q; "+
					"the cluster keys must sort BELOW the pod key", a, tc.cluster, tc.project)
			}
		})
	}
}

// TestEmittedKeysAvoidTheProviderShapedArms — three spellings would route a
// pod-log stream through a derivation that does not describe it.
func TestEmittedKeysAvoidTheProviderShapedArms(t *testing.T) {
	labels := StreamLabels("dev", "api-1", "api", "main", "proj")
	for _, forbidden := range ForbiddenLabelKeys {
		if _, present := labels[forbidden]; present {
			t.Errorf("the emitted label set carries %q, which selects a provider-shaped alias arm", forbidden)
		}
	}
}

// TestUnstableFieldsAreNotInTheLabelSet is the stability requirement, asserted
// as an absence with the emitted set printed so the assertion is readable.
func TestUnstableFieldsAreNotInTheLabelSet(t *testing.T) {
	labels := StreamLabels("dev", "api-1", "api", "main", "proj")
	for _, unstable := range []string{
		"restart_count", "restarts", "pod_uid", "uid", "started_at", "timestamp", "stream",
	} {
		if _, present := labels[unstable]; present {
			t.Errorf("the emitted label set carries %q; a value that moves between collects splits one "+
				"source into two streams and breaks carry-forward", unstable)
		}
	}
	want := []string{LabelCluster, LabelContainer, LabelNamespace, LabelPod, LabelProject}
	got := make([]string, 0, len(labels))
	for k := range labels {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("emitted label keys are %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("emitted label keys are %v, want exactly %v", got, want)
		}
	}
}

// TestEmptyLabelValuesAreOmitted — an empty value contributes nothing to
// identity and would be skipped by the derivation anyway. Every key is covered,
// not only the two a non-GKE context leaves empty: an empty value present in
// the hash distinguishes nothing now and produces a DIFFERENT hash for the same
// source once a later collect learns the value.
func TestEmptyLabelValuesAreOmitted(t *testing.T) {
	for _, tc := range []struct {
		name                                     string
		namespace, pod, container, cluster, proj string
		absent                                   []string
		want                                     int
	}{
		{"a non-GKE context", "dev", "api-1", "api", "", "",
			[]string{LabelCluster, LabelProject}, 3},
		{"a project with no cluster", "dev", "api-1", "api", "", "proj",
			[]string{LabelCluster}, 4},
		{"an unnamed container", "dev", "api-1", "", "main", "proj",
			[]string{LabelContainer}, 4},
		{"an unnamed pod", "dev", "", "api", "main", "proj",
			[]string{LabelPod}, 4},
		{"no namespace", "", "api-1", "api", "main", "proj",
			[]string{LabelNamespace}, 4},
		{"nothing at all", "", "", "", "", "",
			[]string{LabelContainer, LabelPod, LabelNamespace, LabelCluster, LabelProject}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			labels := StreamLabels(tc.namespace, tc.pod, tc.container, tc.cluster, tc.proj)
			for _, absent := range tc.absent {
				if _, present := labels[absent]; present {
					t.Errorf("%q is present with an empty value: %v", absent, labels)
				}
			}
			if len(labels) != tc.want {
				t.Fatalf("emitted %d labels, want %d: %v", len(labels), tc.want, labels)
			}
		})
	}
}

// TestTheSameSourceHashesToTheSameStreamAcrossCollects is the identity half of
// stability, through the hash that actually decides it.
func TestTheSameSourceHashesToTheSameStreamAcrossCollects(t *testing.T) {
	first := logpipe.FingerprintLabels(StreamLabels("dev", "api-1", "api", "main", "proj"))
	second := logpipe.FingerprintLabels(StreamLabels("dev", "api-1", "api", "main", "proj"))
	if first != second {
		t.Fatal("two reads of one source produced different stream ids")
	}
	changed := logpipe.FingerprintLabels(StreamLabels("dev", "api-1", "sidecar", "main", "proj"))
	if changed == first {
		t.Fatal("two containers of one pod share a stream id")
	}
}
