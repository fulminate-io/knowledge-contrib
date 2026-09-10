// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// notfound_test.go — WHAT A 404 MEANS, WHICH IS NOT THE SAME THING AT EVERY
// SITE. It is split out of completeness_test.go, whose matrix table these two
// rows range over.

// TestA404OnAListingScopeIsAnIncompleteWalkAndNamesTheURL is the 404 row for
// every site the matrix marks as a listing, and it is a correction: this suite
// used to assert the opposite of it for every site in the matrix, on a
// rationale that is true only of the file read.
//
// A LISTING THAT ANSWERS 404 IS A ROUTE THIS WALK COULD NOT READ, NOT A SCOPE
// WITH NOTHING IN IT. Bitbucket reports an unconfigured feature by answering the
// listing 200 with an empty `values` array — which is how this module's own
// recorded workspace models every unconfigured feature it carries, and what the
// live provider answered for a workspace with no workspace variables and for a
// repository with no runners. A 404 means the URL was not there: a wrong path
// segment, a scope this credential cannot address, a repository that is gone.
//
// THE COST OF THE OLD READING WAS MEASURED IN PRODUCTION. The repository-scope
// variables path was composed with the wrong segment for this module's whole
// life; every request 404'd; every collect reported a complete, successful walk
// of a workspace whose repository variables it had never once read. Nothing in
// the envelope said so, because a 404 was an empty scope.
//
// THE REASON CARRIES THE URL, which is the whole operator-facing point: the
// provider's error type carries a status and a body and no path, so the URL an
// operator would curl to see the same 404 exists only at the call site.
func TestA404OnAListingScopeIsAnIncompleteWalkAndNamesTheURL(t *testing.T) {
	for _, site := range theNineSites {
		if !site.listing {
			continue
		}
		t.Run(site.site, func(t *testing.T) {
			fixture := newFixture(t)
			fixture.status[site.path] = http.StatusNotFound

			_, err := runOne(t, fixture, site.enumeration)
			if err == nil {
				t.Fatalf("a 404 on the listing %q produced a complete read. That is how a wrong "+
					"path segment ships: the walk asks for a URL that is not there, reads "+
					"nothing, and asserts it saw the whole scope", site.path)
			}
			if !errors.Is(err, collect.ErrPartial) {
				t.Errorf("a 404 was not classified as a partial read: %v", err)
			}
			if errors.Is(err, collect.ErrDenied) || errors.Is(err, collect.ErrRateLimited) {
				t.Errorf("a 404 was reported as a refusal or a rate limit, which sends the "+
					"operator to grant a permission or wait: %v", err)
			}
			if !strings.Contains(err.Error(), site.scope) {
				t.Errorf("the reason does not name the scope %q: %v", site.scope, err)
			}
			if !strings.Contains(err.Error(), site.path) {
				t.Errorf("the reason does not carry the URL %q, so an operator cannot see WHICH "+
					"read 404'd: %v", site.path, err)
			}
			// THE ENUMERATION KEEPS READING, exactly as it does for a refusal.
			if fixture.requests(site.stillRead) == 0 {
				t.Errorf("the 404 on %q stopped the enumeration: it never read %q",
					site.path, site.stillRead)
			}
		})
	}
}

// TestA404OnTheRepositorysPipelineDefinitionIsCompleteAndEmpty is the row above's
// counterpart and its control, and it is the one site where a 404 really is a
// true reading of the repository.
//
// THE ROUTE EXISTS AND THE FILE DOES NOT. `src/<branch>/bitbucket-pipelines.yml`
// is a file read rather than a listing, and a repository with no pipeline
// definition is the ordinary case rather than the exception. Marking it
// incomplete would leave nearly every workspace permanently incomplete, and a
// permanently incomplete collect disables the receiving server's deletion phase
// forever — which is its own defect.
//
// IT IS ALSO THE SAME-RUN CONTROL for the row above: without it, a collector
// that reported every 404 as incomplete would satisfy those assertions while
// making this verdict impossible to reach.
func TestA404OnTheRepositorysPipelineDefinitionIsCompleteAndEmpty(t *testing.T) {
	var fileSites int
	for _, site := range theNineSites {
		if site.listing {
			continue
		}
		fileSites++
		t.Run(site.site, func(t *testing.T) {
			fixture := newFixture(t)
			fixture.status[site.path] = http.StatusNotFound

			got, err := runOne(t, fixture, site.enumeration)
			if err != nil {
				t.Errorf("a 404 on the file %q was reported as an incomplete read: %v",
					site.path, err)
			}
			for id := range resourceIDs(got) {
				if strings.HasPrefix(id, "bitbucket:acme/Pipeline/"+fixtureAPIRepo+"/") {
					t.Errorf("the repository whose definition answered 404 produced %q", id)
				}
			}
		})
	}
	if fileSites == 0 {
		t.Fatal("no site in the matrix is marked as a file read, so this row asserted nothing " +
			"and the listing row above has no control")
	}
}
