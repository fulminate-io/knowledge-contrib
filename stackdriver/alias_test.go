// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// alias_test.go — the stream alias's five shapes and every link of each shape's
// own fallback chain, plus the template alias's rules.
//
// WHY THE SHAPE ROWS EXIST AT ALL: a Cloud Logging stream usually derives
// `<host>.<resource_type>`, and a module that writes that unconditionally passes
// a single-shape check while naming a whole class of streams differently from
// the layer that reads them back. Cloud Logging entries carry OPERATOR-CHOSEN
// labels, and three of them select a different shape.

func TestStreamAliasShapes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		labels map[string]string
		want   string
	}{
		// The five shapes.
		{"stackdriver", labels("resource_type", "k8s_container", "host", "pod-1", "service", "api"),
			"pod-1.k8s_container"},
		{"kubernetes wins over stackdriver via an operator's reason label",
			labels("resource_type", "k8s_container", "pod_name", "pod-1", "reason", "OOMKilled"),
			"pod-1.OOMKilled"},
		{"cloudwatch wins over stackdriver via a log_stream label",
			labels("resource_type", "gce_instance", "log_stream", "task/abc", "service", "api"),
			"task-abc.api"},
		{"loki is reachable only with no resource_type",
			labels("app", "web", "host", "h1"), "web@h1"},
		{"generic takes the first two keys alphabetically",
			labels("log_name", "stderr", "project_id", "p"), "log_name=stderr.project_id=p"},

		// The stackdriver shape's own host chain, one cell per link.
		{"stackdriver with no host at all", labels("resource_type", "k8s_container"), "k8s_container"},
		{"stackdriver falls back to pod_name",
			labels("resource_type", "k8s_container", "pod_name", "pod-9"), "pod-9.k8s_container"},
		{"stackdriver falls back to instance_id",
			labels("resource_type", "k8s_container", "instance_id", "i-3"), "i-3.k8s_container"},
		{"stackdriver falls back to service last",
			labels("resource_type", "k8s_container", "service", "api"), "api.k8s_container"},

		// The kubernetes shape's object chain.
		{"kubernetes falls back to service",
			labels("resource_type", "k8s_container", "service", "api", "reason", "OOMKilled"), "api.OOMKilled"},
		{"kubernetes falls back to related_name",
			labels("related_name", "rs-1", "reason", "OOMKilled"), "rs-1.OOMKilled"},
		{"kubernetes falls back to kind", labels("kind", "Pod", "reason", "OOMKilled"), "Pod.OOMKilled"},
		{"kubernetes with no object is one bare component",
			labels("resource_type", "k8s_container", "reason", "OOMKilled"), "OOMKilled"},

		// The cloudwatch shape, whose log_group is BOTH a trigger and a
		// fallback — the two roles produce different shapes.
		{"cloudwatch with a log_group trigger and a service",
			labels("resource_type", "k8s_container", "log_group", "/ecs/api", "service", "api"), "api"},
		{"cloudwatch with a log_group trigger and no service",
			labels("resource_type", "k8s_container", "log_group", "/ecs/api"), "ecs-api"},

		// The loki shape's instance chain.
		{"loki with no instance", labels("app", "web"), "web"},
		{"loki falls back to pod", labels("app", "web", "pod", "p-1"), "web@p-1"},

		// The generic shape.
		{"generic with one key", labels("log_name", "stderr"), "log_name=stderr"},

		// The two members of the empty-alias class.
		{"no labels at all", nil, ""},
		{"labels present but all values empty", labels("a", "", "b", ""), ""},

		// The discriminator is tested for a NON-EMPTY value, not for presence.
		// Unreachable from a collected entry, because the normalizer skips an
		// empty-valued entry label, so this is a unit arm on the deriver.
		{"an empty-valued reason does not select the kubernetes shape",
			labels("resource_type", "k8s_container", "host", "pod-1", "reason", ""), "pod-1.k8s_container"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := aliasForStream(&logStream{Labels: tc.labels})
			if got != tc.want {
				t.Errorf("aliasForStream(%v) = %q, want %q", tc.labels, got, tc.want)
			}
		})
	}
}

// TestStreamAliasPreservesCase is the cell that catches one case rule run
// through both derivers: a Kubernetes reason is what an operator searches for
// verbatim.
func TestStreamAliasPreservesCase(t *testing.T) {
	got := aliasForStream(&logStream{Labels: labels("pod_name", "API-7b6", "reason", "OOMKilled")})
	if got != "API-7b6.OOMKilled" {
		t.Fatalf("alias = %q, want the case preserved as API-7b6.OOMKilled", got)
	}
}

// TestStreamAliasFallsBackToLowCardinalityLabels covers the second source: a
// stream whose every label classified high-cardinality still has a name.
func TestStreamAliasFallsBackToLowCardinalityLabels(t *testing.T) {
	got := aliasForStream(&logStream{LowCardLabels: labels("app", "web")})
	if got != "web" {
		t.Fatalf("alias = %q, want web from the low-cardinality labels", got)
	}
}

// TestSanitizeCollapsesUnsafeRunsAndKeepsSeparatorsOut covers the component
// normalizer, including why `.` and `@` are unsafe INSIDE a component.
func TestSanitizeCollapsesUnsafeRunsAndKeepsSeparatorsOut(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"api", "api"},
		{"a_b-c:d", "a_b-c:d"},
		{"a.b", "a-b"},
		{"a@b", "a-b"},
		{"a   b", "a-b"},
		{"--a--", "a"},
		{"", ""},
		{"...", ""},
	} {
		if got := sanitize(tc.in); got != tc.want {
			t.Errorf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTemplateAliasRules covers one arm per rule rather than one per example.
func TestTemplateAliasRules(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pattern  string
		severity string
		want     string
	}{
		{"plain pattern", "Node <*> is not ready", severityWarn, "node-not-ready@warn"},
		{"reason prefix stripped", "NodeNotReady: Node is not ready", severityError, "node-not-ready@err"},
		{"seven tokens truncated to five", "one two three four five six seven", severityInfo,
			"one-two-three-four-five@info"},
		{"mixed case is lowercased", "UPPER Case Mixed Tokens", severityInfo, "upper-case-mixed-tokens@info"},
		{"punctuation splits tokens", "connect failed: dial tcp 10.0.0.1:443", severityError,
			"connect-failed-dial-tcp-10@err"},
		{"stopwords only derives nothing", "the of on for to in is and or with a an", severityInfo, ""},
		{"empty pattern derives nothing", "", severityInfo, ""},
		{"an unmapped severity lowercases into the suffix", "x", "NOTASEVERITY", "x@notaseverity"},
		{"an empty severity yields no suffix", "disk pressure", "", "disk-pressure"},
		{"wildcards separate rather than fuse tokens", "disk<*>pressure", severityInfo, "disk-pressure@info"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := templateAliasFor(&logTemplate{Pattern: tc.pattern, Severity: tc.severity})
			if got != tc.want {
				t.Errorf("templateAliasFor(%q, %s) = %q, want %q", tc.pattern, tc.severity, got, tc.want)
			}
		})
	}
	if got := templateAliasFor(nil); got != "" {
		t.Errorf("a nil template derived %q", got)
	}
}

// TestStripReasonPrefixOnlyEatsACamelCaseReason is the three-condition guard.
// Without all three it would eat the head of an ordinary sentence.
func TestStripReasonPrefixOnlyEatsACamelCaseReason(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"NodeNotReady: Node is not ready", "Node is not ready"},
		{"error: connection refused", "error: connection refused"},
		{"GET /x: 404 not found", "GET /x: 404 not found"},
		{"A: short", "A: short"},
		{": leading", ": leading"},
	} {
		if got := stripReasonPrefix(tc.in); got != tc.want {
			t.Errorf("stripReasonPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestTheTwoAliasDerivesDisagreeAboutCaseOnPurpose is the cross-check that makes
// the divergence deliberate rather than accidental: the same word survives
// verbatim through one deriver and lowercased through the other.
func TestTheTwoAliasDerivesDisagreeAboutCaseOnPurpose(t *testing.T) {
	stream := aliasForStream(&logStream{Labels: labels("pod_name", "p", "reason", "OOMKilled")})
	template := templateAliasFor(&logTemplate{Pattern: "OOMKilled container", Severity: severityError})
	if stream != "p.OOMKilled" {
		t.Errorf("the stream deriver lowercased: %q", stream)
	}
	if template != "oomkilled-container@err" {
		t.Errorf("the template deriver preserved case: %q", template)
	}
}

// TestTheCollectorEmitsAnUnsuffixedAlias pins that collision resolution is NOT
// this module's: two streams deriving the same alias both carry it unsuffixed,
// and the reading layer disambiguates because it holds the whole set.
func TestTheCollectorEmitsAnUnsuffixedAlias(t *testing.T) {
	first := newLogStream(labels("resource_type", "k8s_container", "host", "p", "team", "a"), newCardinalityTracker(0))
	second := newLogStream(labels("resource_type", "k8s_container", "host", "p", "team", "b"), newCardinalityTracker(0))
	if first.ID == second.ID {
		t.Fatalf("the fixture's two streams are the same stream")
	}
	if first.Alias != second.Alias {
		t.Fatalf("the fixture does not produce an alias collision: %q vs %q", first.Alias, second.Alias)
	}
	if first.Alias != "p.k8s_container" {
		t.Errorf("alias = %q, want the unsuffixed p.k8s_container", first.Alias)
	}
}
