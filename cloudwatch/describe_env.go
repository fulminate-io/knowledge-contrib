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
	"AWS_ACCESS_KEY":                           framework.EnvClassNotCarried,
	"AWS_ACCESS_KEY_ID":                        framework.EnvClassSecret,
	"AWS_ACCOUNT_ID":                           framework.EnvClassNotCarried,
	"AWS_ACCOUNT_ID_ENDPOINT_MODE":             framework.EnvClassNotCarried,
	"AWS_AUTH_SCHEME_PREFERENCE":               framework.EnvClassNotCarried,
	"AWS_CA_BUNDLE":                            framework.EnvClassNotCarried,
	"AWS_CONFIG_FILE":                          framework.EnvClassPath,
	"AWS_CONTAINER_AUTHORIZATION_TOKEN":        framework.EnvClassNotCarried,
	"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE":   framework.EnvClassNotCarried,
	"AWS_CONTAINER_CREDENTIALS_FULL_URI":       framework.EnvClassNotCarried,
	"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI":   framework.EnvClassNotCarried,
	"AWS_DEFAULTS_MODE":                        framework.EnvClassNotCarried,
	"AWS_DEFAULT_PROFILE":                      framework.EnvClassNotCarried,
	"AWS_DEFAULT_REGION":                       framework.EnvClassNotCarried,
	"AWS_DISABLE_REQUEST_COMPRESSION":          framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_DISABLED":                framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_SERVICE_ENDPOINT":        framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE":   framework.EnvClassNotCarried,
	"AWS_EC2_METADATA_V1_DISABLED":             framework.EnvClassNotCarried,
	"AWS_ENABLE_ENDPOINT_DISCOVERY":            framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL":                         framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_CLOUDWATCH_LOGS":         framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SIGNIN":                  framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SSO":                     framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_SSO_OIDC":                framework.EnvClassNotCarried,
	"AWS_ENDPOINT_URL_STS":                     framework.EnvClassNotCarried,
	"AWS_EXECUTION_ENV":                        framework.EnvClassNotCarried,
	"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS":      framework.EnvClassNotCarried,
	"AWS_LOGIN_CACHE_DIRECTORY":                framework.EnvClassNotCarried,
	"AWS_MAX_ATTEMPTS":                         framework.EnvClassNotCarried,
	"AWS_PROFILE":                              framework.EnvClassSelector,
	"AWS_REGION":                               framework.EnvClassSelector,
	"AWS_REQUEST_CHECKSUM_CALCULATION":         framework.EnvClassNotCarried,
	"AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES":   framework.EnvClassNotCarried,
	"AWS_RESPONSE_CHECKSUM_VALIDATION":         framework.EnvClassNotCarried,
	"AWS_RETRY_MODE":                           framework.EnvClassNotCarried,
	"AWS_ROLE_ARN":                             framework.EnvClassNotCarried,
	"AWS_ROLE_SESSION_NAME":                    framework.EnvClassNotCarried,
	"AWS_S3_DISABLE_EXPRESS_SESSION_AUTH":      framework.EnvClassNotCarried,
	"AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS": framework.EnvClassNotCarried,
	"AWS_S3_USE_ARN_REGION":                    framework.EnvClassNotCarried,
	"AWS_SDK_UA_APP_ID":                        framework.EnvClassNotCarried,
	"AWS_SECRET_ACCESS_KEY":                    framework.EnvClassSecret,
	"AWS_SECRET_KEY":                           framework.EnvClassNotCarried,
	"AWS_SESSION_TOKEN":                        framework.EnvClassSecret,
	"AWS_SHARED_CREDENTIALS_FILE":              framework.EnvClassPath,
	"AWS_USE_DUALSTACK_ENDPOINT":               framework.EnvClassNotCarried,
	"AWS_USE_FIPS_ENDPOINT":                    framework.EnvClassNotCarried,
	"AWS_WEB_IDENTITY_TOKEN_FILE":              framework.EnvClassNotCarried,
	"HOME":                                     framework.EnvClassPath,
	"USERPROFILE":                              framework.EnvClassNotCarried,
}

// NO NAME HERE IS MARKED EMPTY-SENSITIVE, and that is a MEASURED statement rather
// than an omission.
//
// The declaration carries a per-name `empty_sensitive` mark for a collector that
// tells a name PRESENT AND EMPTY apart from ABSENT; the installer's documentation
// gate refuses the defaulted `${NAME:-}` form for a marked name, because that
// reference resolves to the empty string in the process serving the collect and
// reaches the child present and empty. Every name this collector declares was
// driven through the AWS configuration package's own resolution in both states
// and produced a byte-identical result, with a same-run non-empty control on each
// step of the credential chain — the environment provider, the shared-credentials
// file, web identity, the container endpoint and the shared-config profile — plus
// HOME through os.UserHomeDir. So the defaulted form this collector's worked entry
// renders for every declared name is safe, and empty_sensitive_test.go is the run
// that keeps that true: it fails if any declared name starts discriminating, and
// it fails if a mark appears here without one.

// describedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition. It sets no empty-sensitive mark,
// for the reason stated above.
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
