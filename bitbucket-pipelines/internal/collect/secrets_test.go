// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// secrets_test.go — NO VARIABLE VALUE REACHES ANYTHING THIS COLLECTOR PRODUCES.
//
// The graph a collect lands in is indexed, replicated, embedded and searchable,
// so a value reaching it would be a credential in a searchable store. The
// fixture's variables carry values in their recorded responses precisely so this
// can be measured rather than asserted: the decoder declares no field for one,
// so the drop happens at DECODE and there is no point in the module at which the
// value exists as a Go string.

// plantedValues are the values the fixture's recorded responses carry. None of
// them may appear anywhere in what a collect produces.
var plantedValues = []string{
	"workspace-secret-value-should-never-be-stored",
	"repository-secret-value-should-never-be-stored",
	"workspace-scoped-deploy-key",
	"repository-scoped-deploy-key",
	"environment-scoped-deploy-key",
}

// TestNoVariableValueAppearsAnywhereInTheEmittedEnvelope is the allowlist row,
// asserted as ONE SUBSTRING SCAN over the marshaled envelope rather than as a
// per-field check.
//
// A PER-FIELD CHECK COVERS THE FIELDS SOMEONE REMEMBERED. The scan covers every
// node field, every metadata value, every content document, every summary and
// every edge — including a field added later that nobody thought to check.
func TestNoVariableValueAppearsAnywhereInTheEmittedEnvelope(t *testing.T) {
	nodes, edges, err := bbgraph.Build(walkTheFixture(t))
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	envelope := marshal(t, nodes) + marshal(t, edges)

	for _, planted := range plantedValues {
		// EVERY ENCODING THE OBJECT COULD CARRY IT IN. The envelope is JSON, so a
		// value could arrive raw, JSON-escaped, or base64 if some field ever
		// carried bytes; all three spellings are searched.
		for _, spelling := range encodings(t, planted) {
			if strings.Contains(envelope, spelling) {
				t.Errorf("the value %q reached the emitted envelope, as %q", planted, spelling)
			}
		}
	}

	// THE KNOWN POSITIVE. The variables themselves ARE in the envelope, by name,
	// so this is a scan over a result that carries the variables rather than one
	// that carries nothing.
	for _, want := range []string{"WORKSPACE_TOKEN", "API_KEY", "DEPLOY_KEY"} {
		if !strings.Contains(envelope, want) {
			t.Errorf("the variable %q is missing from the envelope, so the absence of its value "+
				"proves nothing", want)
		}
	}
}

// TestTheDecoderDeclaresNoFieldForAValue is the STRUCTURAL half: the drop
// happens at decode rather than at emit, so there is no point at which a
// redaction could be forgotten.
//
// THE OBSERVABLE IS THE NODE'S CONTENT DOCUMENT. It is the marshaled detail
// struct, and it decodes into exactly three keys — a struct that had grown a
// value field would show a fourth here.
func TestTheDecoderDeclaresNoFieldForAValue(t *testing.T) {
	got := walkTheFixture(t)
	variable, ok := resourceByID(got, "bitbucket:acme/Variable/repository/api/API_KEY")
	if !ok {
		t.Fatal("no variable node to inspect")
	}

	var content map[string]json.RawMessage
	if err := json.Unmarshal([]byte(variable.Content), &content); err != nil {
		t.Fatalf("the variable's content does not decode: %v", err)
	}
	for key := range content {
		switch key {
		case "key", "scope", "secured":
		default:
			t.Errorf("a variable node's content carries the key %q; the whole of what it may "+
				"store is the key, the scope and the secured flag", key)
		}
	}
	if len(content) != 3 {
		t.Errorf("a variable node's content carries %d keys, want exactly 3: %v",
			len(content), content)
	}
}

// TestTheVariableNodesMetadataIsTheAllowlistAndNothingElse.
func TestTheVariableNodesMetadataIsTheAllowlistAndNothingElse(t *testing.T) {
	got := walkTheFixture(t)
	allowed := map[string]bool{
		"workspace": true, "key": true, "scope": true, "secured": true,
		"repo": true, "environment": true,
	}
	var checked int
	for _, res := range got.Resources {
		if res.ResourceType != bbgraph.ResourceTypeVariable {
			continue
		}
		checked++
		for key := range res.Metadata {
			if !allowed[key] {
				t.Errorf("the variable node %q carries the metadata key %q, which is outside the "+
					"allowlist", res.ID, key)
			}
		}
	}
	if checked == 0 {
		t.Fatal("the walk emitted no variable nodes; the assertion above means nothing")
	}
}

// TestNoCredentialOrVariableValueReachesTheLog. The client logs the request URL
// on a rate limit, so a credential built into a URL rather than sent as HTTP
// Basic would land in an operator's log; and a converter that logged what it
// decoded would land a variable value there.
func TestNoCredentialOrVariableValueReachesTheLog(t *testing.T) {
	var captured strings.Builder
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(previous) })

	// A whole walk, plus a rate-limited read so the one log line this module
	// writes at all is produced in the same run.
	fixture := newFixture(t)
	fixture.status[repoVariablesPath(fixtureAPIRepo)] = 429
	if _, err := runOne(t, fixture, "bitbucket-variables"); err == nil {
		t.Fatal("the rate-limited read produced no error, so the log line was never written")
	}

	logged := captured.String()
	if !strings.Contains(logged, "rate limited") {
		t.Fatal("no log line was captured at all; the assertions below would pass for the wrong " +
			"reason")
	}
	for _, forbidden := range append([]string{"fixture-app-password"}, plantedValues...) {
		for _, spelling := range encodings(t, forbidden) {
			if strings.Contains(logged, spelling) {
				t.Errorf("the log carries %q, as %q", forbidden, spelling)
			}
		}
	}
}

// encodings is every spelling a value could reach a JSON document or a log line
// in: raw, JSON-escaped, and base64.
func encodings(t *testing.T, value string) []string {
	t.Helper()
	escaped, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("escaping %q: %v", value, err)
	}
	return []string{
		value,
		strings.Trim(string(escaped), `"`),
		base64.StdEncoding.EncodeToString([]byte(value)),
	}
}

// TestNoDecoderInThisModuleDeclaresAValueField is the STRUCTURAL half of the
// omission, and it is a source census rather than a behavioral assertion.
//
// WHY THE BEHAVIORAL ROWS ARE NOT ENOUGH, measured rather than assumed: adding a
// `Value` field to the variable decoder leaves every scan above GREEN, because
// the stored content is a separate sanitized struct and the metadata is an
// allowlist. So the value would exist as a decoded Go string in this process,
// one careless `marshalDetail(variable)` away from the graph, and nothing would
// have said so. The omission is the defense, and this is what observes it.
//
// IT COVERS THE WHOLE MODULE rather than the one struct, because the next
// decoder to be written is the one this rule is for.
func TestNoDecoderInThisModuleDeclaresAValueField(t *testing.T) {
	files := moduleSourceFiles(t)
	if len(files) == 0 {
		t.Fatal("the census found no source files; it is not reading the module")
	}

	// THE KNOWN POSITIVE, through the same matcher in the same run.
	if !declaresAValueField(`type x struct {
	Value string ` + "`json:\"value\"`" + `
}`) {
		t.Fatal("the matcher does not fire on a planted value field; its zero below would mean " +
			"nothing")
	}

	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("reading %s: %v", path, err)
		}
		if declaresAValueField(string(raw)) {
			t.Errorf("%s declares a field carrying the provider's `value`. A variable's value is "+
				"write-only to this collector and must not exist as a decoded string anywhere in "+
				"it: the decoder's OMISSION is what keeps a credential out of a searchable, "+
				"replicated, embedded graph", filepath.Base(path))
		}
	}
}

// declaresAValueField reports whether a source text declares a struct field
// bound to the provider's `value` key.
func declaresAValueField(source string) bool {
	return strings.Contains(source, "`json:\"value\"`") ||
		strings.Contains(source, "`json:\"value,")
}

// moduleSourceFiles is every non-test Go file of this module.
func moduleSourceFiles(t *testing.T) []string {
	t.Helper()
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("locating this package: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("no go.mod above this package")
		}
		root = parent
	}
	var files []string
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	return files
}
