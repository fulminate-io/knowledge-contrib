// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

// env_test.go — the per-target-OS environment table and the example config
// entry it produces.
//
// THE TABLE IS A FUNCTION OF A PARAMETER, NOT OF runtime.GOOS, and that is
// exactly what makes the Windows row assertable here: no venue in this
// repository runs Go tests on Windows, so a runtime.GOOS branch would be a row
// that passes everywhere by never running its subject.

// TestEnvTablePerTargetOS asserts all three rows on one runner.
func TestEnvTablePerTargetOS(t *testing.T) {
	for _, tc := range []struct {
		targetOS string
		want     []string
	}{
		{OSLinux, []string{EnvHome, EnvKubeconfig, EnvPath}},
		{OSDarwin, []string{EnvHome, EnvKubeconfig, EnvPath}},
		{OSWindows, []string{
			EnvHome, EnvHomeDrive, EnvHomePath, EnvKubeconfig, EnvPath, EnvSystemRoot, EnvUserProfile,
		}},
	} {
		t.Run(tc.targetOS, func(t *testing.T) {
			got, err := EnvNames(tc.targetOS, false)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("the %s entry lists %v, want %v", tc.targetOS, got, tc.want)
			}
		})
	}
}

// TestWindowsAddsTheHomeFallbacksAndTheSpawnRoot is the row a Linux-only venue
// would otherwise never observe.
func TestWindowsAddsTheHomeFallbacksAndTheSpawnRoot(t *testing.T) {
	linux, err := EnvNames(OSLinux, false)
	if err != nil {
		t.Fatal(err)
	}
	windows, err := EnvNames(OSWindows, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{EnvHomeDrive, EnvHomePath, EnvUserProfile, EnvSystemRoot} {
		if !slices.Contains(windows, name) {
			t.Errorf("the windows entry does not list %s", name)
		}
		if slices.Contains(linux, name) {
			t.Errorf("the linux entry lists the Windows-only %s", name)
		}
	}
}

// TestInClusterAddsExactlyTwoNames — and only when asked for.
func TestInClusterAddsExactlyTwoNames(t *testing.T) {
	without, err := EnvNames(OSLinux, false)
	if err != nil {
		t.Fatal(err)
	}
	with, err := EnvNames(OSLinux, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(with) != len(without)+2 {
		t.Fatalf("the in-cluster entry lists %v against %v", with, without)
	}
	for _, name := range []string{EnvKubernetesServiceHost, EnvKubernetesServicePort} {
		if !slices.Contains(with, name) {
			t.Errorf("the in-cluster entry does not list %s", name)
		}
		if slices.Contains(without, name) {
			t.Errorf("the ordinary entry lists the in-cluster-only %s", name)
		}
	}
}

// TestTheNamesLeftOffAreLeftOff — the decisions recorded in env.go, asserted so
// a later edit that quietly adds one is a red rather than a silent widening of
// what the child can be told.
func TestTheNamesLeftOffAreLeftOff(t *testing.T) {
	every := map[string]struct{}{}
	for _, targetOS := range []string{OSLinux, OSDarwin, OSWindows} {
		for _, inCluster := range []bool{false, true} {
			names, err := EnvNames(targetOS, inCluster)
			if err != nil {
				t.Fatal(err)
			}
			for _, n := range names {
				every[n] = struct{}{}
			}
		}
	}
	for _, off := range []string{
		"HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "NO_PROXY", "no_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
		"KUBERNETES_MASTER", "POD_NAMESPACE",
		"KUBE_CLIENT_BACKOFF_BASE", "KUBE_CLIENT_BACKOFF_DURATION",
		"AWS_ACCESS_KEY_ID", "GOOGLE_APPLICATION_CREDENTIALS",
	} {
		if _, present := every[off]; present {
			t.Errorf("the entry lists %q. Each omission in env.go is a recorded decision with its cost; "+
				"adding one silently widens what an operator's entry hands this process", off)
		}
	}
}

// TestUnknownTargetOSIsRefused — bad input errors.
func TestUnknownTargetOSIsRefused(t *testing.T) {
	if _, err := EnvNames("plan9", false); err == nil {
		t.Fatal("an unknown target OS was accepted; the table is defined for three")
	}
	if _, err := ExampleEntry("plan9", false, "k8s-logs"); err == nil {
		t.Fatal("an example entry was rendered for an unknown target OS")
	}
	if _, err := ExampleEntry(OSLinux, false, ""); err == nil {
		t.Fatal("an example entry was rendered with no command")
	}
}

// TestExampleEntryHasTheShapeTheLoaderRequires — the shape, the transport's own
// fields and nothing of the other transport's, and every env value a reference
// rather than a literal.
//
// THE LOADER ITSELF RUNS AT TICKET 14, and none of it runs here. This asserts
// the shape the loader's refusal set defines, restated in this test because the
// loader lives in a module this one cannot import — so the name says "has the
// shape the loader requires" rather than "is the entry the loader accepts",
// which would claim an acceptance nothing in this suite observes.
func TestExampleEntryHasTheShapeTheLoaderRequires(t *testing.T) {
	raw, err := ExampleEntry(OSLinux, false, "/home/you/.knowledge/bin/knowledge-collector-k8s-logs")
	if err != nil {
		t.Fatal(err)
	}

	var doc struct {
		Collectors map[string]struct {
			Type      string            `json:"type"`
			Command   string            `json:"command"`
			Tool      string            `json:"tool"`
			URL       string            `json:"url"`
			Headers   map[string]string `json:"headers"`
			Env       map[string]string `json:"env"`
			Behavior  map[string]any    `json:"behavior"`
			NodeTypes map[string]any    `json:"node_types"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("the example entry is not valid JSON: %v\n%s", err, raw)
	}
	entry, ok := doc.Collectors[GraphFamily]
	if !ok {
		t.Fatalf("the example entry is not named %q; the entry NAME is the graph family", GraphFamily)
	}
	if entry.Type != "stdio" {
		t.Errorf("entry type is %q, want stdio", entry.Type)
	}
	if entry.Command == "" || entry.Tool != ToolName {
		t.Errorf("entry command is %q and tool is %q, want a command and %q", entry.Command, entry.Tool, ToolName)
	}
	if entry.URL != "" || len(entry.Headers) != 0 {
		t.Errorf("the stdio entry carries the http transport's fields; the loader refuses that entry")
	}

	names, err := EnvNames(OSLinux, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.Env) != len(names) {
		t.Fatalf("the env block lists %d names, the table lists %d", len(entry.Env), len(names))
	}
	for _, n := range names {
		value, present := entry.Env[n]
		if !present {
			t.Errorf("the env block does not list %s", n)
			continue
		}
		// NO VALUE IS A BARE ${VAR}, and that is the load-bearing half: a
		// reference the serving process cannot resolve refuses the WHOLE scoped
		// config file, so an example teaching that shape costs an operator every
		// other collector in the scope, not just this one.
		if value == "${"+n+"}" {
			t.Errorf("env[%s] is the bare reference %q; one unresolvable reference makes the whole "+
				"collectors.json unreadable, taking every collector in that scope with it", n, value)
		}
		if strings.HasPrefix(value, "${") && !strings.HasSuffix(value, ":-}") {
			t.Errorf("env[%s] is the reference %q with no default; the example's fallback shape carries :-", n, value)
		}
	}
	// AND HOME IS A REAL PATH, not a reference of either spelling. An empty home
	// is the one value this collector documents as failing where an absent one
	// succeeds, and ${HOME:-} resolves to exactly that under a service-managed
	// daemon, whose own environment holds PATH and nothing else.
	home := entry.Env[EnvHome]
	if !strings.HasPrefix(home, "/") {
		t.Errorf("env[%s] is %q; the example shows a real home directory, because an EMPTY one fails "+
			"where an absent one succeeds", EnvHome, home)
	}
}

// TestExampleEnvValueDefaultsAnUnknownName exercises the helper's FALLBACK,
// which no rendered entry reaches.
//
// WHY IT IS TESTED HERE RATHER THAN THROUGH THE ENTRY. Every name EnvNames
// returns has a literal in the switch, so the assertions over a rendered entry
// never evaluate a reference and the fallback is unobserved by all of them. The
// input that reaches it is a name this module does not read YET: the first
// variable a later change adds arrives here before anyone writes its example,
// and whatever this returns is what the generated README then ships. A bare
// ${VAR} there would refuse the whole scoped config file for every collector an
// operator has registered, so the `:-` is the safety net for a change nobody has
// made yet, and this is the only place it can be seen working.
func TestExampleEnvValueDefaultsAnUnknownName(t *testing.T) {
	const added = "KN_A_NAME_ADDED_LATER"
	if got, want := exampleEnvValue(added), "${"+added+":-}"; got != want {
		t.Fatalf("the fallback for an undecided name is %q, want %q; it must carry :- so it cannot "+
			"refuse the whole scoped file", got, want)
	}
	// THE CONTROL: a name this collector DOES read never reaches the fallback,
	// so the case above is about the undecided name rather than about every name.
	for _, n := range []string{EnvHome, EnvKubeconfig, EnvPath} {
		if got := exampleEnvValue(n); strings.HasPrefix(got, "${") {
			t.Errorf("%s is a name this collector reads and its example must be a literal, got %q", n, got)
		}
	}
}

// TestBehaviorDeclaresAllThreeAndExcludesChunkContent. Summarizable and
// embeddable gate the TEXT INDEX; syncable gates SYNC, which is a separate
// mechanism the search fields do not reach.
func TestBehaviorDeclaresAllThreeAndExcludesChunkContent(t *testing.T) {
	raw, err := ExampleEntry(OSLinux, false, "k8s-logs")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Collectors map[string]struct {
			Behavior struct {
				Syncable        *bool    `json:"syncable"`
				Summarizable    *bool    `json:"summarizable"`
				Embeddable      *bool    `json:"embeddable"`
				BM25Fields      []string `json:"bm25_fields"`
				EmbedFields     []string `json:"embed_fields"`
				SummarizeFields []string `json:"summarize_fields"`
			} `json:"behavior"`
			NodeTypes map[string]struct {
				Embeddable *bool    `json:"embeddable"`
				BM25Fields []string `json:"bm25_fields"`
			} `json:"node_types"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	b := doc.Collectors[GraphFamily].Behavior

	for name, got := range map[string]*bool{
		"syncable": b.Syncable, "summarizable": b.Summarizable, "embeddable": b.Embeddable,
	} {
		if got == nil || !*got {
			t.Errorf("behavior.%s is %v; without it the graph is collected and then never "+
				"enters the index or the sync path", name, got)
		}
	}

	for name, fields := range map[string][]string{
		"bm25_fields": b.BM25Fields, "embed_fields": b.EmbedFields, "summarize_fields": b.SummarizeFields,
	} {
		if len(fields) == 0 {
			t.Errorf("behavior.%s is empty", name)
		}
		if slices.Contains(fields, "content") {
			t.Errorf("behavior.%s names `content` graph-wide. A chunk node's content is the COMPRESSED "+
				"payload, so naming it here indexes and summarizes compressed bytes for the graph's "+
				"largest node type", name)
		}
	}

	chunk, ok := doc.Collectors[GraphFamily].NodeTypes["log-chunk"]
	if !ok {
		t.Fatal("the entry declares no log-chunk override; a chunk carries no readable text at all")
	}
	if chunk.Embeddable == nil || *chunk.Embeddable {
		t.Error("the log-chunk override does not turn embedding off")
	}
	if slices.Contains(chunk.BM25Fields, "content") {
		t.Error("the log-chunk override still indexes content")
	}
}

// TestDescribeEnvNames is the README's own rendering, checked so the guide and
// the code cannot drift.
func TestDescribeEnvNames(t *testing.T) {
	got, err := DescribeEnvNames(OSLinux, false)
	if err != nil {
		t.Fatal(err)
	}
	if want := "HOME, KUBECONFIG, PATH"; got != want {
		t.Fatalf("DescribeEnvNames = %q, want %q", got, want)
	}
	if _, err := DescribeEnvNames("plan9", false); err == nil {
		t.Fatal("an unknown target OS was described")
	}
	if strings.Contains(got, "${") {
		t.Fatal("the description carries value references; it names NAMES")
	}
}

// readmePath is this module's README, relative to this package. It is a LITERAL
// relative path rather than one a resolver produced, which is what keeps the
// read inside the module root the test cache records.
const readmePath = "../../README.md"

// TestTheREADMEShipsTheGeneratedEntry — the documented entry and the generated
// one are the same bytes.
//
// They agreed when the README was written and nothing held them together, so
// the next change to the field lists or the tool name would have left the
// README quietly wrong — and an operator copying a stale entry gets a collector
// whose graph never enters the index, which reads as a broken collect rather
// than as a bad instruction.
func TestTheREADMEShipsTheGeneratedEntry(t *testing.T) {
	generated, err := ExampleEntry(OSLinux, false, "/home/you/.knowledge/bin/knowledge-collector-k8s-logs")
	if err != nil {
		t.Fatal(err)
	}
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("this module's README could not be read: %v", err)
	}
	if !strings.Contains(string(readme), generated) {
		t.Fatalf("the README's config entry is not the one ExampleEntry generates. Paste this in place of "+
			"the fenced block:\n\n%s", generated)
	}
}

// TestTheREADMESaysTheCloudBlockIsDeclared — the capability sentence.
//
// The paragraph once described a behavior no collect could reach, and then said
// so; the contract now carries the block, so what it owes is the OTHER honest
// sentence — that the block is declared in the operator's own entry rather than
// queried, so an entry declaring nothing receives nothing and the resulting zero
// is correct rather than a failure.
func TestTheREADMESaysTheCloudBlockIsDeclared(t *testing.T) {
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "The block is declared, never queried.") {
		t.Fatal("the README describes the cloud-context capability without saying the block is DECLARED in " +
			"the operator's entry, so a reader cannot tell why their own collect emitted no proxies")
	}
}
