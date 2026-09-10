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
	"APPDATA":                         framework.EnvClassNotCarried,
	"DEBUG_HTTP2_GOROUTINES":          framework.EnvClassNotCarried,
	"EXPERIMENTAL_GOOGLE_API_USE_S2A": framework.EnvClassNotCarried,
	"GCE_METADATA_HOST":               framework.EnvClassSelector,
	"GODEBUG":                         framework.EnvClassNotCarried,
	"GOOGLE_API_CERTIFICATE_CONFIG":   framework.EnvClassNotCarried,
	"GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB": framework.EnvClassNotCarried,
	"GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB":  framework.EnvClassNotCarried,
	"GOOGLE_API_USE_CLIENT_CERTIFICATE":               framework.EnvClassNotCarried,
	"GOOGLE_API_USE_MTLS":                             framework.EnvClassNotCarried,
	"GOOGLE_API_USE_MTLS_ENDPOINT":                    framework.EnvClassNotCarried,
	"GOOGLE_APPLICATION_CREDENTIALS":                  framework.EnvClassSelector,
	"GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED":              framework.EnvClassNotCarried,
	"GOOGLE_CLOUD_PROJECT":                            framework.EnvClassSelector,
	"GOOGLE_CLOUD_QUOTA_PROJECT":                      framework.EnvClassSelector,
	"GOOGLE_CLOUD_UNIVERSE_DOMAIN":                    framework.EnvClassNotCarried,
	"GOPROTODEBUG":                                    framework.EnvClassNotCarried,
	"GRPC_GO_LOG_FORMATTER":                           framework.EnvClassNotCarried,
	"GRPC_GO_LOG_SEVERITY_LEVEL":                      framework.EnvClassNotCarried,
	"GRPC_GO_LOG_VERBOSITY_LEVEL":                     framework.EnvClassNotCarried,
	"GRPC_XDS_BOOTSTRAP":                              framework.EnvClassNotCarried,
	"GRPC_XDS_BOOTSTRAP_CONFIG":                       framework.EnvClassNotCarried,
	"HOME":                                            framework.EnvClassPath,
	"HTTPS_PROXY":                                     framework.EnvClassSelector,
	"HTTP_PROXY":                                      framework.EnvClassSelector,
	"NO_PROXY":                                        framework.EnvClassSelector,
	"S2A_ACCESS_TOKEN":                                framework.EnvClassNotCarried,
	"S2A_TIMEOUT":                                     framework.EnvClassNotCarried,
	"SSL_CERT_DIR":                                    framework.EnvClassSelector,
	"SSL_CERT_FILE":                                   framework.EnvClassSelector,
	"http_proxy":                                      framework.EnvClassSelector,
	"https_proxy":                                     framework.EnvClassSelector,
	"no_proxy":                                        framework.EnvClassSelector,
}

// describedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition.
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
