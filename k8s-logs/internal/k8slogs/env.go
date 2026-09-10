// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// env.go — the environment variables this collector's config entry must list,
// per target operating system, and the example entry that lists them.
//
// THE ENTRY'S env BLOCK IS THIS PROCESS'S WHOLE ENVIRONMENT. The daemon that
// spawns a stdio collector copies nothing from its own environment and adds
// nothing: a variable the entry does not name is ABSENT from this process,
// whatever the daemon holds. So this list is not advice, it is the complete set
// of levers the collector has, and a name left off it is a capability the
// operator does not have rather than a default they inherit.
//
// THE TABLE IS A FUNCTION OF A TARGET-OS PARAMETER, NOT OF runtime.GOOS, and
// that is deliberate. The Windows home-directory fallbacks are real — client-go
// reads them — but no test venue in this repository runs on Windows, so a
// runtime.GOOS branch would be a branch that passes everywhere by never
// executing its subject. Taking the target as an argument makes all three
// tables assertable on one Linux runner.
//
// WHAT IS DELIBERATELY LEFT OFF, each with the cost of leaving it off, because
// an absence with no reason reads as an oversight:
//
//   - THE PROXY FAMILY (HTTP_PROXY and its five siblings). Whether and through
//     what this collector reaches an apiserver is the operator's explicit
//     choice in the entry, so a collect and the operator's own kubectl cannot
//     silently disagree about what is reachable. THE COST: an operator behind a
//     corporate proxy who has not listed them gets a connection error rather
//     than an explanation.
//   - THE TRUST ROOTS (SSL_CERT_FILE, SSL_CERT_DIR). Same footing, same reason.
//     THE COST: a host whose CA bundle is not at a compiled-in default — a
//     container image, a corporate trust store — fails as a transport error
//     rather than a credential one.
//   - KUBERNETES_MASTER and POD_NAMESPACE. Each silently overrides a parameter
//     this tool owns: the first retargets the apiserver, the second changes the
//     default namespace. A variable that overrides a tool parameter is exactly
//     the class this block exists to close.
//   - THE CLIENT FEATURE GATES (KUBE_FEATURE_*). A gate flipped from outside
//     the tool's parameters changes behavior with nothing in the result saying
//     so. THE COST: enabling one for this collector means editing its entry.
//   - THE TUNING AND DEBUG NAMES (backoff, HTTP/2 timeouts, the several GODEBUG
//     spellings). Each defaults sanely and none is a lever this collector's
//     requirements need.

// The target operating systems the entry table is defined for.
const (
	OSLinux   = "linux"
	OSDarwin  = "darwin"
	OSWindows = "windows"
)

// The environment names, spelled once.
const (
	EnvKubeconfig            = "KUBECONFIG"
	EnvHome                  = "HOME"
	EnvPath                  = "PATH"
	EnvHomeDrive             = "HOMEDRIVE"
	EnvHomePath              = "HOMEPATH"
	EnvUserProfile           = "USERPROFILE"
	EnvSystemRoot            = "SYSTEMROOT"
	EnvKubernetesServiceHost = "KUBERNETES_SERVICE_HOST"
	EnvKubernetesServicePort = "KUBERNETES_SERVICE_PORT"
)

// EnvNames returns the environment names a config entry must list for a target
// operating system, sorted.
//
// The three every target needs: KUBECONFIG names the kubeconfig; HOME is what
// resolves the kubeconfig when KUBECONFIG is unset AND what decides where the
// cloud auth plugin looks for its own configuration; PATH is load-bearing even
// when the kubeconfig names its auth plugin by absolute path, because the
// plugin itself execs another binary and the spawn resolves through PATH.
//
// Windows adds the home fallbacks client-go consults there, and SYSTEMROOT,
// which is how a child process is resolved on that platform at all — naming
// HOMEDRIVE alone would imply the Windows story is only about the home
// directory.
//
// inCluster adds the two variables that select the in-cluster credential path.
// The token and CA it then reads are FILE paths compiled into client-go as
// function-local constants, so no entry can point them anywhere: listing these
// two is the whole of what an entry controls about that arm.
func EnvNames(targetOS string, inCluster bool) ([]string, error) {
	names := []string{EnvKubeconfig, EnvHome, EnvPath}
	switch targetOS {
	case OSLinux, OSDarwin:
	case OSWindows:
		names = append(names, EnvHomeDrive, EnvHomePath, EnvUserProfile, EnvSystemRoot)
	default:
		return nil, fmt.Errorf(
			"k8s-logs: %q is not a target this collector's environment table is defined for; it is defined for %s, %s and %s",
			targetOS, OSDarwin, OSLinux, OSWindows)
	}
	if inCluster {
		names = append(names, EnvKubernetesServiceHost, EnvKubernetesServicePort)
	}
	sort.Strings(names)
	return names, nil
}

// exampleEnvValue is the value the documented entry shows for one name: a
// literal for every name this collector reads, and a DEFAULTED reference for
// anything a later change adds without deciding on an example.
//
// The fallback carries `:-` deliberately. A bare ${VAR} is the shape that refuses
// the whole scoped file when it cannot be resolved, so even the value nobody
// chose is a value that cannot take an operator's other collectors down with it.
func exampleEnvValue(name string) string {
	switch name {
	case EnvHome:
		return "/home/you"
	case EnvKubeconfig:
		return "/home/you/.kube/config"
	case EnvPath:
		return "/usr/local/bin:/usr/bin:/bin"
	case EnvUserProfile:
		return `C:\Users\you`
	case EnvHomeDrive:
		return "C:"
	case EnvHomePath:
		return `\Users\you`
	case EnvSystemRoot:
		return `C:\Windows`
	// THE IN-CLUSTER PAIR IS A LITERAL FOR A SECOND REASON: this collector
	// branches on the PRESENCE of the service host, so a name arriving present
	// and empty tells it that it is running inside a cluster when it is not.
	case EnvKubernetesServiceHost:
		return "10.96.0.1"
	case EnvKubernetesServicePort:
		return "443"
	}
	return "${" + name + ":-}"
}

// ExampleEntry renders the config-file entry an operator installs this
// collector with, as pretty-printed JSON under its family name.
//
// EVERY VALUE IS A LITERAL AN OPERATOR EDITS, and it used to be a ${VAR}
// reference. The property that spelling protected is worth keeping and is not
// lost: the config file may live at a repository root, so a CREDENTIAL must
// never be a literal in the example. None of the names this collector reads
// carries one — they are paths and cluster coordinates — so nothing here needs
// the reference, and it cost more than it bought.
//
// WHAT IT COST. A scoped config file is expanded when it is READ, and the reader
// returns on the FIRST failure, so one reference the serving process cannot
// resolve makes the whole file unreadable and every collector registered in that
// scope disappears at once. A service-managed daemon's own environment holds PATH
// and nothing else, which is where an operator's collect actually runs.
//
// AND HOME IS WORSE UNDER A DEFAULTED REFERENCE than under a bare one. ${HOME:-}
// resolves to the EMPTY string, and an empty home is the one value this collector
// documents as FAILING where an absent one succeeds: leaving HOME unset works,
// because the cloud auth plugin falls back to the password database, while
// pointing it at a home that holds no credentials does not. The intuition runs
// the wrong way, which is why it is written down and why the example shows a real
// path.
func ExampleEntry(targetOS string, inCluster bool, command string) (string, error) {
	names, err := EnvNames(targetOS, inCluster)
	if err != nil {
		return "", err
	}
	if command == "" {
		return "", fmt.Errorf("k8s-logs: an example config entry needs the collector's command")
	}
	env := make(map[string]string, len(names))
	for _, n := range names {
		env[n] = exampleEnvValue(n)
	}
	entry := map[string]any{
		"collectors": map[string]any{
			GraphFamily: map[string]any{
				"type":    "stdio",
				"command": command,
				"tool":    ToolName,
				"env":     env,
				"behavior": map[string]any{
					"syncable":         true,
					"summarizable":     true,
					"embeddable":       true,
					"bm25_fields":      GraphBM25Fields,
					"embed_fields":     GraphEmbedFields,
					"summarize_fields": GraphSummarizeFields,
				},
				"node_types": nodeTypeOverrides(),
				// THE FOREIGN-GRAPH CONTEXT THIS COLLECTOR DECLARES, carried into
				// the entry an operator installs rather than described beside it.
				// It is what makes the client fill a block at all: an entry with
				// no `context` key receives none, and every correlation this
				// collector confirms against cloud resources then resolves against
				// an empty read, with nothing that looks like a failure.
				"context": DeclaredForeignContext(),
			},
		},
	}
	out, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return "", fmt.Errorf("k8s-logs: rendering the example config entry: %w", err)
	}
	return string(out) + "\n", nil
}

// The graph-wide indexed field lists this collector declares.
//
// `content` IS ABSENT FROM ALL THREE, and that is the field choice that matters
// most. A chunk node's Content is the COMPRESSED payload carried as a string,
// so naming content graph-wide would embed, summarize and index compressed
// bytes for every chunk in the graph — the largest node type by far, and the
// one whose text is not text.
var (
	GraphBM25Fields      = []string{"symbol_name", "description", "summary"}
	GraphEmbedFields     = []string{"symbol_name", "description", "summary"}
	GraphSummarizeFields = []string{"symbol_name", "description"}
)

// nodeTypeOverrides narrows the cascade per node type. The chunk is the one
// that needs an override rather than an exclusion: it carries no readable text
// at all, so embedding it produces a vector of compression noise.
func nodeTypeOverrides() map[string]any {
	return map[string]any{
		"log-chunk": map[string]any{
			"embeddable":  false,
			"bm25_fields": []string{"symbol_name"},
		},
	}
}

// DescribeEnvNames renders the names for a target as a comma-separated list,
// for a diagnostic or a README table.
func DescribeEnvNames(targetOS string, inCluster bool) (string, error) {
	names, err := EnvNames(targetOS, inCluster)
	if err != nil {
		return "", err
	}
	return strings.Join(names, ", "), nil
}
