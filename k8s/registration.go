// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// registration.go — THE CONFIG ENTRY AN OPERATOR INSTALLS, generated from this
// collector's own declarations so the documented example and what this process
// can actually receive are one artifact.
//
// WHY IT IS GENERATED RATHER THAN WRITTEN INTO THE README BY HAND. The env
// block IS the child process's whole environment: the daemon copies nothing
// from its own and adds nothing. A hand-written example that drifted from
// envAllowlist would tell an operator to set a variable this collector no
// longer reads, or — the failure that is silent — omit one it does. Generating
// the example from the same value the collector uses makes the two impossible
// to separate.
//
// EVERY VALUE IS A ${VAR:-} REFERENCE, NEVER A LITERAL. The operator sets the
// variables in their own environment and the expansion carries them in, so no
// credential is ever written into a config file. The `:-` default form is used
// because an unset variable with no default is a LOUD failure by the config
// contract's expansion rule, and most of these names are genuinely optional.
//
// THE BEHAVIOR BLOCK IS NOT A DEFAULT TO BE INHERITED. A graph registered
// without the opt-in is collected, stored and walkable but never enters the
// text index — the search reads zero and keeps reading zero, with nothing that
// looks like a failure. So all three flags are written EXPLICITLY, and the
// fields to index are named rather than left for something else to guess.

// registrationName is this collector's name in its config entry, and therefore
// the GRAPH FAMILY its nodes land in. The two are the same string by design; a
// collector cannot choose a family different from the name it is registered
// under.
const registrationName = "k8s"

// behavior is the indexing opt-in this collector's graph needs.
type behavior struct {
	Summarizable bool `json:"summarizable"`
	Embeddable   bool `json:"embeddable"`
	Syncable     bool `json:"syncable"`
	// The three field lists name WHAT to index. An empty list is not a
	// sensible default here: a Kubernetes node's searchable text is its
	// symbol name, its description and its body, and nothing else would be
	// found by a search for a workload by name.
	BM25Fields      []string `json:"bm25_fields"`
	SummarizeFields []string `json:"summarize_fields"`
	EmbedFields     []string `json:"embed_fields"`
}

// declaredBehavior is the opt-in this collector's registration carries.
func declaredBehavior() behavior {
	return behavior{
		Summarizable:    true,
		Embeddable:      true,
		Syncable:        true,
		BM25Fields:      []string{"symbol_name", "description", "content"},
		SummarizeFields: []string{"content"},
		EmbedFields:     []string{"summary", "description"},
	}
}

// azureNodeTypeCloudResource is the node type the AZURE collector emits, spelled
// here because that module is a separate Go module this one cannot import.
//
// IT IS DELIBERATELY NOT nodeTypeCloudResource, THIS COLLECTOR'S OWN CONSTANT,
// even though the two strings are equal today. They are facts about different
// collectors: one is what this walk writes, the other is what the azure walk
// writes and what this declaration must ask for. Sharing the constant would make
// an azure-side rename invisible here — the declaration would keep naming this
// collector's own spelling and keep selecting nothing, which is the defect this
// declaration shipped with. The cross-module census in the root module is what
// holds this copy against the azure module's own constant.
//
// WHAT IT USED TO SAY WAS `azure-resource`, WHICH NOTHING EMITS. The azure
// collector emits ONE node type and puts the Azure resource kind in the
// `resource_type` metadata key; the client's fill filters by exact string
// equality, so this entry selected zero nodes, cloudIdentitiesByClientID indexed
// an empty set, and every WORKLOAD_IDENTITY edge was silently lost.
const azureNodeTypeCloudResource = "cloud-resource"

// declaredForeignContext is the foreign-graph context this collector declares it
// needs, keyed by graph family in the shape the client's config loader decodes.
//
// IT IS A DECLARATION, NOT A REQUEST FOR ACCESS. The collector never reads
// another graph; the client fills exactly what is declared here into the
// collect input, and anything undeclared is never supplied. That is the same
// contract the env block follows, for the same reason: a child receives what it
// declared and nothing else.
//
// THE TWO ENTRIES ARE EXACTLY THE INPUTS THE FOUR LINKAGE SHAPES NEED, and no
// more — see linkage.go. Two of the four shapes need nothing at all.
//
// THE FAMILY IS THE MAP KEY AND THE REASON NEVER RENDERS. Both were struct
// fields carrying `graph` and `reason` json tags, and the entry rendered as an
// ARRAY of them; the client decodes `context` into a map and refuses an unknown
// field by name, so the generated example was refused at load — and a refusal
// fails the operator's whole config file rather than the one entry. The reason
// stays on the value for a reviewer and for the census, tagged `json:"-"`.
func declaredForeignContext() framework.ForeignContextDeclaration {
	return framework.ForeignContextDeclaration{
		framework.FamilyCode: {
			NodeTypes: []string{"file"},
			// The file node's ID AND PATH AND BODY, not its name: the DEPLOYS edge
			// runs FROM the chart file node and the chart name is parsed out of the
			// body, so a declaration carrying names alone could not produce the
			// edge's source endpoint at all.
			NodeFields:    []string{"id", "file_path", "content"},
			PathBasenames: []string{"Chart.yaml", "Chart.yml"},
			Reason: "DEPLOYS: the chart name is parsed out of the file BODY and the edge " +
				"is emitted FROM the file node's id, so the name alone is not enough.",
		},
		familyAzure: {
			NodeTypes:    []string{azureNodeTypeCloudResource},
			NodeFields:   []string{"id"},
			MetadataKeys: []string{"client_id"},
			Reason: "WORKLOAD_IDENTITY (Azure only): an azure.workload.identity/client-id " +
				"annotation is a bare UUID that names nothing on its own. It names the " +
				"AZURE family specifically — the shape it resolves against is a managed " +
				"identity — rather than a cloud-wide family, which no longer exists: the " +
				"client supplies registered graph types, and the azure collector is the one " +
				"that produces managed identities. The IRSA and GCP shapes need no foreign " +
				"context and none is declared for them.",
		},
	}
}

// configEntry is the operator-facing config entry, in the shape the collector
// config file takes.
type configEntry struct {
	Type           string                              `json:"type"`
	Command        string                              `json:"command"`
	Args           []string                            `json:"args,omitempty"`
	Tool           string                              `json:"tool"`
	Env            map[string]string                   `json:"env"`
	Behavior       behavior                            `json:"behavior"`
	ForeignContext framework.ForeignContextDeclaration `json:"context,omitempty"`
}

// ExampleEntry renders the config entry for this collector on the given target
// OS, as the operator would write it.
//
// command is the path the daemon spawns. It is a parameter because an operator
// installs the binary wherever they like, and an example that hard-coded one
// path would be wrong for everyone else.
func ExampleEntry(goos, command string) configEntry {
	env := map[string]string{}
	for _, name := range envAllowlist(goos) {
		// The ${VAR:-} form: the operator's own environment supplies the value,
		// and a name they have not set ARRIVES PRESENT AND EMPTY — the reference
		// resolves to the empty string in the process serving the collect, and
		// the child receives the name with an empty value rather than not at all.
		//
		// THAT IS SAFE HERE BECAUSE THIS COLLECTOR DISCRIMINATES ON NOTHING, and
		// it is measured rather than assumed: empty_sensitive_test.go drives every
		// declared name in both states through this module's own resolution, with
		// a same-run non-empty control on each arm, and the declaration in
		// describe_env.go marks no name. The comment here used to say the name
		// arrives ABSENT, which is not what the loader does; the conclusion — that
		// the defaulted form is right for this entry — survives, on the ground
		// that nothing this collector reads can tell the two states apart.
		//
		// A NAME THIS COLLECTOR STARTS DISCRIMINATING ON reds that test, and the
		// mark it then needs makes the installer's documentation gate refuse this
		// very entry. So the two move together rather than drifting.
		env[name] = "${" + name + ":-}"
	}
	return configEntry{
		Type:           "stdio",
		Command:        command,
		Tool:           "collect",
		Env:            env,
		Behavior:       declaredBehavior(),
		ForeignContext: declaredForeignContext(),
	}
}

// ExampleEntryJSON renders [ExampleEntry] as the operator would paste it: keyed
// by this collector's registration name, UNDER the config file's own
// `collectors` object.
//
// THE WRAPPER IS NOT DECORATION AND ITS ABSENCE WAS A DEFECT. The collector
// config file is `{"collectors": {"<name>": {...}}}` and the client's loader
// decodes it with DisallowUnknownFields, so a document whose top-level key is
// the collector's name is refused with `unknown field "k8s"` — and a refusal
// fails the WHOLE file, so an operator pasting this example lost every other
// collector they had registered. This function rendered the unwrapped shape and
// the README shipped it; nothing in this module could see it, because the loader
// lives in the client module. The client's README-loads test is what found it.
func ExampleEntryJSON(goos, command string) (string, error) {
	doc := map[string]any{"collectors": map[string]any{registrationName: ExampleEntry(goos, command)}}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("rendering the example config entry: %w", err)
	}
	return string(b), nil
}

// declaredEnvNames is the sorted env-block key set of the example entry, which
// is what a test compares against the allowlist.
func declaredEnvNames(entry configEntry) []string {
	out := make([]string, 0, len(entry.Env))
	for name := range entry.Env {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
