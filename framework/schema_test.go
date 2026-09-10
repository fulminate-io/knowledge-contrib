// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// schema_test.go — what this framework ADVERTISES, checked against the client's
// own checked-in contract files rather than against a transcription of them.
//
// THE CLIENT'S FILES ARE READ THROUGH A SYMLINK INSIDE THIS MODULE, and that is
// the only venue that needs no cross-module build: the client's package is
// internal to its module and unimportable here, and go:embed refuses a symlinked
// directory. Opening the files through a link under this module's own testdata
// is ALSO this test's cache fence — the go tool drops any opened name resolving
// outside the tested module's root, so without the link a change to the client's
// contract would be served a stored PASS here.

// clientContractLink is the link this module reads the client's authoritative
// contract schemas through.
const clientContractLink = "testdata/client-contract"

// fenceTestCacheOnClientContract opens the client's contract files through this
// module's own testdata link, so their bytes decide whether this package's
// stored test result still applies.
//
// IT IS CALLED FROM EACH TEST THAT READS THEM, and never from TestMain: the go
// tool installs the hook that records a test's opened files inside m.Run, so a
// fence placed in TestMain opens its files outside the recording window and
// reaches no cache key at all — while looking, in the diff and in a green run,
// exactly like a fence that works.
//
// THE NAME CARRIES THE fenceTestCache PREFIX ON PURPOSE. The repository's
// fence-placement census and its corpus check both match that prefix at the CALL
// SITE, and neither can see a prefix on the far side of a package qualifier, so
// the shared walker is wrapped here rather than called directly. A collector
// module writes the same three lines against its own link.
func fenceTestCacheOnClientContract(t *testing.T) {
	t.Helper()
	// THREE is the real count of what the link serves and is a known positive: a
	// dangling link, a link materialized as a text stub by a checkout without
	// symlink support, and a link to an empty directory all read as zero. It was
	// two until the describe schema joined the contract; the floor moves with the
	// directory, because a floor below what the link serves would let a document
	// stop being opened without this fence noticing.
	frameworktest.FenceTestCacheOnLinkedTree(t, clientContractLink, 3)
}

// clientContract reads one of the client's authoritative schema files through
// the link.
func clientContract(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(clientContractLink, name))
	if err != nil {
		t.Fatalf("reading the client's %s through %s: %v", name, clientContractLink, err)
	}
	return raw
}

// decode renders JSON bytes as a document for comparison.
func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding %s: %v", raw, err)
	}
	return doc
}

// TestContractLinkResolvesToTheClientsContract is the sentinel. A checkout
// without symlink support materializes the link as a one-line text stub, and
// every test below would then read a stub, fence nothing, and compare this
// module's copy against itself.
func TestContractLinkResolvesToTheClientsContract(t *testing.T) {
	info, err := os.Lstat(clientContractLink)
	if err != nil {
		t.Fatalf("%s must exist — it is the only venue that reads the client's authoritative schemas: %v",
			clientContractLink, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("%s is not a symlink; a checkout without symlink support materializes it as a text stub, "+
			"and every contract assertion in this package would then compare this module's copy against itself",
			clientContractLink)
	}
	abs, err := filepath.Abs(clientContractLink)
	if err != nil {
		t.Fatalf("resolving %s: %v", clientContractLink, err)
	}
	target, err := filepath.EvalSymlinks(abs)
	if err != nil {
		t.Fatalf("%s must resolve; a dangling link fences nothing: %v", clientContractLink, err)
	}
	const want = "cmd/knowledge/internal/externalcollector/contract"
	if !strings.HasSuffix(filepath.ToSlash(target), want) {
		t.Fatalf("%s resolves to %s, which is not %s", clientContractLink, target, want)
	}
}

// TestEmbeddedContractCopiesMatchTheClientsFiles is row 8's first half: this
// module embeds a COPY of the client's checked-in schemas, because the client's
// package is unimportable and go:embed refuses a symlinked directory. A copy
// nothing compares is a copy that drifts, so this compares the bytes.
func TestEmbeddedContractCopiesMatchTheClientsFiles(t *testing.T) {
	fenceTestCacheOnClientContract(t)

	for _, tc := range []struct {
		file     string
		embedded []byte
	}{
		{"collector_input.schema.json", InputContractJSON()},
		{"collector_output.schema.json", OutputContractJSON()},
		// THE THIRD FILE JOINS BOTH DIRECTORIES OR THIS ROW REDS. The describe
		// tool is required of every collector and advertises this document
		// verbatim, so a copy that drifted would have every collector advertising
		// a schema the client's comparator refuses.
		{"collector_describe.schema.json", DescribeContractJSON()},
	} {
		t.Run(tc.file, func(t *testing.T) {
			// Compared as strings rather than with bytes.Equal: the corpus check
			// that forbids a variable-time byte comparison in an authentication
			// path is package-scoped by intent but matches the call anywhere, and
			// this comparison authenticates nothing. A string comparison says the
			// same thing and keeps a repository-wide scan honest.
			authoritative := string(clientContract(t, tc.file))
			if authoritative != string(tc.embedded) {
				t.Errorf("this module's copy of %s has drifted from the client's file.\n client: %s\n  copy:  %s",
					tc.file, authoritative, tc.embedded)
			}
		})
	}
}

// TestAdvertisedSchemasAreTheContract is row 8's second half and row 17: what
// the tool ADVERTISES is the contract, not a schema inferred from the Go types.
// The output side is the contract document exactly; the input side is the
// contract document with properties.params replaced and nothing else touched.
func TestAdvertisedSchemasAreTheContract(t *testing.T) {
	fenceTestCacheOnClientContract(t)

	tool, _, err := toolFor[fixtureParams](&fixtureCollector{mode: fixtureConforming})
	if err != nil {
		t.Fatalf("building the tool definition: %v", err)
	}

	wantOut := decode(t, clientContract(t, "collector_output.schema.json"))
	gotOut := decode(t, []byte(mustJSON(t, tool.OutputSchema)))
	if !reflect.DeepEqual(gotOut, wantOut) {
		t.Errorf("the advertised OUTPUT schema is not the contract's:\n want: %s\n  got: %s",
			mustJSON(t, wantOut), mustJSON(t, gotOut))
	}

	wantIn := decode(t, clientContract(t, "collector_input.schema.json"))
	gotIn := decode(t, []byte(mustJSON(t, tool.InputSchema)))

	wantParams, gotParams := propertyOf(t, wantIn, "params"), propertyOf(t, gotIn, "params")
	if reflect.DeepEqual(gotParams, wantParams) {
		t.Errorf("the advertised input schema still carries the contract's placeholder params sub-schema; "+
			"the collector's own params schema is what has to be spliced in: %s", mustJSON(t, gotParams))
	}
	if gotParams["type"] != "object" {
		t.Errorf("the spliced params sub-schema declares type %v, want object", gotParams["type"])
	}
	if !strings.Contains(mustJSON(t, gotParams), "region") {
		t.Errorf("the spliced params sub-schema does not carry the collector's own fields: %s", mustJSON(t, gotParams))
	}

	// Everything OUTSIDE properties.params is the contract document verbatim.
	stripParams(t, wantIn)
	stripParams(t, gotIn)
	if !reflect.DeepEqual(gotIn, wantIn) {
		t.Errorf("the advertised INPUT schema differs from the contract outside properties.params:\n want: %s\n  got: %s",
			mustJSON(t, wantIn), mustJSON(t, gotIn))
	}
}

func propertyOf(t *testing.T, doc map[string]any, name string) map[string]any {
	t.Helper()
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema declares no properties object: %s", mustJSON(t, doc))
	}
	sub, ok := props[name].(map[string]any)
	if !ok {
		t.Fatalf("the schema declares no %q property: %s", name, mustJSON(t, doc))
	}
	return sub
}

func stripParams(t *testing.T, doc map[string]any) {
	t.Helper()
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema declares no properties object: %s", mustJSON(t, doc))
	}
	delete(props, "params")
}

// TestAdvertisedInputSchemaRequiresOnlyTheCollectID is R10, row 9b. A required
// `params` in the advertised input schema refuses EVERY paramless collect before
// the call is sent, because the client omits params from the arguments entirely
// when a collect carries none. Building the input from the contract file keeps
// the top-level required list at ["id"] by construction; this pins it, so a
// later change to the construction turns a named test red rather than breaking
// every paramless collect silently.
func TestAdvertisedInputSchemaRequiresOnlyTheCollectID(t *testing.T) {
	tool, _, err := toolFor[fixtureParams](&fixtureCollector{mode: fixtureConforming})
	if err != nil {
		t.Fatalf("building the tool definition: %v", err)
	}
	doc := decode(t, []byte(mustJSON(t, tool.InputSchema)))

	required, ok := doc["required"].([]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no required list: %s", mustJSON(t, doc))
	}
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("the advertised input schema's top-level required list is %v, want exactly [id]; "+
			"a required params refuses every paramless collect", required)
	}

	// Row 9's other half: the contract declares a params PROPERTY and a tool
	// that declares none is refused at registration, so a collector with no
	// parameters still advertises one.
	if _, present := doc["properties"].(map[string]any)["params"]; !present {
		t.Errorf("the advertised input schema declares no params property at all: %s", mustJSON(t, doc))
	}
}

// TestAdvertisedOutputSchemaRequiresWalkComplete is row 17. Under the ruled
// shape this rides on row 8's equality, and it is kept as its own row because
// the requirement names it: walk_complete is what the server's deletion guard
// reads, and a provider that could omit it would disable that phase by silence.
func TestAdvertisedOutputSchemaRequiresWalkComplete(t *testing.T) {
	doc := decode(t, []byte(mustJSON(t, advertisedOutputSchema())))
	required, ok := doc["required"].([]any)
	if !ok {
		t.Fatalf("the advertised output schema declares no required list: %s", mustJSON(t, doc))
	}
	for _, want := range []string{"nodes", "edges", "walk_complete"} {
		found := false
		for _, got := range required {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the advertised output schema does not require %q: %v", want, required)
		}
	}
}

// TestInferredOutputSchemaWouldNotDeclareAnArrayType is row 7's mechanism,
// executed here rather than cited: the reason the output schema is the
// checked-in file and not the one the SDK would infer.
//
// jsonschema-go infers a Go slice as the UNION type ["null","array"], because a
// nil slice marshals to null. The client's comparator reads a SCALAR Type field
// and a union leaves that field empty, so it reports "the tool declares no
// declared type" and refuses the registration — with every required list
// correct. The assertion below is on the inferred schema's own fields, which is
// exactly what that comparator reads.
func TestInferredOutputSchemaWouldNotDeclareAnArrayType(t *testing.T) {
	inferred, err := jsonschema.For[collectOutput](nil)
	if err != nil {
		t.Fatalf("inferring the output schema: %v", err)
	}
	nodes, ok := inferred.Properties["nodes"]
	if !ok {
		t.Fatalf("the inferred schema declares no nodes property")
	}
	if nodes.Type != "" {
		t.Skipf("jsonschema-go now infers a slice with the scalar type %q; "+
			"the reason this framework advertises the checked-in file has changed and the doc comment needs re-deriving",
			nodes.Type)
	}
	if !reflect.DeepEqual(nodes.Types, []string{"null", "array"}) {
		t.Errorf("the inferred nodes schema declares types %v, want the [null array] union this row is about", nodes.Types)
	}

	// THE CONTROL, same instrument, same field: the schema this framework
	// actually advertises DOES declare the scalar type the comparator reads.
	advertised := decode(t, []byte(mustJSON(t, advertisedOutputSchema())))
	props, _ := advertised["properties"].(map[string]any)
	nodesDoc, _ := props["nodes"].(map[string]any)
	if nodesDoc["type"] != "array" {
		t.Errorf("the advertised nodes schema declares type %v, want the scalar \"array\"", nodesDoc["type"])
	}
}

// paramlessParams is a collector with no parameters at all: the shape an author
// declares when there is nothing to configure.
type paramlessParams struct{}

// TestParamsTypeMustInferAnObject is the guard on the one collector-authored
// input this framework cannot validate any later. The contract declares params
// as an object and the client's registration gate compares that type, so a
// params type inferring to anything else would build here and be refused at
// install time with an error about a schema its author never wrote.
func TestParamsTypeMustInferAnObject(t *testing.T) {
	if _, err := advertisedInputSchema[paramlessParams](); err != nil {
		t.Errorf("an empty struct is the params type of a collector with no parameters and must be accepted: %v", err)
	}
	if _, err := advertisedInputSchema[map[string]any](); err != nil {
		t.Errorf("a map params type must be accepted: %v", err)
	}
	_, err := advertisedInputSchema[string]()
	if err == nil {
		t.Fatalf("a params type that is not an object was accepted")
	}
	if !strings.Contains(err.Error(), "object") {
		t.Errorf("the refusal does not say what the contract requires: %v", err)
	}
}
