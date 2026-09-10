// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// describe_sensitive_test.go — the EMPTY-SENSITIVE MARK: the per-name boolean a
// collector sets when it tells a name PRESENT AND EMPTY apart from ABSENT, and
// the third line-oriented answer the installer's table generator reads.
//
// WHY THE MARK EXISTS AT ALL. A worked entry that renders `${NAME:-}` hands the
// child that name present and empty, because the reference resolves to the
// empty string in the process that serves the collect. For a collector that
// treats empty as absent, that is inert; for one that refuses the name, branches
// on its presence, or hands it to a dependency that does either, it is a broken
// collect. Only the collector knows which it is, so the collector says so here
// and the installer's documentation gate reads the answer rather than carrying a
// list of its own.
//
// WHY IT IS ITS OWN ANSWER RATHER THAN A FOURTH COLUMN ON THE CLASS TABLE. The
// class table's printed rows are read by a consumer that greps a WHOLE LINE with
// a trailing anchor, so any appended field — populated or empty — stops matching
// and every collector declaring that row takes the wrong branch. The mark
// therefore travels on its own switch, its own generated function and its own
// print arm, and the class table stays byte-identical.

// TestEmptySensitiveNames_CarriesEveryMarkedNameOfEveryClass is the row the
// not-carried class makes necessary: a marked name whose class the installer's
// entry-writing table deliberately drops must still reach the answer, because
// what the mark governs is what a DOCUMENT may show, not what an entry carries.
func TestEmptySensitiveNames_CarriesEveryMarkedNameOfEveryClass(t *testing.T) {
	decl := fixtureDeclaration()
	// The fixture's marked names, read from the fixture rather than transcribed,
	// so this row cannot agree with a copy of itself.
	var want []string
	for _, e := range decl.Environment {
		if e.EmptySensitive {
			want = append(want, e.Name)
		}
	}
	if len(want) < 2 {
		t.Fatalf("the fixture marks %d names; this row needs at least two, on two classes, to mean anything", len(want))
	}
	slices.Sort(want)
	if got := decl.EmptySensitiveNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("EmptySensitiveNames returned %q, want %q", got, want)
	}
	// THE CLASS THAT MAKES THIS A REQUIREMENT AND NOT A CONVENIENCE: at least one
	// marked name is not-carried, the class EnvTable drops by construction, so an
	// implementation that folded the mark into the class table would lose it.
	var markedNotCarried string
	for _, e := range decl.Environment {
		if e.EmptySensitive && e.Class == EnvClassNotCarried {
			markedNotCarried = e.Name
		}
	}
	if markedNotCarried == "" {
		t.Fatal("the fixture marks no not-carried name, so the class this answer exists for is untested")
	}
	if !slices.Contains(decl.EmptySensitiveNames(), markedNotCarried) {
		t.Errorf("EmptySensitiveNames dropped the marked not-carried name %q", markedNotCarried)
	}
	for _, row := range decl.EnvTable() {
		if strings.Contains(row, markedNotCarried) {
			t.Errorf("EnvTable rendered %q for a not-carried name; the mark must not have widened the class table", row)
		}
	}
}

// TestEmptySensitiveNames_IsSorted pins the same determinism EnvTable has: the
// answer is baked into a generated shell function whose drift gate diffs it, so
// an unsorted answer would report a change nobody made on every regeneration.
func TestEmptySensitiveNames_IsSorted(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Environment = []EnvDeclaration{
		{Name: "ZED", Class: EnvClassSelector, EmptySensitive: true},
		{Name: "ALPHA", Class: EnvClassSecret, EmptySensitive: true},
		{Name: "MIDDLE", Class: EnvClassNotCarried, EmptySensitive: true},
		{Name: "UNMARKED", Class: EnvClassPath},
	}
	want := []string{"ALPHA", "MIDDLE", "ZED"}
	if got := decl.EmptySensitiveNames(); !reflect.DeepEqual(got, want) {
		t.Errorf("EmptySensitiveNames returned %q, want %q", got, want)
	}
}

// TestEmptySensitiveNames_MarkingNothingAnswersNothing is the arm seven of the
// eleven shipped collectors take. An empty answer is the CORRECT answer, not a
// broken reader, which is why the generator carries no emptiness guard on it.
func TestEmptySensitiveNames_MarkingNothingAnswersNothing(t *testing.T) {
	decl := fixtureDeclaration()
	for i := range decl.Environment {
		decl.Environment[i].EmptySensitive = false
	}
	if got := decl.EmptySensitiveNames(); len(got) != 0 {
		t.Errorf("a declaration that marks nothing answered %q, want no names at all", got)
	}
	// The control, same run: the declaration still renders its class table, so an
	// implementation that returned nothing for everything would not satisfy this.
	if len(decl.EnvTable()) == 0 {
		t.Error("the same declaration rendered no class rows either; this row would have passed vacuously")
	}
}

// TestDescribeEnvSensitiveArgvQueryAnswersTheInstaller pins the third
// line-oriented answer on a REAL CHILD PROCESS, on the same terms as the other
// two: the format is a contract with a `#!/bin/sh` consumer that has no JSON
// parser and cannot speak MCP.
func TestDescribeEnvSensitiveArgvQueryAnswersTheInstaller(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self, describeEnvSensitiveArg)
	cmd.Env = append(os.Environ(), fixtureModeEnv+"="+string(fixtureConforming), fixtureToolEnv+"=")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s exited %v (stderr: %s)", describeEnvSensitiveArg, err, stderr.String())
	}
	got := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	want := fixtureDeclaration().EmptySensitiveNames()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s printed %q, want %q", describeEnvSensitiveArg, got, want)
	}
}

// TestWriteDescribeQuery_RefusesAMalformedDeclarationOnTheSensitiveArm holds the
// third arm to the same rule as the env-table arm: a generator reading an empty
// answer would bake an empty table and every marked name would silently stop
// being marked, which is the silent-narrowing shape the contract refuses.
func TestWriteDescribeQuery_RefusesAMalformedDeclarationOnTheSensitiveArm(t *testing.T) {
	c := &fixtureCollector{declBad: true}
	var out bytes.Buffer
	err := writeDescribeQuery(&out, describeEnvSensitiveArg, c)
	if err == nil {
		t.Fatalf("a malformed declaration printed %q instead of failing", out.String())
	}
	if !strings.Contains(err.Error(), "NOT A NAME") {
		t.Errorf("the refusal does not name the offending value: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("the refusing arm still wrote %q to stdout", out.String())
	}
}
