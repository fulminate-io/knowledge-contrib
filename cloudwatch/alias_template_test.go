// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// alias_template_test.go — ONE CELL PER STEP of the template alias pipeline,
// because each step is independently wrong-able and the alias is the node's
// SymbolName.

// TestTemplateAliasPerStep covers the seven steps and the two empty arms.
func TestTemplateAliasPerStep(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pattern  string
		severity string
		want     string
	}{
		// STEP 1, the reason prefix, in five arms. The last three are the
		// near-misses that discriminate a correct implementation from one that
		// splits on the first ": ".
		{"reason prefix is stripped", "FailedMount: MountVolume SetUp failed", SeverityError, "mountvolume-setup-failed@err"},
		{"non-letter prefix is KEPT", "GET /x: 200 ok", SeverityError, "get-x-200-ok@err"},
		{
			// A single-character prefix is not stripped. The letter chosen is
			// NOT a stopword: with "A: foo bar" the prefix survives step 1 and
			// is then dropped as a stopword, so a wrong implementation that
			// strips every "X: " produces the identical alias and the cell
			// discriminates nothing.
			"single-character prefix is KEPT", "B: foo bar", SeverityError, "b-foo-bar@err",
		},
		{"lowercase-initial prefix is KEPT", "failedMount: foo bar", SeverityError, "failedmount-foo-bar@err"},
		{"a prefix at the very end is KEPT", "abc: ", SeverityError, "abc@err"},

		// STEP 2, the wildcard as a token BOUNDARY rather than a token.
		{"wildcard separates two words", "node <*> ready", SeverityWarn, "node-ready@warn"},
		{"wildcard never appears in the alias", "<*> <*> <*> restart", SeverityWarn, "restart@warn"},

		// STEP 3, the token split: punctuation splits, digits are kept.
		{"punctuation splits, digits survive", "pool.size=42 exhausted", SeverityError, "pool-size-42-exhausted@err"},

		// STEP 4, the stopwords.
		{"stopwords are dropped", "the state of the node is not ready", SeverityWarn, "state-node-not-ready@warn"},
		{"a pattern of only stopwords yields the EMPTY alias", "the of on for to in is and or with", SeverityError, ""},

		// STEP 5, the five-token cap.
		{"six meaningful tokens keep the first five", "alpha beta gamma delta epsilon zeta", SeverityInfo, "alpha-beta-gamma-delta-epsilon@info"},

		// STEP 6, the join, which LOWERCASES — the opposite of the stream
		// deriver's rule, and the single easiest place to write one rule twice.
		{"tokens are lowercased", "OOMKilled Container Restarted", SeverityError, "oomkilled-container-restarted@err"},

		// STEP 7, the severity suffix, in its three arms.
		{"canonical severity yields its short form", "node ready", SeverityCritical, "node-ready@crit"},
		{"an EMPTY severity yields no @ at all", "node ready", "", "node-ready"},
		{"a non-standard severity yields a lowercased copy", "node ready", "NOTICE_LEVEL", "node-ready@notice_level"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TemplateAliasFor(&LogTemplate{Pattern: tc.pattern, Severity: tc.severity})
			if got != tc.want {
				t.Errorf("TemplateAliasFor(%q, %q) = %q, want %q", tc.pattern, tc.severity, got, tc.want)
			}
		})
	}
}

// TestTemplateAliasForNilIsEmpty covers the nil arm, which the emission path
// reaches for a template that was never built.
func TestTemplateAliasForNilIsEmpty(t *testing.T) {
	if got := TemplateAliasFor(nil); got != "" {
		t.Errorf("TemplateAliasFor(nil) = %q, want the empty string", got)
	}
}

// TestTemplateWithNoMeaningfulTokensFallsBackToThePattern is the empty-alias
// emission cell: SymbolName becomes the PATTERN so the node stays searchable,
// and the alias metadata key is OMITTED rather than written empty.
func TestTemplateWithNoMeaningfulTokensFallsBackToThePattern(t *testing.T) {
	node := templateNode(&LogTemplate{ID: "t1", Pattern: "<*> <*>", Severity: SeverityInfo})
	if node.SymbolName != "<*> <*>" {
		t.Errorf("symbol name %q, want the pattern %q", node.SymbolName, "<*> <*>")
	}
	if v, present := node.Metadata["alias"]; present {
		t.Errorf("the alias metadata key is present as %q; an empty alias omits the key entirely", v)
	}
}

// TestTemplateAliasIsWrittenToBothSymbolNameAndMetadata is the ORDINARY case,
// which every other alias cell leaves unobserved: a module that wrote the alias
// only to metadata would satisfy both empty-alias cells vacuously and produce a
// graph the text index cannot match.
func TestTemplateAliasIsWrittenToBothSymbolNameAndMetadata(t *testing.T) {
	node := templateNode(&LogTemplate{ID: "t1", Pattern: "node ready", Severity: SeverityWarn, Alias: "node-ready@warn"})
	if node.SymbolName != "node-ready@warn" {
		t.Errorf("symbol name %q, want the alias", node.SymbolName)
	}
	if node.Metadata["alias"] != "node-ready@warn" {
		t.Errorf("alias metadata %q, want the alias", node.Metadata["alias"])
	}
}

// TestSeverityShortCoversEveryCanonicalLevel pins the suffix table, which also
// decides the node's SymbolName.
func TestSeverityShortCoversEveryCanonicalLevel(t *testing.T) {
	want := map[string]string{
		SeverityCritical: "crit", SeverityError: "err", SeverityWarn: "warn",
		SeverityInfo: "info", SeverityDebug: "debug", SeverityTrace: "trace",
	}
	for sev, short := range want {
		if got := severityShort(sev); got != short {
			t.Errorf("severityShort(%q) = %q, want %q", sev, got, short)
		}
	}
}
