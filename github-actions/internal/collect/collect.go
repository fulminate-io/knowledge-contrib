// SPDX-License-Identifier: Apache-2.0

// Package collect holds this collector's ENUMERATIONS: one per resource family,
// each pairing a narrow list interface over the provider SDK with pure
// converters from what the API returned to what the graph carries.
//
// THE SPLIT IS WHAT MAKES SEVEN ENUMERATIONS TESTABLE WITHOUT A NETWORK. The
// two interfaces in api.go are implemented in production by the SDK's own
// service values and in tests by recorded responses built from the SDK's own
// types, so every behavior worth asserting — the id spelling, the resource type,
// the metadata, the edges and their endpoints, the pagination boundary, the caps
// and the refusal arms — is exercised offline with no credential.
//
// A REFUSED READ IS NEITHER A FAILED WALK NOR AN EMPTY ONE, and keeping those
// three apart is this package's most consequential job.
//
// An organization where the token cannot read one repository's secrets is the
// ordinary case, so failing the whole collect over it would make the collector
// unusable. But reporting it as an EMPTY read is worse than useless: "this
// repository has no secrets" and "I was not allowed to look" are the same empty
// list, and a walk that cannot tell them apart asserts COMPLETE for both. A
// complete walk is what lets the receiving server treat everything the walk did
// not carry as gone, so a permission revoked between two collects would silently
// delete a repository's whole secret inventory and the collect would report
// success.
//
// So a refused or failed per-repository read comes back as [ErrDenied] or
// [ErrPartial], distinguishable from both: everything that WAS read is still
// returned, every other repository is still walked, and the walk that contains
// it is INCOMPLETE and names what it missed.
package collect

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// The two ways a read can come back having NOT seen what it asked for. Both are
// distinct from a clean failure of the whole enumeration and from an empty
// answer, and a caller classifies with errors.Is.
//
// They are distinct VALUES rather than log lines because the difference decides
// whether the walk may assert completeness, and a log line decides nothing. This
// is the one place this collector departs from the source provider's behavior
// rather than reproducing it: that provider logs nine such sites and continues
// with its walk still asserting complete.
var (
	// ErrDenied marks a read the provider REFUSED — an unauthenticated or
	// forbidden answer, which is about this credential and is fixed by granting
	// a scope or a membership.
	ErrDenied = errors.New("the provider refused this read")

	// ErrPartial marks a read that FAILED for any other reason: a repository or
	// an environment the API did not return, a transport failure, a malformed
	// answer. It is not a weaker ErrDenied — an operator fixes it differently —
	// and what the two share is the only thing the walk cares about: this run
	// did not see part of the organization, so it may not claim it saw all of it.
	ErrPartial = errors.New("the provider did not return part of this read")
)

// Subcollector is one named enumeration in the walk. The name is what an
// operator reads in a diagnostic and what the incomplete-walk reason carries.
type Subcollector struct {
	// Name identifies the enumeration, in the source provider's own spelling.
	Name string
	// Run enumerates one organization. It returns everything it read even when
	// it also returns an incompleteness error.
	Run func(ctx context.Context, org string) (ghgraph.Result, error)
}

// IsPermissionDenied reports whether an error is the provider refusing access.
//
// IT READS THE HTTP STATUS RATHER THAN THE MESSAGE, on the SDK's own error type.
// A refusal and an outage arrive on the same call and differ only in the status,
// and a classifier that matched on message text would turn an outage into a
// permanent "grant a role" instruction the operator cannot act on.
func IsPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	var resp *gogithub.ErrorResponse
	if errors.As(err, &resp) && resp.Response != nil {
		return resp.Response.StatusCode == http.StatusUnauthorized ||
			resp.Response.StatusCode == http.StatusForbidden
	}
	return false
}

// reads accumulates the per-scope failures one enumeration survived, keeping a
// refusal apart from every other failure.
//
// IT IS A TYPE RATHER THAN TWO SLICES PASSED AROUND because the classification
// and the message have to stay together: a subcollector that recorded a scope in
// one list and named it in the other would report the wrong remedy for a real
// failure, which is worse than reporting none.
type reads struct {
	denied []string
	failed []string
}

// record classifies one scope's failure. scope is what was being read — a
// repository's full name, an environment, the organization itself — so the
// reason a walk carries names something the operator can go and look at.
func (r *reads) record(scope string, err error) {
	if err == nil {
		return
	}
	entry := fmt.Sprintf("%s: %v", scope, err)
	if IsPermissionDenied(err) {
		r.denied = append(r.denied, entry)
		return
	}
	r.failed = append(r.failed, entry)
}

// clean reports whether every read succeeded.
func (r *reads) clean() bool { return len(r.denied) == 0 && len(r.failed) == 0 }

// err returns the incompleteness this enumeration must report, or nil when every
// read succeeded.
//
// IT WRAPS BOTH SENTINELS WHEN BOTH CLASSES OCCURRED, rather than picking one.
// An enumeration that was refused one repository and could not reach another has
// two different things wrong with it and an operator fixes them differently, so
// errors.Is answers true for both and the message names every scope under its
// own heading.
func (r *reads) err(name string) error {
	switch {
	case r.clean():
		return nil
	case len(r.failed) == 0:
		return fmt.Errorf("%s: %w: %s", name, ErrDenied, strings.Join(r.denied, "; "))
	case len(r.denied) == 0:
		return fmt.Errorf("%s: %w: %s", name, ErrPartial, strings.Join(r.failed, "; "))
	default:
		return fmt.Errorf("%s: %w (%s) and %w (%s)",
			name, ErrDenied, strings.Join(r.denied, "; "), ErrPartial, strings.Join(r.failed, "; "))
	}
}
