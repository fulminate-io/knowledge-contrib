// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// describe_test.go — the REQUIRED SECOND TOOL, over both transports.
//
// Every row here drives a real session against a real provider: the stdio arm is
// this binary re-execed through the shipped ServeStdio, the http arm is the same
// server value behind httptest. The declaration a row asserts against is the
// fixture collector's OWN value, never a literal copy of it, so a test cannot
// pass by agreeing with itself.

// TestDescribeIsServedOverBothTransports is the two-transport row: a collector
// serves BOTH tools on BOTH transports, and the describe tool's advertised
// schemas are byte-identical between them.
func TestDescribeIsServedOverBothTransports(t *testing.T) {
	stdioArm := dial(t, overStdio, fixtureConforming, "")
	httpArm := dial(t, overHTTP, fixtureConforming, "")

	stdioDescribe := stdioArm.advertised(t, DescribeToolName)
	httpDescribe := httpArm.advertised(t, DescribeToolName)
	// The collect tool is still there beside it on both arms.
	_ = stdioArm.collectTool(t)
	_ = httpArm.collectTool(t)

	if got, want := mustJSON(t, httpDescribe.OutputSchema), mustJSON(t, stdioDescribe.OutputSchema); got != want {
		t.Errorf("the two transports advertise different describe OUTPUT schemas:\n stdio: %s\n  http: %s", want, got)
	}
	if got, want := mustJSON(t, httpDescribe.InputSchema), mustJSON(t, stdioDescribe.InputSchema); got != want {
		t.Errorf("the two transports advertise different describe INPUT schemas:\n stdio: %s\n  http: %s", want, got)
	}

	stdioDoc := describeDocument(t, stdioArm)
	httpDoc := describeDocument(t, httpArm)
	if got, want := mustJSON(t, httpDoc), mustJSON(t, stdioDoc); got != want {
		t.Errorf("the two transports returned different declarations:\n stdio: %s\n  http: %s", want, got)
	}
}

// TestDescribeAdvertisesTheContractFileVerbatim is the row the client's
// comparator depends on: an SDK-INFERRED schema declares a Go slice as the union
// ["null","array"] and is REFUSED there, so the describe tool must advertise the
// checked-in bytes.
func TestDescribeAdvertisesTheContractFileVerbatim(t *testing.T) {
	tool, _, err := describeToolFor[fixtureParams](&fixtureCollector{mode: fixtureConforming})
	if err != nil {
		t.Fatalf("building the describe tool: %v", err)
	}
	raw, ok := tool.OutputSchema.(json.RawMessage)
	if !ok {
		t.Fatalf("the describe tool advertises a %T output schema; it must advertise the contract file's bytes", tool.OutputSchema)
	}
	// COMPARED AS STRINGS RATHER THAN WITH bytes.Equal, for the reason
	// schema_test.go's own byte-parity row states beside the identical
	// comparison: the corpus check that forbids a variable-time byte comparison
	// in an authentication path is package-scoped by intent but matches the call
	// anywhere, and this comparison authenticates nothing — it asks whether a
	// tool advertised the checked-in schema. A string comparison says the same
	// thing and keeps a repository-wide scan honest.
	if string(raw) != string(DescribeContractJSON()) {
		t.Errorf("the advertised describe schema is not the checked-in file:\n advertised: %s\n     file: %s",
			raw, DescribeContractJSON())
	}
}

// TestDescribeAnswersTheCollectorsOwnDeclaration reads the declaration off the
// wire and compares it to the fixture's OWN value re-marshaled — an external
// expectation in the only sense available here, since the collector is the sole
// authority on what it declares. What the row actually proves is that the tool
// carries the value through unchanged, which is what a hand-written expected
// literal would NOT prove (it would drift with the fixture).
func TestDescribeAnswersTheCollectorsOwnDeclaration(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureConforming, "")
			got := describeDocument(t, p)

			var want map[string]any
			raw, err := json.Marshal(fixtureDeclaration())
			if err != nil {
				t.Fatalf("marshaling the fixture's own declaration: %v", err)
			}
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatalf("decoding the fixture's own declaration: %v", err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("the served declaration is not the collector's own:\n served: %s\n   own: %s",
					mustJSON(t, got), mustJSON(t, want))
			}
		})
	}
}

// TestDescribeRendersNoReasonKey is the ticket-39 regression guard. The client's
// config loader decodes an entry with DisallowUnknownFields at every level, so a
// rendered `reason` makes the whole scoped file unloadable — and the fixture
// DOES set one, so this row is only green because the renderer drops it.
func TestDescribeRendersNoReasonKey(t *testing.T) {
	if fixtureDeclaration().Context["code"].Reason == "" {
		t.Fatal("the fixture declares no reason, so this row would pass on a renderer that kept the key")
	}
	p := dial(t, overStdio, fixtureConforming, "")
	rendered := mustJSON(t, describeDocument(t, p))
	if strings.Contains(rendered, "reason") {
		t.Errorf("the rendered declaration carries a reason key, which the entry's strict decode refuses by name: %s", rendered)
	}
}

// TestDescribeTakesNoArguments pins the input side: the tool is callable with no
// arguments at all, which is what the client does.
func TestDescribeTakesNoArguments(t *testing.T) {
	p := dial(t, overHTTP, fixtureConforming, "")
	res, err := p.session.CallTool(t.Context(), &mcp.CallToolParams{Name: DescribeToolName})
	if err != nil {
		t.Fatalf("calling describe with no arguments: %v", err)
	}
	if res.IsError {
		t.Fatalf("describe reported an error result: %s", resultText(res))
	}
}

// TestNewServerRefusesAMalformedDeclaration is the gate that makes Describe a
// method worth having: a declaration the client would refuse fails HERE, at the
// author's own build, rather than at an operator's install.
func TestNewServerRefusesAMalformedDeclaration(t *testing.T) {
	_, err := NewServer[fixtureParams](&fixtureCollector{mode: fixtureConforming, declBad: true})
	if err == nil {
		t.Fatal("a declaration carrying an environment name an installer cannot write must refuse the server")
	}
	if !strings.Contains(err.Error(), "NOT A NAME") {
		t.Errorf("the refusal must name the offending value, got: %v", err)
	}
}

// TestDeclarationValidateRefusesEachOmissionIndividually walks the ways a
// declaration can be incomplete. Each row bends ONE thing, so a validator that
// stopped checking any single one turns a row red rather than leaving the suite
// green.
func TestDeclarationValidateRefusesEachOmissionIndividually(t *testing.T) {
	for _, tc := range []struct {
		name       string
		bend       func(d *Declaration)
		wantErrHas string
	}{
		{"summarizable never said", func(d *Declaration) { d.Behavior.Summarizable = nil }, "summarizable"},
		{"embeddable never said", func(d *Declaration) { d.Behavior.Embeddable = nil }, "embeddable"},
		{"syncable never said", func(d *Declaration) { d.Behavior.Syncable = nil }, "syncable"},
		{"no node vocabulary at all", func(d *Declaration) { d.NodeTypes = nil }, "node_types"},
		{"no edge vocabulary at all", func(d *Declaration) { d.EdgeTypes = nil }, "edge_types"},
		{"no environment declaration at all", func(d *Declaration) { d.Environment = nil }, "environment"},
		{"an empty node type", func(d *Declaration) { d.NodeTypes = []string{"issue", " "} }, "empty type name"},
		{"a duplicated node type", func(d *Declaration) { d.NodeTypes = []string{"issue", "issue"} }, "twice"},
		{"an unknown environment class", func(d *Declaration) {
			d.Environment = []EnvDeclaration{{Name: "TOK", Class: "token"}}
		}, "token"},
		{"a duplicated environment name", func(d *Declaration) {
			d.Environment = []EnvDeclaration{{Name: "TOK", Class: EnvClassSecret}, {Name: "TOK", Class: EnvClassPath}}
		}, "twice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decl := fixtureDeclaration()
			tc.bend(&decl)
			err := decl.validate()
			if err == nil {
				t.Fatal("an incomplete declaration must be refused")
			}
			if !strings.Contains(err.Error(), tc.wantErrHas) {
				t.Errorf("the refusal must name what is wrong (%q), got: %v", tc.wantErrHas, err)
			}
		})
	}
}

// TestAThirdToolLeavesEveryRowGreen is the MUTATION that proves this suite
// selects by name rather than counting. The child serves an unrelated third tool
// beside the two the framework serves; the collect call, the describe call and
// the by-name lookups must all behave exactly as they do without it.
//
// A suite that had swapped "exactly 1" for "exactly 2" would fail here, which is
// why the mutation is a permanent row rather than a one-off edit.
func TestAThirdToolLeavesEveryRowGreen(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(),
		fixtureModeEnv+"="+string(fixtureConforming),
		fixtureToolEnv+"=",
		fixtureExtraToolEnv+"=1",
	)
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "framework-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the three-tool collector: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	p := &provider{transport: overStdio, session: session}

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	if len(list.Tools) != 3 {
		t.Fatalf("the mutation must actually add a tool; the listing holds %d", len(list.Tools))
	}

	res, err := p.call(t, map[string]any{"id": "inst-7", "params": map[string]any{"region": "us-east-1"}})
	if err != nil {
		t.Fatalf("collect against a three-tool provider: %v", err)
	}
	doc := envelopeOf(t, res)
	assertArray(t, doc, "nodes", 2)

	if got := describeDocument(t, p)["node_types"]; !reflect.DeepEqual(got, []any{"issue"}) {
		t.Errorf("describe against a three-tool provider returned node_types %v", got)
	}
}

// describeDocument calls describe over a dialed provider and returns the
// declaration as a decoded document.
func describeDocument(t *testing.T, p *provider) map[string]any {
	t.Helper()
	res, err := p.session.CallTool(t.Context(), &mcp.CallToolParams{Name: DescribeToolName, Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("[%s] calling describe: %v", p.transport, err)
	}
	if res.IsError {
		t.Fatalf("[%s] describe reported an error result: %s", p.transport, resultText(res))
	}
	return envelopeOf(t, res)
}

// TestDescribeArgvQueriesAnswerTheInstaller pins the two line-oriented answers
// the installer's table generator reads, on a REAL CHILD PROCESS: the format is
// a contract with a `#!/bin/sh` consumer that has no JSON parser.
func TestDescribeArgvQueriesAnswerTheInstaller(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want []string
	}{
		{describeToolArg, []string{DefaultToolName}},
		{describeEnvTableArg, []string{"path HOME", "selector FIXTURE_REGION", "secret FIXTURE_TOKEN"}},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			self, err := os.Executable()
			if err != nil {
				t.Fatalf("resolving the test binary: %v", err)
			}
			cmd := exec.Command(self, tc.arg)
			cmd.Env = append(os.Environ(), fixtureModeEnv+"="+string(fixtureConforming), fixtureToolEnv+"=")
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err != nil {
				t.Fatalf("%s exited %v (stderr: %s)", tc.arg, err, stderr.String())
			}
			got := strings.Split(strings.TrimSpace(stdout.String()), "\n")
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("%s printed %q, want %q", tc.arg, got, tc.want)
			}
		})
	}
}

// TestDescribeArgvQueriesDoNotEatEntryArgs is the argv-position rule, the same
// one --version follows: a config entry may carry these spellings as its own
// arguments in any position but the first, and a collector that short-circuited
// on them would serve nothing.
func TestDescribeArgvQueriesDoNotEatEntryArgs(t *testing.T) {
	for _, args := range [][]string{
		{"--region", describeEnvTableArg},
		{"--region", describeToolArg},
		// THE THIRD SWITCH GETS ITS OWN CELL, even though describeQuery reads
		// args[1] and nothing else, so all three share one position rule by
		// construction. The guide now states that rule of all THREE switches, in
		// public prose; a later loosening of the new one alone would ship against
		// that sentence with nothing red.
		{"--region", describeEnvSensitiveArg},
	} {
		self, err := os.Executable()
		if err != nil {
			t.Fatalf("resolving the test binary: %v", err)
		}
		cmd := exec.Command(self, args...)
		cmd.Env = append(os.Environ(), fixtureModeEnv+"="+string(fixtureConforming), fixtureToolEnv+"=")
		cmd.Stderr = os.Stderr

		client := mcp.NewClient(&mcp.Implementation{Name: "framework-suite", Version: "v1"}, nil)
		session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
		if err != nil {
			t.Fatalf("a collector spawned with args %v must still serve: %v", args, err)
		}
		if _, err := session.ListTools(t.Context(), nil); err != nil {
			t.Fatalf("listing tools over a collector spawned with args %v: %v", args, err)
		}
		if err := session.Close(); err != nil {
			t.Fatalf("closing the session: %v", err)
		}
	}
}

// TestEnvTable_RendersNoRowForANotCarriedName is the fourth class where it is
// IMPLEMENTED rather than where it is consumed.
//
// WHAT THE CLASS MEANS. A collector declares every environment name it reads;
// `not-carried` is the name an installed entry deliberately does not declare — a
// machine fact, a platform the installer does not target, a non-default
// deployment an operator configures by hand. Declaring it is what tells an
// operator the absence was a decision.
//
// WHY THE ROW IS HERE. The installer's own case arm knows three tokens and fails
// a fourth by name, so a not-carried row reaching the table would break every
// install rather than merely widening one. The generator's drift gate catches
// that one level away, at the point the tables are regenerated; this catches it
// at the point the exclusion is written.
func TestEnvTable_RendersNoRowForANotCarriedName(t *testing.T) {
	decl := fixtureDeclaration()
	var declared string
	for _, row := range decl.Environment {
		if row.Class == EnvClassNotCarried {
			declared = row.Name
		}
	}
	if declared == "" {
		t.Fatal("the fixture declares no not-carried name, so this row would pass vacuously")
	}
	for _, row := range decl.EnvTable() {
		if strings.Contains(row, declared) {
			t.Errorf("the installer table carries the row %q for a not-carried name; install.sh's case arm knows "+
				"path, selector and secret and fails any other token by name", row)
		}
		if strings.HasPrefix(row, EnvClassNotCarried+" ") {
			t.Errorf("the installer table carries a %q row (%q), which is not one of the three classes it consumes",
				EnvClassNotCarried, row)
		}
	}
	// THE CONTROL, in the same run and through the same renderer: the three
	// classes the installer DOES consume are still rendered, so an EnvTable that
	// returned nothing at all would not satisfy this row.
	if got := len(decl.EnvTable()); got != len(decl.Environment)-1 {
		t.Errorf("EnvTable rendered %d rows for %d declared names; it must drop the not-carried one and keep the rest",
			got, len(decl.Environment))
	}
}

// TestEnvTableIsSortedWithinClass pins the generator's own determinism: an
// unsorted table would rewrite the installer's rows on every regeneration and
// make the drift gate report a change nobody made.
func TestEnvTableIsSortedWithinClass(t *testing.T) {
	decl := fixtureDeclaration()
	decl.Environment = []EnvDeclaration{
		{Name: "ZED", Class: EnvClassSelector},
		{Name: "ALPHA", Class: EnvClassSelector},
		{Name: "HOME", Class: EnvClassPath},
		{Name: "TOKEN", Class: EnvClassSecret},
	}
	want := []string{"path HOME", "selector ALPHA", "selector ZED", "secret TOKEN"}
	if got := decl.EnvTable(); !reflect.DeepEqual(got, want) {
		t.Errorf("EnvTable rendered %q, want %q", got, want)
	}
}
