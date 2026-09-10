// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// readme_live_test.go — the half of the README gate that compares the document
// against a REAL child collector rather than against this package's source.
//
// EVERY VALUE HERE COMES OFF THE WIRE. The handshake fields, the two advertised
// schemas and the declaration tool's own name are read from a collector dialed
// over stdio through the shipped entry point, so a claim in the document is
// checked against what a reader's client will actually see. A literal in this
// file would be a second transcription drifting beside the first.
//
// The declaration pin at the bottom is EXPECTED RED until the declaration tool
// lands; its own comment says why and what closes it.

// TestREADMEHandshakeClaimsMatchALiveCollector is the row that makes the
// handshake block something other than a transcription. The values come off a
// REAL child collector dialed over stdio through the shipped entry point, so a
// change to what this framework answers reds the document that documents it.
//
// THE DOCUMENTED BLOCK IS DECODED, NOT SEARCHED FOR. A substring search over
// the whole page passes on a wrong block whenever the right token appears in
// some neighboring sentence — which it does here, since the prose explains
// each of these values beside the block. Decoding the fence and comparing field
// by field is what makes a wrong value in the block a red.
func TestREADMEHandshakeClaimsMatchALiveCollector(t *testing.T) {
	doc := readmeDoc(t)
	p := dialStdio(t, fixtureConforming, "")

	init := p.session.InitializeResult()
	if init == nil {
		t.Fatal("the dialed collector reports no initialize result; the handshake claims have nothing to compare against")
	}
	if init.ServerInfo == nil {
		t.Fatal("the dialed collector's initialize result carries no serverInfo")
	}

	var documented struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	fence := commentsStripped(fenceIn(t, readmeSection(t, doc, "## The wire contract"), "```jsonc\n"))
	if err := json.Unmarshal([]byte(fence), &documented); err != nil {
		t.Fatalf("the documented handshake block does not decode as the JSON it claims to be: %v\n%s", err, fence)
	}

	for _, field := range []struct {
		name       string
		live, docd string
		why        string
	}{
		{"protocolVersion", init.ProtocolVersion, documented.ProtocolVersion,
			"the revision this framework negotiates; a block naming another one sends a port author to the wrong specification"},
		{"serverInfo.name", init.ServerInfo.Name, documented.ServerInfo.Name,
			"the SERVED TOOL's name — the fact the block exists to teach, since a reader expects the collector's own name here"},
		{"serverInfo.version", init.ServerInfo.Version, documented.ServerInfo.Version,
			"this serving layer's version, which is not the binary's build stamp"},
	} {
		if field.live == "" {
			t.Fatalf("the live handshake carries an empty %s; comparing the document against it would "+
				"pass on any document", field.name)
		}
		if field.docd != field.live {
			t.Errorf("the README's handshake block states %s = %q and a live collector answers %q: %s",
				field.name, field.docd, field.live, field.why)
		}
	}

	// The served tool name the README documents is the framework's own constant,
	// never a literal typed twice.
	if !strings.Contains(doc, DefaultToolName) {
		t.Errorf("the README never names the default served tool %q", DefaultToolName)
	}
}

// commentsStripped removes the whole-line // comments a jsonc block carries, so
// the rest can be decoded as JSON.
func commentsStripped(fence string) string {
	var kept []string
	for line := range strings.SplitSeq(fence, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// TestREADMESchemaClaimsMatchTheAdvertisedSchemas compares what the README says
// about the two schemas against what a live collector ADVERTISES, and the
// advertised schemas against the contract bytes.
//
// THE TOOL IS SELECTED BY NAME, never by cardinality: a collector serves more
// than one tool, and a gate that asserted a count would red the day another is
// added, which is a fact about this document that no reader cares about.
func TestREADMESchemaClaimsMatchTheAdvertisedSchemas(t *testing.T) {
	fenceTestCacheOnClientContract(t)

	doc := readmeDoc(t)
	p := dialStdio(t, fixtureConforming, "")
	collect := toolNamed(t, p, DefaultToolName)

	wantOut := decode(t, clientContract(t, "collector_output.schema.json"))
	gotOut := decode(t, []byte(mustJSON(t, collect.OutputSchema)))
	if !reflect.DeepEqual(gotOut, wantOut) {
		t.Errorf("the advertised OUTPUT schema is not the contract's, so the README's rule that it is "+
			"advertised verbatim is false:\n want: %s\n  got: %s", mustJSON(t, wantOut), mustJSON(t, gotOut))
	}

	wantIn := decode(t, clientContract(t, "collector_input.schema.json"))
	gotIn := decode(t, []byte(mustJSON(t, collect.InputSchema)))
	required, ok := gotIn["required"].([]any)
	if !ok || len(required) != 1 || required[0] != "id" {
		t.Errorf("the advertised input schema's top-level required list is %v; the README tells a port "+
			"author it stays [\"id\"] by construction, and a paramless collect is refused when it does not",
			gotIn["required"])
	}
	stripParams(t, wantIn)
	stripParams(t, gotIn)
	if !reflect.DeepEqual(gotIn, wantIn) {
		t.Errorf("the advertised INPUT schema differs from the contract outside properties.params, so the "+
			"README's splice rule is false:\n want: %s\n  got: %s", mustJSON(t, wantIn), mustJSON(t, gotIn))
	}

	assertClaims(t, readmeSection(t, doc, "### Three wire-shaping rules a port has to reproduce"), []claim{
		{"verbatim", "rule 1: the output schema is the contract file, because an inferred one is refused"},
		{`["null","array"]`, "the union an inferred slice schema produces, which is WHY rule 1 exists"},
		{`params`, "rule 2: the property spliced into the contract document"},
		{`["id"]`, "rule 2's consequence: the top-level required list a paramless collect depends on"},
		// THE WHOLE SENTENCE, not the bare token: `null` also occurs inside
		// rule 1's ["null","array"] literal a few lines above, so a bare claim
		// for it observed rule 1's text and rule 3's sentence not at all —
		// changing rule 3 to say `nil` left this case green. missingClaims
		// whitespace-normalizes both sides, so the wrapped line matches.
		{"`[]` for `nodes` and `edges`, never `null`",
			"rule 3: the nil slice a walk that found nothing produces, and must not send"},
	})
}

// TestREADMEDeclarationSectionMatchesTheServedDeclarationTool — A PENDING PIN.
// It asserts nothing while the framework serves one tool, and it starts
// asserting, with no edit here, the moment a second one is served.
//
// The declaration tool is a REQUIRED second tool in the custom collector
// contract, decided by the owner and built by its own ticket, which lands
// before this document is published. This README's declaration section is
// therefore written EXPECTED: it states what that tool is and what its result
// carries, and this pin is what will prove the statement true rather than
// plausible. What the section says TODAY is not unobserved in the meantime —
// TestREADMEDeclarationSectionStatesTheRequiredTool reads it on every run.
//
// WHAT IT DOES, and why in this shape. It selects the tool the collector serves
// BESIDE the collect tool, reads its NAME off the live listing, and asserts the
// document names it — so the document is checked against whatever name the
// contract ends up with rather than against a name typed here. It then calls
// that tool and asserts the section names every top-level field the live
// declaration carries, which is an expectation derived from the producer's
// actual result rather than a transcription of a schema.
//
// WHY IT SKIPS RATHER THAN FAILS BEFORE THAT TOOL EXISTS. A test that fails on
// every tree until a sibling change lands is not a pin in this repository, it
// is a stop: the pre-commit hook runs `go test` over the package of every
// staged Go file, so a standing red here would refuse every commit that touches
// this module, on this branch and on every lane's. The pin therefore reports
// the pending state as a SKIP naming what closes it, and turns into assertions
// the moment the condition it waits for is true.
//
// DO NOT CONVERT THE SKIP INTO A PASS, and do not delete the case. If the
// framework serves a declaration tool while this module's fixture collector
// does not, this skip has gone stale and the FIXTURE is what changes — the skip
// message says so where a reader of a run will see it.
func TestREADMEDeclarationSectionMatchesTheServedDeclarationTool(t *testing.T) {
	doc := readmeDoc(t)
	section := readmeSection(t, doc, "## Declaring what your collector is")

	p := dialStdio(t, fixtureConforming, "")
	list, err := p.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing the dialed collector's tools: %v", err)
	}
	var declaration *mcp.Tool
	for _, tool := range list.Tools {
		if tool.Name != DefaultToolName {
			declaration = tool
			break
		}
	}
	if declaration == nil {
		t.Skipf("PENDING PIN, NOT A PASS: this collector serves only %q, so the README's declaration "+
			"section — which describes a REQUIRED second tool — is not compared against anything here "+
			"yet. It starts asserting the moment a second tool is served, with no edit to this test. If "+
			"the framework serves a declaration tool and this module's fixture collector does not, this "+
			"skip is stale and the fixture is what has to change.", DefaultToolName)
	}

	if !strings.Contains(section, declaration.Name) {
		t.Errorf("the collector serves the declaration tool as %q and the section never names it; "+
			"a reader would implement a tool the client does not call", declaration.Name)
	}

	result, err := p.session.CallTool(t.Context(), &mcp.CallToolParams{Name: declaration.Name})
	if err != nil {
		t.Fatalf("calling the declaration tool %q: %v", declaration.Name, err)
	}
	if result.IsError {
		t.Fatalf("the declaration tool %q returned an error result, so its contents cannot be compared "+
			"against the document", declaration.Name)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("re-encoding the declaration result: %v", err)
	}
	var declared map[string]any
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatalf("the declaration result is not an object: %v", err)
	}
	if len(declared) == 0 {
		t.Fatal("the declaration result carries no field at all; every assertion below it would pass vacuously")
	}
	for field := range declared {
		if !strings.Contains(section, field) {
			t.Errorf("the live declaration carries the field %q and the section never names it; the "+
				"section is what a port author implements the declaration from", field)
		}
	}
}

// toolNamed selects one advertised tool by NAME and asserts nothing about how
// many are served.
func toolNamed(t *testing.T, p *provider, name string) *mcp.Tool {
	t.Helper()
	list, err := p.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("[%s] listing tools: %v", p.transport, err)
	}
	for _, tool := range list.Tools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("[%s] the collector serves no tool named %q", p.transport, name)
	return nil
}
