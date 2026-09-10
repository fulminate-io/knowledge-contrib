// SPDX-License-Identifier: Apache-2.0

package main

import (
	"slices"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe_env.go — WHAT AN INSTALLER MUST DO WITH EVERY ENVIRONMENT NAME THIS
// COLLECTOR READS, and the describe tool's environment declaration built from
// it.
//
// THE DECISION LIVES HERE, BESIDE THE COLLECTOR THAT READS THE NAME. It used to
// live in the installer as a hand-written per-collector table and in a
// disposition file beside it; both are now DERIVED from this map, so a variable
// this collector starts reading is one decision in one place rather than three
// edits in three repositories' worth of files. The disposition file remains the
// frozen review record, and the environment census pins this map against it.
//
// THE FOUR DISPOSITIONS ARE THE CONTRACT'S OWN, and each says what an installed
// entry carries: a path literal, a selector literal when set, a secret in NO
// state whatsoever, and not-carried for a name this collector reads that an
// installed entry deliberately does not declare. Declaring the last kind is the
// point: an operator whose variable is absent can tell a decision from an
// oversight.
// envDispositions is that decision, one row per name.
var envDispositions = map[string]string{
	"CLOUDSDK_CONFIG":                 framework.EnvClassPath,
	"DISABLE_HTTP2":                   framework.EnvClassNotCarried,
	"HOME":                            framework.EnvClassPath,
	"HOMEDRIVE":                       framework.EnvClassNotCarried,
	"HOMEPATH":                        framework.EnvClassNotCarried,
	"HTTP2_PING_TIMEOUT_SECONDS":      framework.EnvClassNotCarried,
	"HTTP2_READ_IDLE_TIMEOUT_SECONDS": framework.EnvClassNotCarried,
	"HTTPS_PROXY":                     framework.EnvClassNotCarried,
	"HTTP_PROXY":                      framework.EnvClassNotCarried,
	"KUBECONFIG":                      framework.EnvClassPath,
	"KUBERNETES_MASTER":               framework.EnvClassNotCarried,
	"KUBERNETES_SERVICE_HOST":         framework.EnvClassNotCarried,
	"KUBERNETES_SERVICE_PORT":         framework.EnvClassNotCarried,
	"NO_PROXY":                        framework.EnvClassNotCarried,
	"PATH":                            framework.EnvClassPath,
	"POD_NAMESPACE":                   framework.EnvClassNotCarried,
	"SSL_CERT_DIR":                    framework.EnvClassNotCarried,
	"SSL_CERT_FILE":                   framework.EnvClassNotCarried,
	"USERPROFILE":                     framework.EnvClassNotCarried,
	"http_proxy":                      framework.EnvClassNotCarried,
	"https_proxy":                     framework.EnvClassNotCarried,
	"no_proxy":                        framework.EnvClassNotCarried,
}

// NO NAME HERE IS MARKED EMPTY-SENSITIVE, and that is a MEASURED statement rather
// than an omission — but the names do not all rest on the same ground, and
// empty_sensitive_test.go says which is which rather than reporting one number.
//
// The declaration carries a per-name `empty_sensitive` mark for a collector that
// tells a name PRESENT AND EMPTY apart from ABSENT; the installer's documentation
// gate refuses the defaulted `${NAME:-}` form for a marked name, because that
// reference resolves to the empty string in the process serving the collect and
// reaches the child present and empty. Three grounds appear here:
//
//   - MOST NAMES are inert because a reader on this collector's path treats empty
//     as absent. They are driven through resolveCredential and the proxy resolver
//     in both states, with a same-run non-empty control per arm.
//   - KUBERNETES_MASTER is inert STRUCTURALLY: nothing on this path reads it in
//     any state. client-go reads it once at package initialisation into a
//     package-level default that reaches a client only through a caller's own
//     ConfigOverrides, and this collector passes a fresh one. No control that
//     moved the result is available for it, and one that did would mean the module
//     had started reading it. Its row uses a re-exec child, because the read
//     happens before any test body runs.
//   - SEVEN NAMES have no live control on the hosts this suite runs on, and their
//     row states exactly what the source establishes and no more: the three http2
//     knobs ARE read, under a `len(s) > 0` guard that makes empty and absent the
//     same branch by construction; the two trust roots are read on linux and not
//     on darwin, so their behavior is established on neither target; and
//     CLOUDSDK_CONFIG and PATH reach a gcloud credential plugin no arm spawns.
//
// Unmarked is what those reads support. A mark here would be a claim no run on
// this host can carry, which is why the test refuses one for those seven by name.

// describedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition. It sets no empty-sensitive mark,
// for the reasons stated above.
//
// SORTED so one unchanged collector renders one byte-identical declaration, and
// so the installer table generated from it does not reorder itself between runs.
func describedEnvironment() []framework.EnvDeclaration {
	names := make([]string, 0, len(envDispositions))
	for name := range envDispositions {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{Name: name, Class: envDispositions[name]})
	}
	return out
}
