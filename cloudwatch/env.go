// SPDX-License-Identifier: Apache-2.0

package main

import "sort"

// env.go — THE ENVIRONMENT VARIABLES THIS COLLECTOR'S CONFIGURATION ENTRY MUST
// CARRY, as a value computed from a target operating system.
//
// WHY THIS EXISTS AS CODE AT ALL. A collector process receives exactly the
// environment its configuration entry declares — a name the entry does not
// carry is ABSENT in the child, whatever the daemon's own environment holds.
// So the set below is not documentation of what AWS happens to read: it is the
// list an operator must write into the entry, and a name missing from it is a
// deployment that fails in a way no test of this collector can see.
//
// WHY A TARGET-OS PARAMETER AND NOT runtime.GOOS. The list differs on Windows
// by one name, and no test runner in this repository runs on Windows, so a
// runtime.GOOS branch would be dead code on every machine that could exercise
// it. A function taking the operating system as DATA is exercised on the
// runners that exist.
//
// HOW THE SET WAS DERIVED, and why a search for quoted AWS_ literals could not
// have produced it: the credential chain reads its per-service endpoint
// overrides through a name it COMPOSES at runtime from a service id, so four of
// the names below appear as no literal anywhere in the configuration package.
// They come from the modules the chain builds clients for — the token service,
// the two single-sign-on modules and the sign-in module — plus this collector's
// own service. Refreshing this list means re-running a CALL-SITE census over
// this module's dependency closure when a pin moves, never grepping for
// literals.

// TargetOS names the operating system a configuration entry is being written
// for.
type TargetOS string

// The target operating systems the entry differs between. Every non-Windows
// system takes the same list, so they share one value.
const (
	TargetWindows TargetOS = "windows"
	TargetPOSIX   TargetOS = "posix"
)

// credentialChainEnv are the names the AWS configuration package reads while
// resolving a credential and a region.
var credentialChainEnv = []string{
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SESSION_TOKEN",
	"AWS_ACCESS_KEY",
	"AWS_SECRET_KEY",
	"AWS_PROFILE",
	"AWS_DEFAULT_PROFILE",
	"AWS_REGION",
	"AWS_DEFAULT_REGION",
	"AWS_CONFIG_FILE",
	"AWS_SHARED_CREDENTIALS_FILE",
	"AWS_ROLE_ARN",
	"AWS_ROLE_SESSION_NAME",
	"AWS_WEB_IDENTITY_TOKEN_FILE",
	"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
	"AWS_CONTAINER_CREDENTIALS_FULL_URI",
	"AWS_CONTAINER_AUTHORIZATION_TOKEN",
	"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
	"AWS_EC2_METADATA_DISABLED",
	"AWS_EC2_METADATA_SERVICE_ENDPOINT",
	"AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE",
	"AWS_EC2_METADATA_V1_DISABLED",
	"AWS_LOGIN_CACHE_DIRECTORY",
	"AWS_CA_BUNDLE",
}

// behaviorTuningEnv are the names that tune how requests are made rather than
// who makes them.
//
// THEY ARE ON THE LIST DELIBERATELY. They look optional, and leaving them off
// would break exactly the deployments that cannot work around it — a private
// endpoint, a FIPS region, a proxy that rewrites requests — and it would break
// them SILENTLY, because the collector would simply talk to the public endpoint
// and succeed.
var behaviorTuningEnv = []string{
	"AWS_ACCOUNT_ID",
	"AWS_ACCOUNT_ID_ENDPOINT_MODE",
	"AWS_AUTH_SCHEME_PREFERENCE",
	"AWS_DEFAULTS_MODE",
	"AWS_DISABLE_REQUEST_COMPRESSION",
	"AWS_ENABLE_ENDPOINT_DISCOVERY",
	"AWS_ENDPOINT_URL",
	"AWS_EXECUTION_ENV",
	"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS",
	"AWS_MAX_ATTEMPTS",
	"AWS_REQUEST_CHECKSUM_CALCULATION",
	"AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES",
	"AWS_RESPONSE_CHECKSUM_VALIDATION",
	"AWS_RETRY_MODE",
	"AWS_S3_DISABLE_EXPRESS_SESSION_AUTH",
	"AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS",
	"AWS_S3_USE_ARN_REGION",
	"AWS_SDK_UA_APP_ID",
	"AWS_USE_DUALSTACK_ENDPOINT",
	"AWS_USE_FIPS_ENDPOINT",
}

// perServiceEndpointEnv are the composed per-service endpoint overrides. Four
// of the five belong to modules the CREDENTIAL CHAIN builds clients for, not to
// the logs API: an operator on a private endpoint has to redirect the leg that
// gets the credential as well as the leg that reads the logs.
var perServiceEndpointEnv = []string{
	"AWS_ENDPOINT_URL_CLOUDWATCH_LOGS",
	"AWS_ENDPOINT_URL_STS",
	"AWS_ENDPOINT_URL_SSO",
	"AWS_ENDPOINT_URL_SSO_OIDC",
	"AWS_ENDPOINT_URL_SIGNIN",
}

// homeEnv is the home-directory variable per target operating system.
//
// IT IS NOT AN AWS NAME AND IT IS LOAD-BEARING. The shared credentials and
// config files are found by expanding the home directory, so without it no
// profile, no SSO session and no assumed role is reachable AT ALL, however many
// AWS names are declared.
//
// IT IS A SWAP, NOT AN ADDITION, and the distinction decides the declared list's
// SIZE: the Windows arm exchanges HOME for USERPROFILE rather than declaring
// both, so the two target lists carry the same number of names. Declaring HOME
// on Windows would put a name in the child's environment that the AWS chain does
// not read there and that therefore decides nothing, and a declared name that
// decides nothing is a claim this collector would be making falsely about its
// own requirements.
var homeEnv = map[TargetOS]string{
	TargetPOSIX:   "HOME",
	TargetWindows: "USERPROFILE",
}

// DeclaredEnv returns every environment variable name this collector's
// configuration entry must carry for the given target operating system, sorted.
//
// WHAT IT DELIBERATELY EXCLUDES, with the cost stated rather than hidden: the
// two trust-store names and the six proxy spellings that Go's own HTTP
// transport and certificate verification read. A collector is responsible for
// its own environment and the daemon passes nothing, so on a host whose
// certificate bundle is not at a compiled-in default path — a container image,
// a workplace that intercepts TLS — an operator who has not listed those names
// cannot complete the connection, and one behind a proxy never reaches the
// endpoint. Both surface as a TRANSPORT error rather than a credential one, so
// the loud-failure arm in the walk is what keeps that cost visible. Adding any
// of them later is an edit to the configuration entry, never a code change.
//
// ONE FURTHER ROUTE IT DOES NOT REACH, named rather than discovered later: no
// PATH is declared, so a shared-config profile using a credential process with
// a bare command name cannot resolve that command. It fails loudly at the
// chain, which is what the credential requirement asks; an absolute path still
// works.
func DeclaredEnv(target TargetOS) []string {
	out := make([]string, 0, len(credentialChainEnv)+len(behaviorTuningEnv)+len(perServiceEndpointEnv)+1)
	out = append(out, credentialChainEnv...)
	out = append(out, behaviorTuningEnv...)
	out = append(out, perServiceEndpointEnv...)
	out = append(out, homeEnv[targetOrPOSIX(target)])
	sort.Strings(out)
	return out
}

// targetOrPOSIX maps any target that is not Windows onto the POSIX list, which
// is what every other operating system this collector runs on shares.
func targetOrPOSIX(target TargetOS) TargetOS {
	if target == TargetWindows {
		return TargetWindows
	}
	return TargetPOSIX
}
