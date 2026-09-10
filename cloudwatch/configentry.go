// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// configentry.go — THE CONFIGURATION ENTRY THIS COLLECTOR IS INSTALLED WITH,
// generated rather than only documented.
//
// WHY IT IS CODE. Two of the entry's parts are facts about this binary that
// prose cannot keep honest: the environment names the AWS credential chain
// reads (which move when a dependency pin moves) and the behavior block that
// decides whether the collected graph is searchable at all. Generating the
// entry makes both testable as pure values, so a name dropped from the
// environment list or a behavior flag flipped to false is a red rather than a
// documentation drift nobody reads.
//
// THE ENTRY IS NOT WRITTEN BY THIS PROGRAM. An operator installs a collector by
// writing the entry into the collector configuration file, by hand or through
// the client's own command. This function renders what that entry should say.

// entryName is the registration name, which IS the graph family this
// collector's results land in. It is not a parameter: the ticket's observable
// names this family, and a collector installed under a different name writes a
// different graph.
const entryName = "cloudwatch"

// binaryName is the executable an entry's command names. It is the name the
// published release archive carries — one archive per collector per platform,
// each holding a single binary called knowledge-collector-<collector> — so an
// operator who installed from a release and an operator who built from source
// write the same entry.
const binaryName = "knowledge-collector-cloudwatch"

// exampleCommand is the path the documented entry names. It is ABSOLUTE, and
// that is a property of how the daemon starts a collector rather than a
// formatting choice: an entry's command is resolved once with exec.LookPath
// against the DAEMON's own PATH, which under a service manager is a handful of
// system directories and never the directory collectors are installed into. A
// bare binary name in a worked entry is an entry that cannot start.
//
// IT IS A PARAMETER ON ExampleEntry AND THIS IS ONLY THE DOCUMENTED DEFAULT,
// because an operator installs the binary wherever they like and an example that
// hard-coded one path would be wrong for everyone else. The install script
// writes the real path.
const exampleCommand = "/home/you/.knowledge/bin/" + binaryName

// Behavior is the entry's behavior block.
//
// ALL THREE FLAGS ARE TRUE DELIBERATELY, and this is the opposite lifecycle
// from the built-in log graph, which is never summarized, never embedded and
// never synced. A family registered with summarizable and embeddable unset or
// false is collected, stored and walkable but never enters the text index — a
// search against it reads zero and keeps reading zero, with nothing to
// distinguish that from an empty graph.
type Behavior struct {
	Syncable     bool `json:"syncable"`
	Summarizable bool `json:"summarizable"`
	Embeddable   bool `json:"embeddable"`
	// BM25Fields decides WHICH text is indexed. Summary and content are the two
	// this collector fills: a chunk's content is its entry block, and a
	// summary is written for it by the client's own pipeline.
	BM25Fields []string `json:"bm25_fields"`
}

// Entry is one collector configuration entry.
type Entry struct {
	Type     string            `json:"type"`
	Command  string            `json:"command"`
	Env      map[string]string `json:"env"`
	Tool     string            `json:"tool"`
	Behavior Behavior          `json:"behavior"`
	// Context is the foreign-graph context this collector declares it needs. It
	// is part of the ENTRY rather than a request made at collect time: the client
	// fills exactly what is declared here and supplies nothing that is not, so an
	// entry without it receives an empty block and every cross-graph edge this
	// collector could have emitted is lost silently. See declaration.go.
	Context framework.ForeignContextDeclaration `json:"context,omitempty"`
}

// ExampleEntry renders this collector's configuration entry for a target
// operating system.
//
// EVERY DECLARED ENVIRONMENT NAME IS PRESENT WITH A SUBSTITUTION PLACEHOLDER
// rather than a value. The entry is the child's WHOLE environment, so a name
// the entry does not carry is absent in the collector however the operator's
// own shell is set up; the placeholder is what an operator replaces or lets
// their own tooling expand.
//
// THE `:-` DEFAULT FORM IS LOAD-BEARING AND THE BARE ${VAR} WAS A DEFECT. The
// config contract's expansion refuses a reference that is unset and carries no
// default, and the refusal is not scoped to the entry — it fails the operator's
// WHOLE config file, taking every other collector they registered with it. This
// entry declares around fifty AWS names and almost all of them are genuinely
// optional, so a bare reference made the documented block unloadable for any
// operator who had not exported all fifty. The k8s collector's renderer states
// the same rule for the same reason (registration.go, "an unset variable with no
// default is a LOUD failure by the config contract's expansion rule").
//
// IT COSTS NOTHING AN OPERATOR WANTED. `${VAR:-}` resolves an unset name to the
// empty string, which for every name here is the same input the AWS credential
// chain sees when the variable is absent.
func ExampleEntry(target TargetOS, command string) Entry {
	env := make(map[string]string, len(DeclaredEnv(target)))
	for _, name := range DeclaredEnv(target) {
		env[name] = "${" + name + ":-}"
	}
	return Entry{
		Type:    "stdio",
		Command: command,
		Env:     env,
		Tool:    toolName,
		Behavior: Behavior{
			Syncable:     true,
			Summarizable: true,
			Embeddable:   true,
			BM25Fields:   []string{"summary", "content"},
		},
		Context: declaredForeignContext(),
	}
}

// ExampleEntryJSON renders the entry as the configuration file's own JSON,
// keyed by the registration name, at the documented example command.
//
// THE COMMAND IS NOT A PARAMETER HERE and ExampleEntry's is: this is the
// DOCUMENTED entry, and the README's own gate requires an absolute path an
// operator can see and edit. A caller rendering an entry for a real install
// passes its own path to ExampleEntry.
func ExampleEntryJSON(target TargetOS) (string, error) {
	body, err := json.MarshalIndent(
		map[string]any{"collectors": map[string]Entry{entryName: ExampleEntry(target, exampleCommand)}}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cloudwatch: rendering the example configuration entry: %w", err)
	}
	return string(body), nil
}
