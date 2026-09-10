// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"
	"time"
)

// cluster_cap_test.go — the MaxClusters overflow class, which the prefill's
// input-class list omitted and the parity target's own suite covers.
//
// Past the cap a message that would have created a template merges into its best
// GLOBAL match, or into the most recently created cluster when nothing matches.
// No entry is dropped, so this is not a size cap: what it changes is template
// attribution, and through the template id every chunk id under it.

// TestTheClusterCapDivertsRatherThanDropping is the overflow class, absent from
// the prefill's list and present in the parity target's suite.
//
// WHAT THE BRANCH DECIDES. Past MaxClusters, a message that would have created a
// template instead merges into its best GLOBAL match, or — when nothing scores
// above the threshold — into the MOST RECENTLY CREATED cluster. No entry is
// dropped, so this is not a size cap; what it changes is template attribution,
// and through the template id every chunk id under it, silently, with the walk
// still asserting completeness.
func TestTheClusterCapDivertsRatherThanDropping(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 3
	d := NewDrainEngine(cfg)

	// Five shapes distinct enough that each would otherwise create its own
	// cluster: different leading tokens put them in different tree branches.
	shapes := []string{
		"alpha service started",
		"bravo cache evicted",
		"charlie queue drained",
		"delta socket closed",
		"echo worker retired",
	}
	for i, m := range shapes {
		if tpl := d.AddMessage(msg(m, time.Duration(i)*time.Second)); tpl == nil {
			t.Fatalf("message %d (%q) produced NO template; the cap must divert, never drop", i, m)
		}
	}

	templates := d.Templates()
	if len(templates) != cfg.MaxClusters {
		t.Fatalf("templates = %d, want exactly the cap %d", len(templates), cfg.MaxClusters)
	}

	// EVERY MESSAGE STILL RESOLVES to one of the surviving templates, which is
	// what "diverts rather than drops" means: the counts across the survivors
	// sum to the number of messages fed in.
	byID := templatesByID(templates)
	total := 0
	for _, tmpl := range templates {
		total += tmpl.Count
	}
	if total != len(shapes) {
		t.Fatalf("the surviving templates account for %d of %d messages; the cap dropped %d",
			total, len(shapes), len(shapes)-total)
	}
	if len(byID) != cfg.MaxClusters {
		t.Fatalf("the surviving templates carry %d distinct ids for %d clusters", len(byID), cfg.MaxClusters)
	}
}

// TestTheClusterCapFallsBackToTheMostRecentlyCreatedCluster pins WHICH cluster
// takes an overflow message that matches none of them.
//
// The five shapes below share no token, so similarity scores zero against every
// existing cluster and the global search returns nothing. The fallback is then
// the last cluster CREATED, not the first: consolidators merge forward and the
// newest cluster is the one a burst is most likely to belong to.
func TestTheClusterCapFallsBackToTheMostRecentlyCreatedCluster(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 2
	d := NewDrainEngine(cfg)

	d.AddMessage(msg("alpha service started", 0))
	newest := d.AddMessage(msg("bravo cache evicted", time.Second))
	if len(d.Templates()) != 2 {
		t.Fatalf("the fixture did not fill the cap: %d templates", len(d.Templates()))
	}
	newestID := newest.ID

	// A shape sharing nothing with either cluster, so no global match scores
	// above the threshold.
	overflowed := d.AddMessage(msg("zulu quarantine lifted", 2*time.Second))
	if overflowed == nil {
		t.Fatal("the overflow message produced no template")
	}
	if overflowed != newest {
		t.Fatalf("the overflow message landed in the template %q; want the most recently created cluster %q",
			overflowed.Pattern, newest.Pattern)
	}
	if newest.Count != 2 {
		t.Fatalf("the most recent cluster's count = %d, want 2 after absorbing the overflow", newest.Count)
	}
	// Its id moved, because the merge broadened its pattern — which is the
	// consequence that reaches every chunk keyed on it.
	if newest.ID == newestID {
		t.Fatalf("the absorbing cluster's id stayed %q though its pattern is now %q", newestID, newest.Pattern)
	}
}

// TestTheClusterCapPrefersAGlobalMatchOverTheFallback is the fallback's
// control: when an overflow message DOES resemble an existing cluster, that
// cluster takes it rather than the most recent one.
//
// THE FIXTURE HAS TO KEEP THE RESEMBLING CLUSTER OUT OF REACH OF THE NORMAL
// PATH, and getting that wrong is how an earlier version of this cell observed
// nothing. AddMessage consults findMatchingCluster BEFORE the cap, and that
// search looks only inside the LEAF the message routes to; a third message that
// shares its first three tokens with an existing cluster lands in that leaf and
// is absorbed by the ordinary similarity merge, so the assertion below would be
// true of the normal path and would say nothing about the cap at all.
//
// So the overflow message differs at token 0, which routes it to an EMPTY leaf
// and makes findMatchingCluster return nil, while keeping the same token count
// and three of four tokens in common with the first cluster — a global
// similarity of 0.75, well above the 0.4 threshold. The second cluster carries a
// different token count, so the length check excludes it from the global search
// and leaves it as the fallback and nothing else.
func TestTheClusterCapPrefersAGlobalMatchOverTheFallback(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 2
	d := NewDrainEngine(cfg)

	// Four tokens, reachable by the overflow message's global search.
	resembled := d.AddMessage(msg("alpha service started ok", 0))
	// THREE tokens, so findBestGlobalMatch's length check skips it: it can only
	// ever be the fallback, which is what makes the two outcomes distinguishable.
	fallback := d.AddMessage(msg("bravo cache evicted", time.Second))
	if len(d.Templates()) != 2 {
		t.Fatalf("the fixture did not fill the cap: %d templates", len(d.Templates()))
	}
	if resembled == fallback {
		t.Fatal("the two capped messages merged; the fixture needs two distinct clusters")
	}

	// Four tokens differing from the first cluster only at token 0: a different
	// prefix path, so the leaf is empty and the cap is reached.
	got := d.AddMessage(msg("zulu service started ok", 2*time.Second))
	if got == nil {
		t.Fatal("the overflow message produced no template")
	}
	if got == fallback {
		t.Fatalf("the overflow message took the FALLBACK %q though a global match scored above the threshold",
			fallback.Pattern)
	}
	if got != resembled {
		t.Fatalf("the overflow message landed in %q, want the cluster it resembles (%q)", got.Pattern, resembled.Pattern)
	}

	// It really was a MERGE into that cluster, not a third one: the cap held,
	// the resembling cluster's count rose, and its pattern widened at the token
	// the two disagree on.
	if len(d.Templates()) != cfg.MaxClusters {
		t.Fatalf("templates = %d, want the cap %d", len(d.Templates()), cfg.MaxClusters)
	}
	if resembled.Count != 2 {
		t.Fatalf("the resembling cluster's count = %d, want 2", resembled.Count)
	}
	if want := "<*> service started ok"; resembled.Pattern != want {
		t.Fatalf("the resembling cluster's pattern = %q, want %q", resembled.Pattern, want)
	}
	if fallback.Count != 1 {
		t.Fatalf("the fallback cluster absorbed the message: count = %d, want 1", fallback.Count)
	}
}

// TestTheOverflowMessageWithNoGlobalMatchTakesTheFallback is the other half of
// the discrimination, on the SAME fixture shape: an overflow message that
// resembles neither capped cluster falls back to the most recently created one.
// Without this pair, either cell alone would pass on an engine that always chose
// the same target.
func TestTheOverflowMessageWithNoGlobalMatchTakesTheFallback(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 2
	d := NewDrainEngine(cfg)

	d.AddMessage(msg("alpha service started ok", 0))
	fallback := d.AddMessage(msg("bravo cache evicted", time.Second))

	// Four tokens, so the length check admits it to the global search against
	// the first cluster, and it shares NOTHING with it: similarity zero, below
	// the threshold, so the search returns nil and the fallback takes it.
	got := d.AddMessage(msg("zulu quarantine lifted early", 2*time.Second))
	if got == nil {
		t.Fatal("the overflow message produced no template")
	}
	if got != fallback {
		t.Fatalf("the overflow message landed in %q, want the most recently created cluster %q",
			got.Pattern, fallback.Pattern)
	}
}
