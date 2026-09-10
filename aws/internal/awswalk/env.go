// SPDX-License-Identifier: Apache-2.0

package awswalk

import "slices"

// env.go — THE ENVIRONMENT THIS COLLECTOR NEEDS, as a value computed from a
// target-OS parameter.
//
// WHY IT IS CODE AND NOT PROSE. A collector installed by a config entry gets that
// entry's `env` block as its WHOLE environment: the daemon copies nothing from
// its own and adds nothing. So a name this process reads and the entry does not
// list is simply absent, and the failure is an operator who set AWS_PROFILE and
// got "no credentials" with nothing to act on. The list below is what the install
// documentation is checked against and what a test compares against the SDK's
// ACTUAL read set, so neither can drift.
//
// IT TAKES THE TARGET OS AS A PARAMETER rather than reading runtime.GOOS. Every
// runner that runs `go test` in this repository is Linux, so a runtime.GOOS
// branch for Windows would be dead code on all of them — untestable, and
// therefore unverified in exactly the case it exists for. As a parameter both
// arms are ordinary table rows.
//
// FOUR GROUPS, AND EACH IS DERIVED DIFFERENTLY:
//
//  1. The pinned aws-sdk-go-v2/config package's whole AWS_* read set, censused
//     from that package's source at the version this module's go.sum pins.
//  2. The per-service endpoint overrides, one per service client in this module's
//     dependency CLOSURE. The closure is not the import list: `config` pulls in
//     sts, sso, ssooidc and signin to resolve the credential chain, and each
//     reads its own name. An operator pointing STS at a private endpoint is
//     authenticating through exactly the client an import-list census misses.
//  3. The home directory, which is not an AWS_ variable and is REQUIRED: without
//     it no profile, SSO session or shared-file assumed role is reachable,
//     however many AWS_* names the block carries.
//  4. The standard library's own reads, which no census of this module's closure
//     can ever surface because the standard library is outside every module
//     closure by construction.

// EnvAllowlist returns the environment variable names this collector's process
// needs, for a given target OS, sorted.
//
// goos takes Go's own spelling ("linux", "darwin", "windows"). An unknown value
// is treated as a unix-like OS, which is the shape every non-Windows target
// takes; the ONE thing that varies is the home-directory variable.
func EnvAllowlist(goos string) []string {
	names := make([]string, 0, len(configEnvNames)+len(serviceEndpointEnvNames)+len(stdlibEnvNames)+1)
	names = append(names, configEnvNames...)
	names = append(names, serviceEndpointEnvNames...)
	names = append(names, stdlibEnvNames...)
	names = append(names, homeEnvName(goos))
	slices.Sort(names)
	return slices.Compact(names)
}

// homeEnvName is the variable the Go standard library's os.UserHomeDir reads on
// a given target OS, which is what the AWS SDK resolves the shared config and
// credentials files through.
func homeEnvName(goos string) string {
	if goos == "windows" {
		return "USERPROFILE"
	}
	return "HOME"
}

// DocumentedEnvNames is what the module's README must name, and it is the same
// list EnvAllowlist returns for both target OSes taken together.
//
// THE README DOES NOT SHOW AN 87-LINE env BLOCK WITH EMPTY VALUES, and the
// reason is readability rather than a behavior difference in the SDK. Under the
// config-file contract a name PRESENT with an empty value is a different INPUT
// from a name absent, and for some collectors that difference is load-bearing —
// but not for this one, and AWS_PROFILE is the example that was stated backwards
// here. MEASURED: `AWS_PROFILE` set to the empty string and `AWS_PROFILE` absent
// BOTH leave the resolved profile empty, and an empty resolved profile is what
// the shared-config loader reads as `default`. So both states select the default
// profile; neither asks for a profile named "". A non-empty value selects that
// profile, which is the same-run control.
//
// The run is in the sibling module: cmd/collectors/cloudwatch's
// empty_sensitive_test.go drives every declared name in both states through the
// AWS configuration package's own resolution, with a per-chain-step control, and
// finds no name that tells them apart — which is also why the cloudwatch and k8s
// worked entries may keep the defaulted `${NAME:-}` form and why neither module's
// describe declaration marks a name empty-sensitive.
//
// So the documentation names every variable this collector can use and what it is
// for, and its worked entry sets only the handful an operator is actually
// setting — because an 87-line block of empty values is unreadable, not because
// the empty values would resolve something different.
func DocumentedEnvNames() []string {
	names := EnvAllowlist("linux")
	return append(names, "USERPROFILE")
}

// configEnvNames is the pinned aws-sdk-go-v2/config package's whole AWS_* read
// set: forty-four names at v1.32.14.
//
// ALL FORTY-FOUR ARE LISTED, not the credential-chain subset. Every one is a
// provider-namespaced SDK input, and the ones beyond the credential chain are the
// ones a non-default deployment needs: AWS_ENDPOINT_URL for a private endpoint,
// AWS_CA_BUNDLE for a TLS-intercepting proxy, AWS_USE_FIPS_ENDPOINT for a FIPS
// region. An operator who cannot set those has a collector that works only in the
// default configuration.
var configEnvNames = []string{
	"AWS_ACCESS_KEY",
	"AWS_ACCESS_KEY_ID",
	"AWS_ACCOUNT_ID",
	"AWS_ACCOUNT_ID_ENDPOINT_MODE",
	"AWS_AUTH_SCHEME_PREFERENCE",
	"AWS_CA_BUNDLE",
	"AWS_CONFIG_FILE",
	"AWS_CONTAINER_AUTHORIZATION_TOKEN",
	"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
	"AWS_CONTAINER_CREDENTIALS_FULL_URI",
	"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
	"AWS_DEFAULTS_MODE",
	"AWS_DEFAULT_PROFILE",
	"AWS_DEFAULT_REGION",
	"AWS_DISABLE_REQUEST_COMPRESSION",
	"AWS_EC2_METADATA_DISABLED",
	"AWS_EC2_METADATA_SERVICE_ENDPOINT",
	"AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE",
	"AWS_EC2_METADATA_V1_DISABLED",
	"AWS_ENABLE_ENDPOINT_DISCOVERY",
	"AWS_ENDPOINT_URL",
	"AWS_EXECUTION_ENV",
	"AWS_IGNORE_CONFIGURED_ENDPOINT_URLS",
	"AWS_LOGIN_CACHE_DIRECTORY",
	"AWS_MAX_ATTEMPTS",
	"AWS_PROFILE",
	"AWS_REGION",
	"AWS_REQUEST_CHECKSUM_CALCULATION",
	"AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES",
	"AWS_RESPONSE_CHECKSUM_VALIDATION",
	"AWS_RETRY_MODE",
	"AWS_ROLE_ARN",
	"AWS_ROLE_SESSION_NAME",
	"AWS_S3_DISABLE_EXPRESS_SESSION_AUTH",
	"AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS",
	"AWS_S3_USE_ARN_REGION",
	"AWS_SDK_UA_APP_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_SECRET_KEY",
	"AWS_SESSION_TOKEN",
	"AWS_SHARED_CREDENTIALS_FILE",
	"AWS_USE_DUALSTACK_ENDPOINT",
	"AWS_USE_FIPS_ENDPOINT",
	"AWS_WEB_IDENTITY_TOKEN_FILE",
}

// serviceEndpointEnvNames is one AWS_ENDPOINT_URL_<SERVICE> name per service
// client in this module's dependency closure.
//
// THE NAMES ARE NOT DERIVABLE FROM THE SERVICE NAME and must be read from each
// client's own endpoints.go: apigateway is API_GATEWAY while apigatewayv2 is
// APIGATEWAYV2, route53 is ROUTE_53, elasticloadbalancingv2 is
// ELASTIC_LOAD_BALANCING_V2, secretsmanager is SECRETS_MANAGER, ssooidc is
// SSO_OIDC. The SDK composes them at runtime from each client's SDK ID, upper-
// cased with spaces replaced by underscores, so a literal grep of the config
// package finds none of them and a hand-derived list gets several wrong.
//
// FOUR OF THEM ARE TRANSITIVE and are the reason this list is built from the
// closure rather than the import list: signin, sso, ssooidc and sts are pulled in
// by `config` to resolve the credential chain, and this module imports none of
// them directly.
var serviceEndpointEnvNames = []string{
	"AWS_ENDPOINT_URL_ACM",
	"AWS_ENDPOINT_URL_APIGATEWAYV2",
	"AWS_ENDPOINT_URL_API_GATEWAY",
	"AWS_ENDPOINT_URL_CLOUDFRONT",
	"AWS_ENDPOINT_URL_CLOUDTRAIL",
	"AWS_ENDPOINT_URL_CLOUDWATCH",
	"AWS_ENDPOINT_URL_CLOUDWATCH_LOGS",
	"AWS_ENDPOINT_URL_DYNAMODB",
	"AWS_ENDPOINT_URL_EC2",
	"AWS_ENDPOINT_URL_ECR",
	"AWS_ENDPOINT_URL_ECS",
	"AWS_ENDPOINT_URL_EFS",
	"AWS_ENDPOINT_URL_EKS",
	"AWS_ENDPOINT_URL_ELASTICACHE",
	"AWS_ENDPOINT_URL_ELASTIC_LOAD_BALANCING_V2",
	"AWS_ENDPOINT_URL_EVENTBRIDGE",
	"AWS_ENDPOINT_URL_IAM",
	"AWS_ENDPOINT_URL_KINESIS",
	"AWS_ENDPOINT_URL_KMS",
	"AWS_ENDPOINT_URL_LAMBDA",
	"AWS_ENDPOINT_URL_OPENSEARCH",
	"AWS_ENDPOINT_URL_RDS",
	"AWS_ENDPOINT_URL_REDSHIFT",
	"AWS_ENDPOINT_URL_ROUTE_53",
	"AWS_ENDPOINT_URL_S3",
	"AWS_ENDPOINT_URL_SECRETS_MANAGER",
	"AWS_ENDPOINT_URL_SESV2",
	"AWS_ENDPOINT_URL_SFN",
	"AWS_ENDPOINT_URL_SIGNIN",
	"AWS_ENDPOINT_URL_SNS",
	"AWS_ENDPOINT_URL_SQS",
	"AWS_ENDPOINT_URL_SSO",
	"AWS_ENDPOINT_URL_SSO_OIDC",
	"AWS_ENDPOINT_URL_STS",
}

// stdlibEnvNames are the names the Go standard library reads on this collector's
// behalf, which no census of this module's dependency closure can surface: the
// standard library is outside every module closure by construction.
//
// THE PROXY PAIR IS SIX NAMES, upper and lower case, because net/http's
// ProxyFromEnvironment reads both spellings — and the AWS SDK's default buildable
// HTTP client sets exactly that proxy function. An operator who has not listed
// them gets a collector that cannot dial an endpoint the AWS CLI on the same host
// reaches, surfacing as a transport error rather than as an explanation.
//
// THE TWO TRUST-ROOT NAMES ARE LISTED because AWS_CA_BUNDLE is already in the
// set above and overrides the SDK's OWN bundle without reaching crypto/x509's
// system pool. An operator on a TLS-intercepting host who set the standard names
// and not the AWS one would get a child that cannot complete a handshake — and a
// half-covered trust story is worse than either whole one, which is why both are
// here rather than neither.
var stdlibEnvNames = []string{
	"HTTPS_PROXY",
	"HTTP_PROXY",
	"NO_PROXY",
	"SSL_CERT_DIR",
	"SSL_CERT_FILE",
	"http_proxy",
	"https_proxy",
	"no_proxy",
}
