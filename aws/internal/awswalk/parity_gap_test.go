// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"path/filepath"
	"slices"
	"testing"
)

// parity_gap_test.go — THE DECLARED SHORTFALL against the reference collector,
// and the two assertions that keep it honest.
//
// A COVERAGE CLAIM IS ONLY AS GOOD AS ITS SHORTFALL. The parity floor next door
// asserts that everything on the list is emitted, which stays green while the
// list itself shrinks — and shrinking the list is how a coverage claim gets
// quietly reduced to whatever was achieved. So the count is an EQUALITY, the
// missing type is DECLARED with the reason and the condition that would close it,
// and the declaration is re-measured against the pinned SDK on every run.

// TestParity_TheFloorIsFiftyThreeAndTheGapIsNamed is the EQUALITY that keeps the
// shortfall against the reference collector honest.
//
// A FLOOR ALONE CANNOT CARRY THIS. The floor asserts that everything on the list
// is emitted, which stays green while the list itself shrinks — and shrinking the
// list is exactly how a coverage claim gets quietly reduced to whatever was
// achieved. So the count is asserted for equality, the shortfall is declared, and
// the two have to add up to the reference collector's own number.
func TestParity_TheFloorIsFiftyThreeAndTheGapIsNamed(t *testing.T) {
	const wantFloor = 53
	if len(AllResourceTypes) != wantFloor {
		t.Errorf("this collector declares %d resource types, want exactly %d. Adding one is not a free "+
			"improvement and removing one is not a free simplification: this number is a coverage claim "+
			"against the reference collector's %d, so a change here is a deliberate change to that claim "+
			"and moves this assertion with it.",
			len(AllResourceTypes), wantFloor, ReferenceResourceTypeCount)
	}
	if got := len(AllResourceTypes) + len(ParityGaps); got != ReferenceResourceTypeCount {
		t.Errorf("the declared floor (%d) plus the declared gaps (%d) is %d, and the reference collector's "+
			"vocabulary is %d. Every type the reference emits is either emitted here or declared as a gap; "+
			"a type that is neither is an undeclared shortfall.",
			len(AllResourceTypes), len(ParityGaps), got, ReferenceResourceTypeCount)
	}
	for _, gap := range ParityGaps {
		if gap.ResourceType == "" || gap.Reason == "" {
			t.Errorf("parity gap %+v carries an empty type or reason; a gap with no reason is a hole in the "+
				"coverage claim rather than a declaration of one", gap)
		}
		// THE GAP MUST NOT ALSO BE EMITTED. A type that is both declared missing
		// and produced by a walk is the relabelling defect coming back.
		if slices.Contains(AllResourceTypes, gap.ResourceType) {
			t.Errorf("%q is declared as a parity gap and is also in AllResourceTypes; it is one or the other",
				gap.ResourceType)
		}
		res := runWalk(t, fixtureClients(), Params{})
		if resourceTypesIn(res)[gap.ResourceType] {
			t.Errorf("the walk emitted a node of type %q, which is declared as a parity GAP. Either the gap "+
				"closed and this declaration should go, or a walk is emitting some other object under that "+
				"label — which is the defect the declaration exists to prevent.", gap.ResourceType)
		}
	}
}

// TestParity_TheDeclaredGapIsStillGenuine reads the PINNED SDK and reds when the
// gap becomes closable.
//
// THIS IS THE HALF THAT MAKES THE DECLARATION MORE THAN A NOTE. A declared gap
// with no check against reality is permanent by construction: the SDK gains the
// operation, nobody looks, and the shortfall outlives its own cause. This asserts
// the absence that justifies each gap, with a same-run known positive so that a
// glob typo or a moved module directory cannot render as a satisfied absence.
func TestParity_TheDeclaredGapIsStillGenuine(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)

	for _, gap := range ParityGaps {
		dir := moduleDir(t, gap.SDKModule)

		matches, err := filepath.Glob(filepath.Join(dir, gap.OperationGlob))
		if err != nil {
			t.Fatalf("glob %q in %s: %v", gap.OperationGlob, dir, err)
		}
		if len(matches) > 0 {
			t.Errorf("the gap declared for %q says %s carries no matching operation, and %d file(s) now match "+
				"%q: %v.\nThe pinned SDK has gained what the gap was waiting on. Implement the walk, add the "+
				"type back to AllResourceTypes, and delete the gap.",
				gap.ResourceType, gap.SDKModule, len(matches), gap.OperationGlob, matches)
		}

		// THE KNOWN POSITIVE, in the same directory through the same glob call.
		// Without it a wrong module directory, an unreadable path or a glob that
		// matches nothing by construction all render identically to a genuine
		// absence — and every one of them would report the gap as still open
		// forever.
		control, err := filepath.Glob(filepath.Join(dir, "api_op_*.go"))
		if err != nil {
			t.Fatalf("control glob in %s: %v", dir, err)
		}
		if len(control) == 0 {
			t.Fatalf("the control glob api_op_*.go matched nothing in %s, so the absence above was measured "+
				"through an instrument that finds nothing at all", dir)
		}
	}
}
