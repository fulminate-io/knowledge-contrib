// SPDX-License-Identifier: Apache-2.0

package logpipe

import "testing"

// alias_stream_test.go — the stream alias deriver, on three axes.
//
// AXIS 1 is the dispatch: every arm of inferProvider, one case each, in its
// order. AXIS 2 is the component chain and its collapse, one case per link and
// one per collapse, ON ALL FOUR NAMED ARMS rather than only the Loki one — this
// is the axis an implementation written from the shape strings fails, because
// it emits a bare separator where a component is absent. AXIS 3 is the
// character rules.
//
// EVERY CASE GOES THROUGH AliasFor, never through an arm directly, so each one
// proves its arm is REACHABLE through the real dispatch. Three arms carry an
// empty-discriminator branch that the dispatch makes unreachable — aliasK8s's
// empty reason, aliasLoki's empty app, aliasStackdriver's empty resource type —
// and no case below asserts one, because a case that can only be reached by a
// direct call would be a test of dead code.

func aliasOf(labels map[string]string) string { return AliasFor(&Stream{Labels: labels}) }

// TestStreamAliasDispatchesOnLabelKeysInOrder is axis 1.
func TestStreamAliasDispatchesOnLabelKeysInOrder(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"k8s wins on reason", map[string]string{"reason": "OOMKilled", "pod_name": "api-7b6"}, "api-7b6.OOMKilled"},
		{"cloudwatch on log_stream", map[string]string{"log_stream": "ecs-task", "service": "api-server"}, "ecs-task.api-server"},
		// The leading slash of a log group name is unsafe, so it would open the
		// component with a dash; sanitize TRIMS it, which is why this reads
		// "aws-lambda-fn" and not "-aws-lambda-fn".
		{"cloudwatch on log_group", map[string]string{"log_group": "/aws/lambda/fn", "log_stream": "s1"}, "s1.aws-lambda-fn"},
		{"stackdriver on resource_type", map[string]string{"resource_type": "k8s_container", "host": "api-7b6"}, "api-7b6.k8s_container"},
		{"loki on app", map[string]string{"app": "checkout", "instance": "host-3"}, "checkout@host-3"},
		{"generic when no discriminator", map[string]string{"job": "j1", "namespace": "prod"}, "job=j1.namespace=prod"},

		// THE ORDER IS WHAT THESE ASSERT. Each set carries two discriminators,
		// and the earlier check has to win; reordering inferProvider re-aliases
		// every stream that carries both.
		{"reason beats resource_type", map[string]string{"reason": "Evicted", "resource_type": "k8s_container", "pod_name": "p1"}, "p1.Evicted"},
		{"reason beats log_group", map[string]string{"reason": "Evicted", "log_group": "g", "log_stream": "s"}, "Evicted"},
		{"log_group beats resource_type", map[string]string{"log_group": "g1", "resource_type": "gce_instance"}, "g1"},
		{"resource_type beats app", map[string]string{"resource_type": "gce_instance", "app": "checkout"}, "gce_instance"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aliasOf(tc.labels); got != tc.want {
				t.Fatalf("AliasFor(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}

// TestStreamAliasWalksEachArmsComponentChain is axis 2's first half: every link
// of every arm's firstNonEmpty chain, reached by dropping the link before it.
func TestStreamAliasWalksEachArmsComponentChain(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		// K8s object = pod_name, service, related_name, kind.
		{"k8s pod_name", map[string]string{"reason": "OOMKilled", "pod_name": "p1", "service": "s1", "related_name": "r1", "kind": "k1"}, "p1.OOMKilled"},
		{"k8s service", map[string]string{"reason": "OOMKilled", "service": "s1", "related_name": "r1", "kind": "k1"}, "s1.OOMKilled"},
		{"k8s related_name", map[string]string{"reason": "OOMKilled", "related_name": "r1", "kind": "k1"}, "r1.OOMKilled"},
		{"k8s kind", map[string]string{"reason": "OOMKilled", "kind": "k1"}, "k1.OOMKilled"},

		// Stackdriver host = host, pod_name, instance_id, service.
		{"stackdriver host", map[string]string{"resource_type": "rt", "host": "h", "pod_name": "p", "instance_id": "i", "service": "s"}, "h.rt"},
		{"stackdriver pod_name", map[string]string{"resource_type": "rt", "pod_name": "p", "instance_id": "i", "service": "s"}, "p.rt"},
		{"stackdriver instance_id", map[string]string{"resource_type": "rt", "instance_id": "i", "service": "s"}, "i.rt"},
		{"stackdriver service", map[string]string{"resource_type": "rt", "service": "s"}, "s.rt"},

		// CloudWatch svc = service, log_group.
		{"cloudwatch service", map[string]string{"log_stream": "ls", "service": "s", "log_group": "lg"}, "ls.s"},
		{"cloudwatch log_group", map[string]string{"log_stream": "ls", "log_group": "lg"}, "ls.lg"},

		// Loki instance = instance, host, pod, pod_name.
		{"loki instance", map[string]string{"app": "checkout", "instance": "host-3"}, "checkout@host-3"},
		{"loki host", map[string]string{"app": "checkout", "host": "h-9"}, "checkout@h-9"},
		{"loki pod", map[string]string{"app": "checkout", "pod": "p-1"}, "checkout@p-1"},
		{"loki pod_name", map[string]string{"app": "checkout", "pod_name": "pn-2"}, "checkout@pn-2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aliasOf(tc.labels); got != tc.want {
				t.Fatalf("AliasFor(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}

// TestStreamAliasChainTestsTheValueNotTheKey is the cell that separates a chain
// walking VALUES from one checking PRESENCE. A key present with an empty value
// must fall through to the next link.
func TestStreamAliasChainTestsTheValueNotTheKey(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"loki empty instance falls to host", map[string]string{"app": "checkout", "instance": "", "host": "h-9"}, "checkout@h-9"},
		{"k8s empty pod_name falls to service", map[string]string{"reason": "OOMKilled", "pod_name": "", "service": "s1"}, "s1.OOMKilled"},
		{"stackdriver empty host falls to pod_name", map[string]string{"resource_type": "rt", "host": "", "pod_name": "p"}, "p.rt"},
		{"cloudwatch empty service falls to log_group", map[string]string{"log_stream": "ls", "service": "", "log_group": "lg"}, "ls.lg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := aliasOf(tc.labels); got != tc.want {
				t.Fatalf("AliasFor(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}

// TestStreamAliasCollapsesToItsSinglePresentComponent is axis 2's second half
// and the counter-implementation this whole axis exists to catch. An arm
// written as an unconditional sanitize(a) + separator + sanitize(b) emits
// ".OOMKilled", ".rt", ".lg" and "ls." on exactly these cases, and passes every
// case in the two tests above while doing it.
func TestStreamAliasCollapsesToItsSinglePresentComponent(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"k8s reason alone", map[string]string{"reason": "OOMKilled"}, "OOMKilled"},
		{"stackdriver resource_type alone", map[string]string{"resource_type": "rt"}, "rt"},
		{"cloudwatch log_group alone", map[string]string{"log_group": "lg"}, "lg"},
		{"cloudwatch log_stream alone", map[string]string{"log_stream": "ls"}, "ls"},
		{"loki app alone", map[string]string{"app": "checkout"}, "checkout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aliasOf(tc.labels)
			if got != tc.want {
				t.Fatalf("AliasFor(%v) = %q, want %q", tc.labels, got, tc.want)
			}
			for _, sep := range []string{".", "@"} {
				if len(got) > 0 && (got[:1] == sep || got[len(got)-1:] == sep) {
					t.Fatalf("AliasFor(%v) = %q, which begins or ends with the reserved separator %q; "+
						"a collapsed alias carries no separator at all", tc.labels, got, sep)
				}
			}
		})
	}
}

// TestGenericAliasDropsEmptyValuedLabelsBeforeSorting is the generic arm's own
// value-not-key rule: an empty-valued label must not take one of the two slots.
func TestGenericAliasDropsEmptyValuedLabelsBeforeSorting(t *testing.T) {
	// "aaa" and "bbb" sort first but carry nothing, so the alias is built from
	// "job" and "zone".
	got := aliasOf(map[string]string{"aaa": "", "bbb": "", "job": "j1", "zone": "z1"})
	if want := "job=j1.zone=z1"; got != want {
		t.Fatalf("generic alias = %q, want %q", got, want)
	}
	if got := aliasOf(map[string]string{"aaa": "", "bbb": ""}); got != "" {
		t.Fatalf("a label set whose every value is empty must derive no alias, got %q", got)
	}
}

// TestStreamAliasCharacterRules is axis 3.
func TestStreamAliasCharacterRules(t *testing.T) {
	t.Run("unsafe runs collapse to one dash and are trimmed", func(t *testing.T) {
		got := aliasOf(map[string]string{"app": "  my  weird//name  ", "instance": "h"})
		if want := "my-weird-name@h"; got != want {
			t.Fatalf("alias = %q, want %q", got, want)
		}
	})
	t.Run("case is preserved", func(t *testing.T) {
		// OOMKilled is itself a Kubernetes reason, so this case is also the
		// dispatch discriminator arriving in its real spelling.
		if got := aliasOf(map[string]string{"reason": "OOMKilled", "pod_name": "API-7b6"}); got != "API-7b6.OOMKilled" {
			t.Fatalf("alias = %q, want %q; the stream deriver preserves case", got, "API-7b6.OOMKilled")
		}
	})
	t.Run("the reserved separators are unsafe inside a component", func(t *testing.T) {
		// A '.' or '@' inside a value would otherwise read as a component
		// boundary, so both collapse to a dash.
		if got := aliasOf(map[string]string{"app": "a.b@c", "instance": "h"}); got != "a-b-c@h" {
			t.Fatalf("alias = %q, want %q", got, "a-b-c@h")
		}
	})
	t.Run("underscore colon and digits survive", func(t *testing.T) {
		if got := aliasOf(map[string]string{"resource_type": "k8s_container:v2"}); got != "k8s_container:v2" {
			t.Fatalf("alias = %q, want %q", got, "k8s_container:v2")
		}
	})
}

// TestShortHashIsTheFirstEightHex covers the collision suffix the reading layer
// appends. This collector does not de-collide, so the rule it owes is only that
// the suffix it would produce is the documented one.
func TestShortHashIsTheFirstEightHex(t *testing.T) {
	if got := ShortHash("0123456789abcdef"); got != "01234567" {
		t.Fatalf("ShortHash = %q, want %q", got, "01234567")
	}
	if got := ShortHash("short"); got != "short" {
		t.Fatalf("an input shorter than eight is returned whole, got %q", got)
	}
}

// TestAliasForEmptyStreamSets covers the two arms that derive nothing.
func TestAliasForEmptyStreamSets(t *testing.T) {
	if got := AliasFor(nil); got != "" {
		t.Fatalf("AliasFor(nil) = %q, want the empty string", got)
	}
	if got := AliasFor(&Stream{}); got != "" {
		t.Fatalf("AliasFor over a stream with no labels = %q, want the empty string", got)
	}
	// The low-cardinality set is the fallback when the full set is absent,
	// which is the arm a stream reconstructed from a node takes.
	s := &Stream{LowCardLabels: map[string]string{"app": "checkout"}}
	if got := AliasFor(s); got != "checkout" {
		t.Fatalf("AliasFor over low-cardinality labels = %q, want %q", got, "checkout")
	}
}
