// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// phaseone_internal_test.go — WHAT THE REPOSITORIES ENUMERATION'S ERROR MEANS.
//
// The walk-level row drives the FATAL side end to end: a provider that 500s the
// listing fails the whole collect and the five later enumerations never run. The
// other side cannot be driven that way. The only incompleteness that enumeration
// can report today is a detail it could not render, and no recorded response can
// provoke one — every value it marshals is a struct of scalars. So the decision
// is asserted here, where both sides are reachable, and the walk-level row is its
// integration half.

// TestAnIncompletenessInThePhaseOneReadMarksTheWalkRatherThanEndingIt.
func TestAnIncompletenessInThePhaseOneReadMarksTheWalkRatherThanEndingIt(t *testing.T) {
	for _, sentinel := range []error{
		collect.ErrDenied, collect.ErrRateLimited, collect.ErrPartial,
	} {
		wrapped := fmt.Errorf("bitbucket-repos: %w (acme/api: rendering its detail)", sentinel)

		incomplete, fatal := phaseOneOutcome("acme", wrapped)
		if fatal != nil {
			t.Errorf("%v ended the walk: %v. The listing answered; one repository could not be "+
				"carried, and the rest of the workspace is still walkable", sentinel, fatal)
		}
		if len(incomplete) != 1 {
			t.Errorf("%v produced %d marks, want 1", sentinel, len(incomplete))
			continue
		}
		if !strings.Contains(incomplete[0], "acme/api") {
			t.Errorf("the mark does not name what was dropped: %q", incomplete[0])
		}
	}
}

// TestAFailureToListEndsTheWalk is the other side, and the same-run control for
// the rows above: the decision refuses one class rather than every class.
func TestAFailureToListEndsTheWalk(t *testing.T) {
	unclassified := errors.New("listing the workspace's repositories: bitbucket API 500: down")

	incomplete, fatal := phaseOneOutcome("acme", unclassified)
	if fatal == nil {
		t.Fatal("a walk that could not list the workspace's repositories was allowed to " +
			"continue. Five enumerations start from that list, so it would land an empty " +
			"generation the server then reconciles against")
	}
	if len(incomplete) != 0 {
		t.Errorf("the fatal arm also produced %d marks", len(incomplete))
	}
	for _, want := range []string{"acme", "could not be enumerated", "500"} {
		if !strings.Contains(fatal.Error(), want) {
			t.Errorf("the failure does not name %q: %v", want, fatal)
		}
	}
}

// TestACleanPhaseOneReadMarksNothing.
func TestACleanPhaseOneReadMarksNothing(t *testing.T) {
	incomplete, fatal := phaseOneOutcome("acme", nil)
	if fatal != nil || len(incomplete) != 0 {
		t.Errorf("a clean read produced fatal=%v marks=%v", fatal, incomplete)
	}
}

// TestThePhaseOneMarksReachTheVerdict closes the wiring, which the decision rows
// above do not: a mark the fan-out never received is a mark the completeness
// assertion never sees, and no fixture can produce one because the only
// incompleteness phase one reports today is a detail it could not render.
//
// SO THE SEED IS ASSERTED AT THE FAN-OUT. It is a PARAMETER rather than an
// append after the call precisely so a later edit cannot drop it silently, and
// this row is what says the parameter is used rather than merely accepted.
func TestThePhaseOneMarksReachTheVerdict(t *testing.T) {
	collector := Collector{}
	seed := []string{"bitbucket-repos: acme/api: rendering its detail"}

	_, incomplete, failures := collector.fanOut(
		t.Context(), 1, "acme", nil, seed)

	if len(failures) != 0 {
		t.Errorf("the seed was recorded as a failure rather than an incompleteness: %v", failures)
	}
	if len(incomplete) != 1 || incomplete[0] != seed[0] {
		t.Fatalf("the fan-out reported %v, want the seeded mark %v", incomplete, seed)
	}

	// AND IT REACHES THE ASSERTION, which is the thing that decides whether the
	// server may treat what this walk did not carry as gone.
	verdict := completeness("acme", 6, incomplete, failures)
	if verdict.IsComplete() {
		t.Fatal("a walk carrying a phase-one mark asserted it saw the whole workspace")
	}
	if !strings.Contains(verdict.Reason(), "acme/api") {
		t.Errorf("the reason does not carry the mark: %s", verdict.Reason())
	}
}
