// SPDX-License-Identifier: Apache-2.0

// Package collect holds this collector's ENUMERATIONS: one per resource family,
// each pairing a paginated read of the provider's REST API with pure converters
// from what the API returned to what the graph carries.
//
// A REFUSED READ IS NEITHER A FAILED WALK NOR AN EMPTY ONE, and keeping those
// three apart is this package's most consequential job.
//
// A workspace where the credential cannot read one repository's variables is the
// ordinary case, so failing the whole collect over it would make the collector
// unusable. But reporting it as an EMPTY read is worse than useless: "this
// repository has no variables" and "I was not allowed to look" are the same
// empty list, and a walk that cannot tell them apart asserts COMPLETE for both.
// A complete walk is what lets the receiving server treat everything the walk did
// not carry as gone, so a permission revoked between two collects would silently
// delete a repository's whole variable inventory and the collect would report
// success.
//
// THIS IS THE ONE PLACE THIS COLLECTOR DEPARTS FROM THE SOURCE PROVIDER'S
// BEHAVIOR rather than reproducing it. That provider drops a refused or failed
// per-repository read at NINE separate sites — eight of them logged at debug
// level and one discarded with no log at all — and continues with its walk still
// asserting it saw the whole workspace. Every one of those nine reaches this
// walk's completeness verdict instead.
package collect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// The three ways a read can come back having NOT seen what it asked for. All
// three are distinct from a clean failure of the whole enumeration and from an
// empty answer, and a caller classifies with errors.Is.
//
// They are distinct VALUES rather than log lines because the difference decides
// whether the walk may assert completeness, and a log line decides nothing.
var (
	// ErrDenied marks a read the provider REFUSED — a 401 or a 403, which is
	// about this credential and is fixed by granting the app password a scope or
	// the account a membership.
	ErrDenied = errors.New("the provider refused this read")

	// ErrRateLimited marks a read the provider RATE-LIMITED and kept rate-limiting
	// after the client's whole retry budget. It is its own class because it is
	// neither of the other two and an operator fixes it differently: by waiting,
	// by collecting less often, or by lowering the history depth. This provider is
	// the only one of the three CI/CD providers with a retry at all, so this arm
	// has no counterpart in a sibling collector.
	ErrRateLimited = errors.New("the provider rate-limited this read")

	// ErrPartial marks a read that FAILED for any other reason: a 5xx, a transport
	// failure, the client's own timeout, a malformed answer, a repository the API
	// did not return. It is not a weaker ErrDenied — an operator fixes it
	// differently — and what the three share is the only thing the walk cares
	// about: this run did not see part of the workspace, so it may not claim it
	// saw all of it.
	ErrPartial = errors.New("the provider did not return part of this read")
)

// Subcollector is one named enumeration in the walk. The name is what an
// operator reads in a diagnostic and what the incomplete-walk reason carries.
type Subcollector struct {
	// Name identifies the enumeration, in the source provider's own spelling.
	Name string
	// Run enumerates one workspace. It returns everything it read even when it
	// also returns an incompleteness error.
	Run func(ctx context.Context, workspace string) (bbgraph.Result, error)
}

// IsRefused reports whether an error is the provider refusing access.
//
// IT READS THE HTTP STATUS RATHER THAN THE MESSAGE. A refusal, a rate limit and
// an outage arrive on the same call and differ only in the status; a classifier
// that matched on message text would turn an outage into a permanent "grant a
// permission" instruction the operator cannot act on.
func IsRefused(err error) bool {
	return hasStatus(err, http.StatusUnauthorized, http.StatusForbidden)
}

// IsRateLimited reports whether an error is the provider still rate-limiting
// after the client's whole retry budget.
//
// REACHING THIS AT ALL MEANS THE RETRY WAS EXHAUSTED. The client answers a 429
// by retrying and only returns one after its final unconditional attempt, so a
// 429 arriving here is the fourth in a row rather than the first.
func IsRateLimited(err error) bool {
	return hasStatus(err, http.StatusTooManyRequests)
}

// hasStatus reports whether err is an API error carrying one of these statuses.
func hasStatus(err error, statuses ...int) bool {
	if err == nil {
		return false
	}
	var apiErr *bbclient.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return slices.Contains(statuses, apiErr.StatusCode)
}

// IsNotFound reports whether an error is the provider answering 404.
//
// WHAT A 404 MEANS DEPENDS ON WHAT WAS BEING READ, and this package has exactly
// two kinds of read. A FILE — a repository's bitbucket-pipelines.yml under
// `src/<branch>/` — answers 404 when the file is not there, which is the
// ordinary case for a repository with no pipeline, and [repoPipelines] reads it
// as the true empty it is. A LISTING answers 404 when the URL is not there;
// Bitbucket reports an unconfigured feature by answering the listing 200 with an
// empty `values` array, so a 404 on one is a read this walk could not make. See
// [notFoundOnAListing].
//
// THE OTHER READING SHIPPED HERE AND COST REAL DATA. Every listing 404 used to
// convert to a clean empty, so the repository-scope variables read — composed
// with a path segment Bitbucket answers 404 for — returned nothing, reported
// success, and asserted the walk had seen the whole scope. No collect ever
// carried a repository-scope variable and no envelope ever said so.
func IsNotFound(err error) bool { return hasStatus(err, http.StatusNotFound) }

// notFoundOnAListing is the incompleteness a 404 on a paginated LISTING is.
//
// THE PATH IS SPLICED INTO THE MESSAGE BECAUSE IT IS NOWHERE ELSE.
// [bbclient.APIError] carries a status and a response body and no URL, so an
// operator handed a bare "404" cannot go and curl the read that produced it —
// and a wrong path segment is indistinguishable from a missing repository until
// they can. It is deliberately the one read whose reason quotes a URL.
//
// IT IS THE PARTIAL CLASS RATHER THAN A CLASS OF ITS OWN. An operator fixes a
// refusal by granting a permission and a rate limit by waiting; a 404 on a
// listing, like a 5xx or a dropped connection, is fixed by investigating what
// the walk asked for. Wrapping the provider's own error keeps the status
// available to [IsNotFound] for anything that needs to tell it apart later.
func notFoundOnAListing(subject, path string, err error) error {
	return fmt.Errorf("reading %s: the provider answered 404 for %s. A scope with nothing "+
		"configured in it answers with an empty page, so this is a listing the walk could not "+
		"read rather than an empty one: %w", subject, path, err)
}

// reads accumulates the per-scope failures one enumeration survived, keeping the
// three classes apart.
//
// IT IS A TYPE RATHER THAN THREE SLICES PASSED AROUND because the classification
// and the message have to stay together: a subcollector that recorded a scope in
// one list and named it in another would report the wrong remedy for a real
// failure, which is worse than reporting none.
type reads struct {
	denied      []string
	rateLimited []string
	failed      []string
}

// record classifies one scope's failure. scope is what was being read — a
// repository slug, an environment, the workspace itself — so the reason a walk
// carries names something the operator can go and look at.
func (r *reads) record(scope string, err error) {
	if err == nil {
		return
	}
	entry := fmt.Sprintf("%s: %v", scope, err)
	switch {
	case IsRefused(err):
		r.denied = append(r.denied, entry)
	case IsRateLimited(err):
		r.rateLimited = append(r.rateLimited, entry)
	default:
		r.failed = append(r.failed, entry)
	}
}

// clean reports whether every read succeeded.
func (r *reads) clean() bool {
	return len(r.denied) == 0 && len(r.rateLimited) == 0 && len(r.failed) == 0
}

// err returns the incompleteness this enumeration must report, or nil when every
// read succeeded.
//
// IT WRAPS EVERY SENTINEL WHOSE CLASS OCCURRED, rather than picking one. An
// enumeration that was refused one repository, rate-limited on another and
// unable to reach a third has three different things wrong with it and an
// operator fixes them differently, so errors.Is answers true for each and the
// message names every scope under its own heading.
func (r *reads) err(name string) error {
	if r.clean() {
		return nil
	}
	// The enumeration NAME is an argument rather than part of the format string:
	// a name spliced into the format would be interpreted as verbs if it ever
	// carried a percent sign.
	args := []any{name}
	clauses := []string{}
	for _, class := range []struct {
		sentinel error
		scopes   []string
	}{
		{ErrDenied, r.denied},
		{ErrRateLimited, r.rateLimited},
		{ErrPartial, r.failed},
	} {
		if len(class.scopes) == 0 {
			continue
		}
		clauses = append(clauses, "%w (%s)")
		args = append(args, class.sentinel, strings.Join(class.scopes, "; "))
	}
	return fmt.Errorf("%s: "+strings.Join(clauses, " and "), args...)
}
