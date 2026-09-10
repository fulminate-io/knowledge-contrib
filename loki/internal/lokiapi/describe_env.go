// SPDX-License-Identifier: Apache-2.0

package lokiapi

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
// EnvDispositions is that decision, one row per name.
var EnvDispositions = map[string]string{
	"HTTPS_PROXY":             framework.EnvClassSelector,
	"HTTP_PROXY":              framework.EnvClassSelector,
	"LOKI_AUTH_HEADER":        framework.EnvClassSecret,
	"LOKI_BEARER_TOKEN":       framework.EnvClassSecret,
	"LOKI_BEARER_TOKEN_FILE":  framework.EnvClassSelector,
	"LOKI_CA_CERT_PATH":       framework.EnvClassSelector,
	"LOKI_CLIENT_CERT_PATH":   framework.EnvClassSelector,
	"LOKI_CLIENT_KEY_PATH":    framework.EnvClassSelector,
	"LOKI_CLIENT_MAX_BACKOFF": framework.EnvClassSelector,
	"LOKI_CLIENT_MIN_BACKOFF": framework.EnvClassSelector,
	"LOKI_CLIENT_RETRIES":     framework.EnvClassSelector,
	"LOKI_ENV_PROXY":          framework.EnvClassSelector,
	"LOKI_HTTP_COMPRESSION":   framework.EnvClassSelector,
	"LOKI_HTTP_PROXY_URL":     framework.EnvClassSelector,
	"LOKI_NO_CACHE":           framework.EnvClassSelector,
	"LOKI_ORG_ID":             framework.EnvClassSelector,
	"LOKI_PASSWORD":           framework.EnvClassSecret,
	"LOKI_QUERY_TAGS":         framework.EnvClassSelector,
	"LOKI_TLS_SKIP_VERIFY":    framework.EnvClassSelector,
	"LOKI_USERNAME":           framework.EnvClassSecret,
	"NO_PROXY":                framework.EnvClassSelector,
	"SSL_CERT_DIR":            framework.EnvClassSelector,
	"SSL_CERT_FILE":           framework.EnvClassSelector,
	"http_proxy":              framework.EnvClassSelector,
	"https_proxy":             framework.EnvClassSelector,
	"no_proxy":                framework.EnvClassSelector,
}

// EmptySensitiveNames are the names this collector tells PRESENT AND EMPTY apart
// from ABSENT, and it is exactly the set `LoadSettings` refuses by name.
//
// WHY THE SET IS DECLARED AND NOT INFERRED. A worked entry in a README that
// renders `${NAME:-}` hands the child that name present and empty: the reference
// is expanded by the process serving the collect, whose environment holds none
// of these, so it resolves to the empty string. For every name below that is a
// collect that fails on its first line, naming the variable. The installer's
// documentation gate refuses the defaulted form for these names and admits it for
// the rest, and this declaration is where it reads the answer — so a name that
// starts or stops discriminating is one edit here rather than a list in a script
// nobody updates.
//
// THE EIGHT NAMES NOT LISTED ARE THE SIX PROXY DELEGATES AND THE TWO TRUST ROOTS,
// and their absence is a measured statement rather than an omission: this module
// never reads them. They are declared because the entry's env block is the
// child's WHOLE environment and net/http and crypto/x509 read them from it
// directly, so their inertness through this collector's own resolution is
// structural. empty_sensitive_test.go drives every declared name and fails if
// this set and the measured one disagree in either direction.
var EmptySensitiveNames = map[string]bool{
	"LOKI_AUTH_HEADER":        true,
	"LOKI_BEARER_TOKEN":       true,
	"LOKI_BEARER_TOKEN_FILE":  true,
	"LOKI_CA_CERT_PATH":       true,
	"LOKI_CLIENT_CERT_PATH":   true,
	"LOKI_CLIENT_KEY_PATH":    true,
	"LOKI_CLIENT_MAX_BACKOFF": true,
	"LOKI_CLIENT_MIN_BACKOFF": true,
	"LOKI_CLIENT_RETRIES":     true,
	"LOKI_ENV_PROXY":          true,
	"LOKI_HTTP_COMPRESSION":   true,
	"LOKI_HTTP_PROXY_URL":     true,
	"LOKI_NO_CACHE":           true,
	"LOKI_ORG_ID":             true,
	"LOKI_PASSWORD":           true,
	"LOKI_QUERY_TAGS":         true,
	"LOKI_TLS_SKIP_VERIFY":    true,
	"LOKI_USERNAME":           true,
}

// DescribedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition and its empty-sensitivity.
//
// SORTED so one unchanged collector renders one byte-identical declaration, and
// so the installer table generated from it does not reorder itself between runs.
func DescribedEnvironment() []framework.EnvDeclaration {
	names := make([]string, 0, len(EnvDispositions))
	for name := range EnvDispositions {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{
			Name:           name,
			Class:          EnvDispositions[name],
			EmptySensitive: EmptySensitiveNames[name],
		})
	}
	return out
}
