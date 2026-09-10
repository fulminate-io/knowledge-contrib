// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"strings"
	"testing"
)

// variables_path_test.go — THE TWO REPOSITORY-SCOPE LISTING PATHS, PINNED IN
// BOTH SPELLINGS.
//
// A DEFECT FOUND LIVE, AND THE REASON THE REST OF THIS SUITE COULD NOT SEE IT.
// The repository-scope variables read was composed with `pipelines-config`, the
// spelling its workspace-scope sibling and both runner reads use. Bitbucket
// serves the repository-scope VARIABLES listing only at `pipelines_config`, so
// every repository-scope variable in every workspace was missing from every
// graph this collector ever produced — and the collect reported success, because
// a 404 on a listing was read as a scope with nothing in it.
//
// EVERY OTHER TEST IN THIS PACKAGE AGREED WITH THE DEFECT BY CONSTRUCTION. The
// fixture was keyed on the module's own path strings, so the declaration and the
// recorded provider were the same claim written twice; the parity floor counted
// Variable as a covered resource type off a walk whose repository scope had
// silently answered nothing. A row that asserts a node exists cannot catch this.
// The two rows below assert the REQUESTS: which URL the walk asked for, and
// which one it must never ask for.
//
// THE PAIR IS THE POINT. A row pinning only the variables spelling would pass
// for a change that rewrote every `pipelines-config` in the module to
// `pipelines_config`, which would break the runners read that measurably needs
// the hyphen. The asymmetry is Bitbucket's own; each row is the other's control.

// TestTheRepositoryVariablesListingIsReadAtPipelinesConfigWithAnUnderscore is
// the defect's own row.
func TestTheRepositoryVariablesListingIsReadAtPipelinesConfigWithAnUnderscore(t *testing.T) {
	fixture := newFixture(t)

	got, err := runOne(t, fixture, "bitbucket-variables")
	if err != nil {
		t.Fatalf("the variables enumeration over the recorded workspace: %v", err)
	}

	// THE URL THE WALK ASKED FOR. The fixture serves the repository's variables
	// at the underscore and answers 404 at the hyphen, exactly as the provider
	// does, so a walk asking for the wrong one reads nothing.
	served := repoVariablesPath(fixtureAPIRepo)
	refused := replaceSegment(served, variablesSegment, wrongVariablesSegment)
	if fixture.requests(served) == 0 {
		t.Errorf("the walk never requested %q, which is the only path Bitbucket serves the "+
			"repository's pipeline variables at", served)
	}
	if n := fixture.requests(refused); n != 0 {
		t.Errorf("the walk requested %q %d times. Bitbucket answers that path 404: the hyphen is "+
			"the RUNNERS spelling, and reading the variables through it is why no repository-scope "+
			"variable ever reached the graph", refused, n)
	}

	// AND THE VARIABLE REALLY LANDED. Without this the two request assertions
	// would hold for a walk that read the right URL and dropped what it returned.
	for _, id := range []string{
		"bitbucket:acme/Variable/repository/api/API_KEY",
		"bitbucket:acme/Variable/repository/api/DEPLOY_KEY",
	} {
		if _, ok := resourceIDs(got)[id]; !ok {
			t.Errorf("the repository-scope variable %q is missing from the walk", id)
		}
	}
}

// TestTheRepositoryRunnersListingKeepsTheHyphen is the row above's control, and
// the reason this fix is one path rather than a normalization.
//
// MEASURED, NOT ASSUMED: the same credential in the same run read
// `pipelines-config/runners` at 200 and `pipelines_config/runners` at 404. Two
// adjacent repository-scope endpoints, opposite spellings.
func TestTheRepositoryRunnersListingKeepsTheHyphen(t *testing.T) {
	fixture := newFixture(t)

	got, err := runOne(t, fixture, "bitbucket-runners")
	if err != nil {
		t.Fatalf("the runners enumeration over the recorded workspace: %v", err)
	}

	served := repoRunnersPath(fixtureAPIRepo)
	refused := replaceSegment(served, runnersSegment, wrongRunnersSegment)
	if fixture.requests(served) == 0 {
		t.Errorf("the walk never requested %q, which is the only path Bitbucket serves the "+
			"repository's runners at", served)
	}
	if n := fixture.requests(refused); n != 0 {
		t.Errorf("the walk requested %q %d times. Bitbucket answers that path 404: normalizing "+
			"every path segment to the underscore would fix the variables read and break this one",
			refused, n)
	}
	if _, ok := resourceIDs(got)["bitbucket:acme/Runner/{runner-api}"]; !ok {
		t.Error("the repository-scope runner is missing from the walk")
	}
}

// replaceSegment swaps one path segment for another, so a row can name the
// spelling the walk must NOT ask for without writing the whole URL out twice and
// letting the two drift.
func replaceSegment(path, from, to string) string {
	return strings.Replace(path, "/"+from+"/", "/"+to+"/", 1)
}
