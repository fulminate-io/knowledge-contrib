// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// alias_stream_test.go — the five-arm dispatch, the CloudWatch component chain,
// and the sanitize rules, each with the near-miss that discriminates it.

// TestStreamAliasDispatchTakesOneArmPerShape gives the fixture ONE STREAM PER
// REACHABLE ARM, so a reordered dispatch renames a stream and reds here instead
// of renaming every stream silently in production.
func TestStreamAliasDispatchTakesOneArmPerShape(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			// The Kubernetes arm wins even with the CloudWatch keys present.
			// This IS the CloudWatch misroute: a collector that lifted a
			// `reason` field out of a JSON body into a label renames every
			// stream, and nothing else would notice.
			"reason wins over the cloudwatch keys",
			map[string]string{"reason": "OOMKilled", "pod_name": "api-7b6", "log_stream": "s", "log_group": "/g"},
			"api-7b6.OOMKilled",
		},
		{
			"cloudwatch keys with no reason",
			map[string]string{"log_stream": "ecs-task", "log_group": "/ecs/prod/api", "service": "api"},
			"ecs-task.api",
		},
		{"resource_type only takes stackdriver", map[string]string{"resource_type": "k8s_container", "host": "h1"}, "h1.k8s_container"},
		{"app only takes loki", map[string]string{"app": "checkout", "instance": "host-3"}, "checkout@host-3"},
		{"none of those keys takes generic", map[string]string{"zone": "eu", "team": "core"}, "team=core.zone=eu"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AliasFor(&LogStream{Labels: tc.labels}); got != tc.want {
				t.Errorf("AliasFor(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}

// TestCloudWatchComponentChain covers every fallback of the two-component
// chain, including the two DANGLING-SEPARATOR cells that a guard on the
// sanitized value would get wrong.
func TestCloudWatchComponentChain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{"both components present", map[string]string{"log_stream": "ecs-task", "service": "api"}, "ecs-task.api"},
		{"log_stream absent yields the second alone", map[string]string{"log_group": "/g", "service": "api"}, "api"},
		{
			// firstNonEmpty prefers service and falls back to log_group.
			"service absent falls back to log_group",
			map[string]string{"log_stream": "s1", "log_group": "/ecs/prod"}, "s1.ecs-prod",
		},
		{
			// The fallback fires on an EMPTY VALUE too, because firstNonEmpty
			// tests the value rather than the key's presence.
			"service present but empty falls back to log_group",
			map[string]string{"log_stream": "s1", "log_group": "/ecs/prod", "service": ""}, "s1.ecs-prod",
		},
		{"service and log_group absent yields the first alone", map[string]string{"log_stream": "s1"}, "s1"},
		{
			// A log_group present with an EMPTY value does not select the
			// CloudWatch arm at all: the dispatch tests values, so this falls
			// through to generic, where an empty value is skipped.
			"an empty-valued log_group alone takes generic and yields nothing",
			map[string]string{"log_group": ""}, "",
		},
		{
			"an empty-valued log_group beside a service takes generic",
			map[string]string{"log_group": "", "service": "api-server"}, "service=api-server",
		},
		{
			// THE DANGLING SEPARATOR, both directions. The branch tests the RAW
			// values, so a component that is non-empty and sanitizes to empty
			// still takes the both-present branch.
			"a first component that sanitizes to empty leaves a leading dot",
			map[string]string{"log_stream": "...", "service": "api"}, ".api",
		},
		{
			"a second component that sanitizes to empty leaves a trailing dot",
			map[string]string{"log_stream": "ecs-task", "service": "@@@"}, "ecs-task.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := AliasFor(&LogStream{Labels: tc.labels}); got != tc.want {
				t.Errorf("AliasFor(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}

// TestSanitizeSafeSetBothDirections is the pair that discriminates the real
// safe set from the obvious one. WITHOUT THE SURVIVING HALF, every collapse
// cell passes under a `[^A-Za-z0-9]` implementation.
func TestSanitizeSafeSetBothDirections(t *testing.T) {
	t.Run("underscore and colon SURVIVE", func(t *testing.T) {
		for _, in := range []string{"a_b", "a:b", "my_function", "ecs:task"} {
			if got := sanitize(in); got != in {
				t.Errorf("sanitize(%q) = %q, want it unchanged", in, got)
			}
		}
	})
	t.Run("the reserved separators and everything else COLLAPSE", func(t *testing.T) {
		for _, r := range []string{".", "@", "/", " ", "+", "=", "*", "<", ">", "%"} {
			in := "a" + r + "b"
			if got := sanitize(in); got != "a-b" {
				t.Errorf("sanitize(%q) = %q, want %q", in, got, "a-b")
			}
		}
	})
}

// TestSanitizeCollapsesTrimsAndPreservesCase covers the three remaining rules.
func TestSanitizeCollapsesTrimsAndPreservesCase(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"a///b", "a-b"},
		{"...api...", "api"},
		{"OOMKilled", "OOMKilled"},
		{"", ""},
		{"@@@", ""},
	} {
		if got := sanitize(tc.in); got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestStreamAliasSeparatorSurvivesPerComponentSanitizing pins that sanitize is
// applied per COMPONENT: applying it to the joined alias would collapse the
// separator, since the separator is itself an unsafe rune.
func TestStreamAliasSeparatorSurvivesPerComponentSanitizing(t *testing.T) {
	got := AliasFor(&LogStream{Labels: map[string]string{"log_stream": "ecs task", "service": "api server"}})
	if got != "ecs-task.api-server" {
		t.Errorf("AliasFor = %q, want %q; the separating dot must survive", got, "ecs-task.api-server")
	}
}

// TestStreamAliasFallsBackToLowCardLabels covers the entry rule's second arm,
// and its empty case.
func TestStreamAliasFallsBackToLowCardLabels(t *testing.T) {
	got := AliasFor(&LogStream{LowCardLabels: map[string]string{"log_stream": "s1", "service": "api"}})
	if got != "s1.api" {
		t.Errorf("AliasFor with only low-card labels = %q, want %q", got, "s1.api")
	}
	if got := AliasFor(&LogStream{}); got != "" {
		t.Errorf("AliasFor with no labels at all = %q, want the empty string", got)
	}
	if got := AliasFor(nil); got != "" {
		t.Errorf("AliasFor(nil) = %q, want the empty string", got)
	}
}

// TestStreamNodeWithEmptyAliasLeavesSymbolNameEmpty is the stream counterpart
// of the template's pattern fallback — and it is DIFFERENT: a stream has no
// second readable form, so SymbolName stays empty and the key is omitted.
func TestStreamNodeWithEmptyAliasLeavesSymbolNameEmpty(t *testing.T) {
	node := streamNode(&LogStream{ID: "s1", Labels: map[string]string{"log_group": ""}, Fingerprint: "f"})
	if node.SymbolName != "" {
		t.Errorf("symbol name %q, want empty", node.SymbolName)
	}
	if v, present := node.Metadata["alias"]; present {
		t.Errorf("the alias metadata key is present as %q; an empty alias omits the key", v)
	}
}

// TestStreamAliasIsWrittenToBothSymbolNameAndMetadata is the stream's ordinary
// case, for the same reason the template has one.
func TestStreamAliasIsWrittenToBothSymbolNameAndMetadata(t *testing.T) {
	node := streamNode(&LogStream{
		ID: "s1", Fingerprint: "f",
		Labels: map[string]string{"log_stream": "ecs-task", "service": "api"},
		Alias:  "ecs-task.api",
	})
	if node.SymbolName != "ecs-task.api" || node.Metadata["alias"] != "ecs-task.api" {
		t.Errorf("symbol name %q and alias metadata %q, want both to be %q",
			node.SymbolName, node.Metadata["alias"], "ecs-task.api")
	}
}
