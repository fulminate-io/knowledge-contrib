// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// requirements_test.go — the Slack collector ticket's requirement rows that the
// served wire states: what the describe tool declares, what the collect tool
// advertises inside params, and what a collect naming no target or carrying no
// token answers. The child each row dials is this test binary running the
// module's own main(), so a row names no package or type of the module.

// reqChildEnv switches this test binary into the collector child.
const reqChildEnv = "KNOWLEDGE_SLACK_REQUIREMENTS_CHILD"

func init() {
	if os.Getenv(reqChildEnv) == "1" {
		main()
		os.Exit(0)
	}
}

// reqDeclaration is the subset of the describe document these rows read.
type reqDeclaration struct {
	NodeTypes   []string `json:"node_types"`
	EdgeTypes   []string `json:"edge_types"`
	Environment []struct {
		Name  string `json:"name"`
		Class string `json:"class"`
	} `json:"environment"`
}

// reqDial starts the collector child with exactly the environment given here
// plus PATH, every proxy variable pointed at a closed local port, and connects
// to it over stdio.
func reqDial(t *testing.T, env ...string) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append([]string{
		reqChildEnv + "=1",
		"PATH=" + os.Getenv("PATH"),
		"HTTPS_PROXY=http://127.0.0.1:9",
		"HTTP_PROXY=http://127.0.0.1:9",
		"https_proxy=http://127.0.0.1:9",
		"http_proxy=http://127.0.0.1:9",
	}, env...)
	cmd.Dir = t.TempDir()
	cmd.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "slack-requirements", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the collector child: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// reqDescribe calls the describe tool and decodes its document.
func reqDescribe(t *testing.T, s *mcp.ClientSession) reqDeclaration {
	t.Helper()
	res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: "describe", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("calling describe: %v", err)
	}
	if res.IsError {
		t.Fatalf("describe answered an error: %s", reqText(t, res.Content))
	}
	var decl reqDeclaration
	if err := json.Unmarshal(reqJSON(t, res.StructuredContent), &decl); err != nil {
		t.Fatalf("decoding the describe document: %v", err)
	}
	return decl
}

// reqSecrets is every environment name the declaration classes secret.
func reqSecrets(decl reqDeclaration) []string {
	var names []string
	for _, e := range decl.Environment {
		if e.Class == "secret" {
			names = append(names, e.Name)
		}
	}
	return names
}

// reqCollectTool is the one served tool that is not describe.
func reqCollectTool(t *testing.T, s *mcp.ClientSession) *mcp.Tool {
	t.Helper()
	tools, err := s.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	var collect []*mcp.Tool
	for _, tool := range tools.Tools {
		if tool.Name != "describe" {
			collect = append(collect, tool)
		}
	}
	if len(collect) != 1 {
		t.Fatalf("the collector serves %d tools beside describe, want exactly one collect tool", len(collect))
	}
	return collect[0]
}

// reqParams is the params sub-schema's properties and required list.
func reqParams(t *testing.T, tool *mcp.Tool) (map[string]any, []string) {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(reqJSON(t, tool.InputSchema), &doc); err != nil {
		t.Fatalf("decoding the collect tool's input schema: %v", err)
	}
	top, _ := doc["properties"].(map[string]any)
	params, _ := top["params"].(map[string]any)
	props, _ := params["properties"].(map[string]any)
	var required []string
	if list, ok := params["required"].([]any); ok {
		for _, r := range list {
			if s, ok := r.(string); ok {
				required = append(required, s)
			}
		}
	}
	return props, required
}

// reqIsArray reports whether a property schema admits a JSON array.
func reqIsArray(schema any) bool {
	m, _ := schema.(map[string]any)
	switch v := m["type"].(type) {
	case string:
		return v == "array"
	case []any:
		return slices.Contains(v, any("array"))
	}
	return false
}

// reqProperty is the first property, by name, whose name matches the pattern.
func reqProperty(props map[string]any, pattern string, wantArray bool) (string, bool) {
	re := regexp.MustCompile(pattern)
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if re.MatchString(name) && (!wantArray || reqIsArray(props[name])) {
			return name, true
		}
	}
	return "", false
}

// reqCall calls the collect tool and reports whether the collect was refused,
// with the refusal's text.
func reqCall(t *testing.T, s *mcp.ClientSession, tool string, args map[string]any) (bool, string) {
	t.Helper()
	res, err := s.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		return true, err.Error()
	}
	if res.IsError {
		return true, reqText(t, res.Content)
	}
	return false, string(reqJSON(t, res.StructuredContent))
}

// reqNamesOneOf reports whether text carries one of the names.
func reqNamesOneOf(text string, names []string) bool {
	for _, n := range names {
		if n != "" && strings.Contains(text, n) {
			return true
		}
	}
	return false
}

// reqTypesMatch reports whether any declared type matches the pattern.
func reqTypesMatch(types []string, pattern string) bool {
	return slices.ContainsFunc(types, regexp.MustCompile(pattern).MatchString)
}

func reqText(t *testing.T, content []mcp.Content) string {
	t.Helper()
	return string(reqJSON(t, content))
}

func reqJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	return b
}

func reqKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// reqPlaceholderSecrets sets every declared secret to a value that is no token.
func reqPlaceholderSecrets(decl reqDeclaration) []string {
	var env []string
	for _, name := range reqSecrets(decl) {
		env = append(env, name+"=requirements-placeholder-not-a-token")
	}
	return env
}

// TestRequirement3_DescribeDeclaresChannelsMessagesRepliesAndUsers is
// requirement 3: the collector declares, through describe, the channels,
// messages, thread replies and posting users it emits, and the relations
// between them.
func TestRequirement3_DescribeDeclaresChannelsMessagesRepliesAndUsers(t *testing.T) {
	decl := reqDescribe(t, reqDial(t))
	for _, want := range []struct{ noun, pattern string }{
		{"channels", `(?i)channel`},
		{"messages", `(?i)message`},
		{"the users who posted", `(?i)user`},
	} {
		if !reqTypesMatch(decl.NodeTypes, want.pattern) {
			t.Errorf("describe declares no node type for %s: node_types %v", want.noun, decl.NodeTypes)
		}
	}
	if !reqTypesMatch(slices.Concat(decl.NodeTypes, decl.EdgeTypes), `(?i)repl|thread`) {
		t.Errorf("describe declares neither a node type nor an edge type for thread replies: node_types %v, edge_types %v",
			decl.NodeTypes, decl.EdgeTypes)
	}
	if len(decl.EdgeTypes) == 0 {
		t.Errorf("describe declares no edge type, so the relations between channels, messages, replies and users are not declared")
	}
}

// TestRequirement4_TheTokenIsDeclaredAsASecret is requirement 4's declared
// half: the token is a secret-class variable, which is the class the installer
// copies into the collector's own credential file behind a ${NAME} reference.
func TestRequirement4_TheTokenIsDeclaredAsASecret(t *testing.T) {
	decl := reqDescribe(t, reqDial(t))
	if len(reqSecrets(decl)) == 0 {
		t.Errorf("describe declares no secret-class environment variable, so no token reaches this collector "+
			"through its credential file: environment %+v", decl.Environment)
	}
}

// TestRequirement1_TheTargetIsOneOrMoreChannelsWithAnOptionalTimeWindow is
// requirement 1's selection: params name one or more channels, and a time
// window a collect may leave out.
func TestRequirement1_TheTargetIsOneOrMoreChannelsWithAnOptionalTimeWindow(t *testing.T) {
	props, required := reqParams(t, reqCollectTool(t, reqDial(t)))
	if _, ok := reqProperty(props, `(?i)channel`, true); !ok {
		t.Errorf("params declares no list of channels: %v", reqKeys(props))
	}
	window, ok := reqProperty(props, `(?i)oldest|latest|since|until|start|end|after|before|from|window`, false)
	if !ok {
		t.Errorf("params declares no time-window bound: %v", reqKeys(props))
	} else if slices.Contains(required, window) {
		t.Errorf("the time-window bound %q is required; requirement 1 makes the window optional", window)
	}
}

// TestRequirement1_ACollectNamingNoTargetIsRefused is requirement 1's refusal:
// a collect that names no channel is refused, naming the params it needs, and
// the same-run control shows a collect that names one is refused for a
// different cause.
func TestRequirement1_ACollectNamingNoTargetIsRefused(t *testing.T) {
	decl := reqDescribe(t, reqDial(t))
	s := reqDial(t, reqPlaceholderSecrets(decl)...)
	tool := reqCollectTool(t, s)
	props, _ := reqParams(t, tool)
	if len(props) == 0 {
		t.Fatalf("the collect tool declares no params property, so no collect can name a target")
	}
	names := reqKeys(props)

	cases := []struct {
		name string
		args map[string]any
	}{
		{"no params key", map[string]any{"id": "requirements"}},
		{"empty params", map[string]any{"id": "requirements", "params": map[string]any{}}},
	}
	for _, name := range names {
		if reqIsArray(props[name]) {
			cases = append(cases, struct {
				name string
				args map[string]any
			}{"only " + name + " and it is empty", map[string]any{"id": "requirements", "params": map[string]any{name: []any{}}}})
		}
	}
	for _, c := range cases {
		refused, text := reqCall(t, s, tool.Name, c.args)
		if !refused {
			t.Errorf("%s: a collect naming no target was accepted: %s", c.name, text)
			continue
		}
		if !reqNamesOneOf(text, names) {
			t.Errorf("%s: the refusal names none of the params %v: %s", c.name, names, text)
		}
	}

	// Control: one named channel and no token is refused for the token.
	channels, ok := reqProperty(props, `(?i)channel`, true)
	if !ok {
		t.Fatalf("params declares no list of channels, so the control cannot name one: %v", names)
	}
	secrets := reqSecrets(decl)
	if len(secrets) == 0 {
		t.Fatalf("describe declares no secret-class variable, so the control has no token to leave out")
	}
	bare := reqDial(t)
	refused, text := reqCall(t, bare, tool.Name, map[string]any{
		"id": "requirements", "params": map[string]any{channels: []any{"C0123456789"}},
	})
	if !refused || !reqNamesOneOf(text, secrets) {
		t.Errorf("control: a collect naming a channel with no token was not refused naming one of %v: refused=%v %s",
			secrets, refused, text)
	}
}

// TestRequirement4_AMissingTokenIsRefusedNamingIt is requirement 4's missing
// half: a collect naming a channel with no token in its environment fails, and
// the error names the variable it needed.
func TestRequirement4_AMissingTokenIsRefusedNamingIt(t *testing.T) {
	decl := reqDescribe(t, reqDial(t))
	secrets := reqSecrets(decl)
	if len(secrets) == 0 {
		t.Fatalf("describe declares no secret-class variable: environment %+v", decl.Environment)
	}
	s := reqDial(t)
	tool := reqCollectTool(t, s)
	props, _ := reqParams(t, tool)
	channels, ok := reqProperty(props, `(?i)channel`, true)
	if !ok {
		t.Fatalf("params declares no list of channels: %v", reqKeys(props))
	}
	refused, text := reqCall(t, s, tool.Name, map[string]any{
		"id": "requirements", "params": map[string]any{channels: []any{"C0123456789"}},
	})
	if !refused {
		t.Fatalf("a collect with no token was accepted: %s", text)
	}
	if !reqNamesOneOf(text, secrets) {
		t.Errorf("the refusal names none of the declared token variables %v: %s", secrets, text)
	}
}
