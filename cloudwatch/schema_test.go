// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// schema_test.go — what this collector's own advertised parameters declare, and
// what its configuration entry says.
//
// THE SCHEMA IS READ BACK FROM THE RUNNING PROVIDER, never from a checked-in
// copy of it: what a client validates against is what the provider advertised
// over the wire, and a copy in this package could agree with a schema the
// provider does not serve.

// TestAdvertisedParamsDeclareTheLogGroupsAndTheWindow is R2's params row: the
// tool's own sub-schema names the source and the time range, and marks required
// exactly what the collector cannot run without.
func TestAdvertisedParamsDeclareTheLogGroupsAndTheWindow(t *testing.T) {
	params := advertisedParams(t)

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised params sub-schema declares no properties: %s", mustJSON(t, params))
	}
	for _, want := range []string{"log_groups", "start_time", "end_time"} {
		if _, declared := props[want]; !declared {
			t.Errorf("the advertised params sub-schema declares no %q property: %s", want, mustJSON(t, props))
		}
	}

	required, ok := params["required"].([]any)
	if !ok || len(required) == 0 {
		t.Fatalf("the advertised params sub-schema marks nothing required; a collector that cannot run "+
			"without a log group must say so: %s", mustJSON(t, params))
	}
	if !slices.Contains(required, any("log_groups")) {
		t.Errorf("the advertised params required list is %v, want it to carry log_groups", required)
	}
	// The control that keeps the assertion above from being about the envelope
	// rather than this collector's sub-schema: the ENVELOPE's own required list
	// is exactly the collect id, and params is not on it.
	envelopeRequired := advertisedInput(t)["required"].([]any)
	if len(envelopeRequired) != 1 || envelopeRequired[0] != "id" {
		t.Errorf("the advertised envelope requires %v, want exactly [id]", envelopeRequired)
	}
}

// TestACollectWithNoLogGroupIsRefusedBeforeTheWalk is R2's refusal row: the
// client validates against the advertised schema, so a paramless collect never
// reaches the handler.
func TestACollectWithNoLogGroupIsRefusedBeforeTheWalk(t *testing.T) {
	p := dialStdioProvider(t, modeServe)

	res, err := p.call(t, map[string]any{"id": "instance-1", "params": map[string]any{}})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("a collect naming no log group was accepted")
	}
	text := refusalText(res, err)
	if !strings.Contains(text, "log_groups") {
		t.Errorf("the refusal does not name the missing key: %s", text)
	}

	// THE SAME-RUN CONTROL: the identical call WITH the key present reaches the
	// handler, so the refusal above is attributable to the missing key and not
	// to the provider refusing everything.
	res, err = p.call(t, map[string]any{
		"id":     "instance-1",
		"params": map[string]any{"log_groups": []any{"/g"}},
	})
	if err != nil {
		t.Fatalf("the call with the key present failed at the transport: %v", err)
	}
	reached := refusalText(res, nil)
	if strings.Contains(reached, "log_groups") {
		t.Errorf("the call with the key present was still refused for the key: %s", reached)
	}
	// The walk itself fails on this machine, which holds no AWS credential —
	// that arm is asserted in TestCredentialChainFailureNamesTheChain. What
	// this control shows is that validation let the call through.
	if !strings.Contains(reached, "credential") && !strings.Contains(reached, "region") {
		t.Errorf("the call with the key present did not reach the walk: %s", reached)
	}
}

// TestConfigurationEntryDeclaresTheBehaviorBlock is R2's behavior row. Without
// all three flags the collected graph is stored and walkable but never enters
// the text index, and a search against it reads zero forever.
func TestConfigurationEntryDeclaresTheBehaviorBlock(t *testing.T) {
	entry := ExampleEntry(TargetPOSIX, exampleCommand)
	if !entry.Behavior.Syncable || !entry.Behavior.Summarizable || !entry.Behavior.Embeddable {
		t.Errorf("behavior = %+v, want all three true", entry.Behavior)
	}
	if len(entry.Behavior.BM25Fields) == 0 {
		t.Error("the behavior block names no bm25_fields, so nothing decides which text is indexed")
	}
	if !slices.Contains(entry.Behavior.BM25Fields, "content") {
		t.Errorf("bm25_fields = %v, want it to carry content — a chunk's entry block is its content",
			entry.Behavior.BM25Fields)
	}
	// THE COMMAND IS THE ONE THE CALLER PASSED, and its basename is the binary
	// the release publishes. It used to be compared to the bare binaryName,
	// which is why the generator emitted a bare command for as long as it did:
	// a command the daemon resolves against its OWN PATH and never finds.
	if entry.Type != "stdio" || entry.Command != exampleCommand || entry.Tool != toolName {
		t.Errorf("entry = {type:%q command:%q tool:%q}, want the stdio shape naming this command and tool",
			entry.Type, entry.Command, entry.Tool)
	}
	if filepath.Base(entry.Command) != binaryName {
		t.Errorf("the entry's command basename is %q, and the release publishes %q",
			filepath.Base(entry.Command), binaryName)
	}
}

// TestExampleEntryCarriesEveryDeclaredEnvironmentName pins that the generated
// entry and the declared environment cannot drift apart.
func TestExampleEntryCarriesEveryDeclaredEnvironmentName(t *testing.T) {
	for _, target := range []TargetOS{TargetPOSIX, TargetWindows} {
		entry := ExampleEntry(target, exampleCommand)
		declared := DeclaredEnv(target)
		if len(entry.Env) != len(declared) {
			t.Errorf("[%s] the entry carries %d environment names, the declaration names %d",
				target, len(entry.Env), len(declared))
		}
		for _, name := range declared {
			if _, present := entry.Env[name]; !present {
				t.Errorf("[%s] the entry does not carry the declared name %s", target, name)
			}
		}
	}
	body, err := ExampleEntryJSON(TargetPOSIX)
	if err != nil {
		t.Fatalf("rendering the entry: %v", err)
	}
	if !strings.Contains(body, `"cloudwatch"`) {
		t.Errorf("the rendered entry is not keyed by the registration name:\n%s", body)
	}
}

// advertisedInput reads the whole advertised input schema back from a running
// provider.
func advertisedInput(t *testing.T) map[string]any {
	t.Helper()
	p := dialStdioProvider(t, modeServe)
	tool := p.advertised(t)
	body, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshaling the advertised input schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		t.Fatalf("decoding the advertised input schema: %v", err)
	}
	return doc
}

// advertisedParams reads the params sub-schema out of the advertised input.
func advertisedParams(t *testing.T) map[string]any {
	t.Helper()
	doc := advertisedInput(t)
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no properties: %s", mustJSON(t, doc))
	}
	params, ok := props["params"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no params object: %s", mustJSON(t, props))
	}
	return params
}

// refusalText renders whatever a call came back with, so a refusal can be
// inspected whether it arrived as an error result or as a transport error.
func refusalText(res *mcp.CallToolResult, err error) string {
	if err != nil {
		return err.Error()
	}
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// mustJSON renders a value for a failure message.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	body, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshaling for the failure message: %v", err)
	}
	return string(body)
}

// TestAnExplicitZeroBoundIsRefusedThroughTheLiveProvider carries the
// zero-versus-absent distinction across the whole seam rather than asserting it
// on the validator alone.
//
// IT IS NOT REDUNDANT WITH THE UNIT ARM. Between a caller and the validator sit
// the advertised schema and the argument decoding, and either could erase the
// distinction: a schema that declared the field non-nullable would refuse the
// zero with a type error naming nothing useful, and a decoding that dropped an
// explicit zero would hand the walk an absent bound and drain the source. Only
// a call through the real provider observes which of the three happened.
func TestAnExplicitZeroBoundIsRefusedThroughTheLiveProvider(t *testing.T) {
	p := dialStdioProvider(t, modeServe)

	res, err := p.call(t, map[string]any{
		"id":     "instance-1",
		"params": map[string]any{"log_groups": []any{"/g"}, "max_entries": 0},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("a collect naming max_entries 0 was accepted: %v", res)
	}
	text := refusalText(res, err)
	if !strings.Contains(text, "max_entries") {
		t.Errorf("the refusal does not name the parameter: %s", text)
	}
	// The refusal must be THIS collector's, naming the distinction, not a
	// schema type error from the layer above.
	if !strings.Contains(text, "omit the key") {
		t.Errorf("the refusal does not tell the caller what to do instead: %s", text)
	}

	// THE CONTROL, in the same run: the same collect with the key OMITTED gets
	// past validation and reaches the walk, which then fails at the credential
	// chain on this machine. That is what shows the zero was refused for its
	// value rather than for the key being present at all.
	res, err = p.call(t, map[string]any{
		"id":     "instance-1",
		"params": map[string]any{"log_groups": []any{"/g"}},
	})
	reached := refusalText(res, err)
	if strings.Contains(reached, "max_entries") {
		t.Errorf("a collect that omitted max_entries was still refused for it: %s", reached)
	}
	if !strings.Contains(strings.ToLower(reached), "credential") {
		t.Errorf("the collect with the key omitted did not reach the walk: %s", reached)
	}
}
