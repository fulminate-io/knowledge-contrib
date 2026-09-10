// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pagination_census_test.go — EVERY CONTINUATION-BEARING SDK CALL IS INSIDE THE
// PAGINATOR, asserted against the pinned SDK rather than against a list.
//
// WHY A LIST WOULD NOT DO. The obvious version of this test is a hand-written set
// of "operations that paginate", checked against the call sites. That set is
// written by the same person who wrote the walk, from the same understanding, and
// it goes stale the moment the SDK adds a token to an operation that did not have
// one. So the question is asked of the SDK ITSELF: the pinned `<Op>Output` struct
// either declares a continuation field or it does not, and this census reads the
// answer out of the module cache the go.sum pins.
//
// THE OWNER'S RULING IS WHAT THIS ENFORCES, verbatim: "there should NOT be any
// caps, this is mcp to mcp traffic NONE of it hits the LLM. NO CAPS". A list call
// read one page deep is a cap of the worst kind, because it does not look like
// one: the account simply appears smaller than it is, and every downstream
// deletion pass treats the missing pages as resources that went away.

// continuationFields are the four names AWS uses for a continuation token across
// its services. The SDK has no common interface for them, which is precisely why
// a walk can forget one.
var continuationFields = []string{"NextToken", "NextMarker", "Marker", "Position"}

func TestCensus_EveryContinuationBearingCallIsPaginated(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)

	sites := sdkCallSites(t)
	if len(sites) == 0 {
		t.Fatal("the census found no SDK call sites at all, so it would pass on an empty walk")
	}

	var offenders []string
	var bearing, plain int
	for _, s := range sites {
		if s.SDKPackage == "" {
			t.Errorf("%s:%d calls w.clients.%s.%s and no SDK package could be resolved for it. An "+
				"unresolvable site is NOT a pass: the census cannot say whether that call paginates, and "+
				"skipping it would make an unreadable site indistinguishable from a compliant one.",
				s.File, s.Line, s.Service, s.Operation)
			continue
		}
		field, carries := continuationFieldOf(t, s.SDKPackage, s.Operation)
		if !carries {
			plain++
			continue
		}
		bearing++
		if !s.InsidePaginate {
			offenders = append(offenders, fmtOffender(s, field))
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Errorf("%d SDK call(s) whose pinned output type carries a continuation field are NOT inside a "+
			"paginate call, so each reads ONE PAGE and reports a truncated account that looks exactly like a "+
			"small one:\n  %s\n\nRoute the call through paginate, passing the token into the input and "+
			"returning the field from the nextToken function.",
			len(offenders), strings.Join(offenders, "\n  "))
	}

	// THE CENSUS DISCRIMINATES, and both halves are the control. If every
	// operation classified as continuation-bearing, the assertion above would be
	// "every SDK call is paginated", which is false of DescribeCluster and would
	// have shown up as noise; if none did, it asserts nothing at all and passes on
	// any tree. Requiring both counts positive is what makes the zero above mean
	// something.
	if bearing == 0 {
		t.Errorf("not one of the %d call sites classified as continuation-bearing. The classifier is reading "+
			"the pinned SDK wrongly — an output-type layout change, a moved module directory — and a census "+
			"that classifies nothing passes on a walk that paginates nothing.", len(sites))
	}
	if plain == 0 {
		t.Errorf("all %d call sites classified as continuation-bearing and none as plain. The classifier is "+
			"answering yes to everything, so it cannot distinguish a list call from a describe call.", len(sites))
	}
	t.Logf("census: %d SDK call sites, %d continuation-bearing, %d plain, %d unpaginated offenders",
		len(sites), bearing, plain, len(offenders))
}

// TestCensus_TheContinuationClassifierIsCalibrated is the named-pair control for
// the classifier the census above depends on.
//
// THE CENSUS'S OWN COUNTS prove the classifier discriminates in aggregate. This
// proves it gets two SPECIFIC, independently-known answers right, so a classifier
// that had drifted into some other systematic reading — matching on the wrong
// struct, or on any field whose name contains "Token" — is caught by name rather
// than by a count that still happens to be non-zero on both sides.
func TestCensus_TheContinuationClassifierIsCalibrated(t *testing.T) {
	fenceTestCacheOnPinnedModules(t)

	const (
		elbv2Pkg = "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
		ec2Pkg   = "github.com/aws/aws-sdk-go-v2/service/ec2"
		ecsPkg   = "github.com/aws/aws-sdk-go-v2/service/ecs"
	)
	for _, tc := range []struct {
		pkg       string
		operation string
		want      bool
		why       string
	}{
		{elbv2Pkg, "DescribeListeners", true, "its output carries a NextMarker, which is the defect this census was written for"},
		{ec2Pkg, "DescribeInstances", true, "a NextToken; the largest list this collector reads"},
		{elbv2Pkg, "DescribeTargetHealth", false, "a target group's health is returned whole, with no continuation of any kind"},
		{ecsPkg, "DescribeTaskDefinition", false, "one definition by name; there is nothing to continue"},
	} {
		field, got := continuationFieldOf(t, tc.pkg, tc.operation)
		if got != tc.want {
			t.Errorf("the classifier says %s.%s continuation-bearing=%v (field %q), want %v — %s.\n"+
				"The classifier reads the pinned SDK's %sOutput struct; a wrong answer here means the census "+
				"above is measuring something other than what it reports.",
				tc.pkg, tc.operation, got, field, tc.want, tc.why, tc.operation)
		}
	}
}

// fmtOffender renders one unpaginated continuation-bearing call.
func fmtOffender(s sdkCallSite, field string) string {
	return fmt.Sprintf("%s:%d w.clients.%s.%s (its %sOutput carries %s)",
		s.File, s.Line, s.Service, s.Operation, s.Operation, field)
}

// continuationFieldOf reports whether the pinned SDK's `<Operation>Output` struct
// declares a continuation field, and which one.
//
// IT READS THE GENERATED SOURCE, not documentation and not a hand-kept list. Each
// aws-sdk-go-v2 service module declares an operation's output type in
// api_op_<Operation>.go, so the answer is a field declaration in a file whose path
// is a function of the operation name.
func continuationFieldOf(t *testing.T, pkg, operation string) (string, bool) {
	t.Helper()

	dir := moduleDir(t, pkg)
	path := filepath.Join(dir, "api_op_"+operation+".go")
	body, err := os.ReadFile(path) //nolint:gosec // a path composed from a pinned module dir and an operation name
	if err != nil {
		// A MISSING FILE IS NOT A "NO". The operation is called by this package,
		// so its declaration exists somewhere; not finding it means the census
		// looked in the wrong place, and answering "does not paginate" there would
		// turn every future miss into a silent pass.
		t.Fatalf("read the pinned declaration of %s.%s at %s: %v.\nThe census cannot classify an operation "+
			"whose generated source it cannot open, and treating that as 'no continuation field' would make "+
			"a wrong path indistinguishable from a non-paginated call.", pkg, operation, path, err)
	}

	// THE OUTPUT STRUCT ONLY. The input struct of a paginated operation carries
	// the REQUEST-side token under the same names, so scanning the whole file
	// would classify every paginated operation twice and, worse, would classify an
	// operation as paginated on the strength of its input alone.
	block, ok := structBody(string(body), operation+"Output")
	if !ok {
		t.Fatalf("no %sOutput struct found in %s; the generated layout this census reads has changed",
			operation, path)
	}
	for _, field := range continuationFields {
		if declaresField(block, field) {
			return field, true
		}
	}
	return "", false
}

// structBody returns the body of `type <name> struct { ... }` from Go source.
func structBody(src, name string) (string, bool) {
	head := "type " + name + " struct {"
	_, after, ok := strings.Cut(src, head)
	if !ok {
		return "", false
	}
	rest := after
	// The generated types are flat: the first line consisting of a lone closing
	// brace ends the struct.
	if before, _, ok := strings.Cut(rest, "\n}"); ok {
		return before, true
	}
	return "", false
}

// declaresField reports whether a struct body declares a field of the given name,
// matched as a whole leading identifier rather than as a substring.
//
// THE WHOLE-IDENTIFIER MATCH IS LOAD-BEARING: "NextToken" appears inside
// "NextTokenValue" and inside a doc comment sentence, and a substring match would
// classify an operation as paginated on the strength of prose about one.
func declaresField(block, name string) bool {
	for line := range strings.SplitSeq(block, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) >= 2 && fields[0] == name {
			return true
		}
	}
	return false
}
