// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"fmt"
	"sort"
)

// env.go — THE ENVIRONMENT THIS COLLECTOR NEEDS, AS DATA.
//
// A collector installed through the config file gets exactly the environment
// its entry's `env` block spells out: the daemon copies nothing from its own
// environment and adds nothing, so a variable absent from the block is absent
// in this process, indistinguishable from one the operator never set. That
// makes the block an INTERFACE, and this table is its source of truth.
//
// THE TABLE IS A PURE FUNCTION OF A TARGET OS, not a runtime.GOOS branch. An
// inline `if runtime.GOOS == "windows"` arm inside a builder is dead code on
// every machine that runs the tests — no test runner in this repository runs
// Windows — so the Windows row would be green forever and a module that simply
// omitted the name would ship. Taking the target as a PARAMETER makes every
// row executable on any runner.
//
// EVERY NAME THE CREDENTIAL AND TRANSPORT CLOSURE READS IS IN ONE OF TWO
// TABLES AND THERE IS NO THIRD CLASS: [declaredEnv] lists what the entry must
// carry and what each name does, and [excludedEnv] lists what the entry
// deliberately leaves out and what omitting it costs. A name read by the
// closure and present in neither table is the silent degrade this repository's
// bad-input invariant forbids.

// EnvVar is one variable of the collector's environment interface: the name an
// operator writes in the entry's env block, and the sentence saying what it
// does.
type EnvVar struct {
	// Name is the variable name, spelled exactly as the reader reads it.
	Name string
	// Reason says what the variable does, for a declared name, or what
	// omitting it costs, for an excluded one.
	Reason string
}

// Supported target operating systems. These are the three the release matrix
// builds; a target outside them is refused rather than served an approximate
// table, because the difference between them IS the table.
const (
	OSLinux   = "linux"
	OSDarwin  = "darwin"
	OSWindows = "windows"
)

// EnvironmentNames returns the variables this collector's config entry must
// declare when the collector runs on the named target OS, sorted by name.
//
// It REFUSES an unrecognized target rather than returning the portable subset:
// a caller asking about an OS this table does not describe is asking a question
// the table cannot answer, and a plausible-looking answer is worse than none.
func EnvironmentNames(goos string) ([]EnvVar, error) {
	if !supportedOS(goos) {
		return nil, fmt.Errorf(
			"azure collector: no environment table for target OS %q; this collector describes %s, %s and %s",
			goos, OSLinux, OSDarwin, OSWindows)
	}
	out := append([]EnvVar(nil), declaredEnvAllTargets...)
	switch goos {
	case OSWindows:
		out = append(out, declaredEnvWindows...)
	case OSLinux:
		// The unix trust-root names are read by crypto/x509's root_unix.go,
		// whose build constraint covers linux and the BSDs but NOT darwin and
		// NOT windows. On darwin the system roots come from the Security
		// framework and these two names are read by nothing at all.
		out = append(out, declaredEnvUnixTrustRoots...)
		out = append(out, declaredEnvPosixHome...)
	case OSDarwin:
		out = append(out, declaredEnvPosixHome...)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ExcludedEnvironmentNames returns the variables this collector's closure reads
// and the entry deliberately does NOT declare, each with the cost of omitting
// it. It is the other half of the disposition: a censused name appears here or
// in [EnvironmentNames], never in neither.
func ExcludedEnvironmentNames() []EnvVar {
	out := append([]EnvVar(nil), excludedEnv...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func supportedOS(goos string) bool {
	switch goos {
	case OSLinux, OSDarwin, OSWindows:
		return true
	default:
		return false
	}
}

// declaredEnvAllTargets is what every target needs: the whole
// DefaultAzureCredential chain's own variables, this collector's subscription
// selector, the two region names, and the proxy family the Go transport reads
// on every platform.
var declaredEnvAllTargets = []EnvVar{
	// The collector's own read: which subscription to walk, when neither the
	// collect id nor the params carry one.
	{"AZURE_SUBSCRIPTION_ID", "the subscription this collector walks, when the collect id and params name none"},

	// azidentity's EnvironmentCredential and the chain's shared options.
	{"AZURE_TENANT_ID", "the Entra tenant the service-principal and user credentials authenticate against"},
	{"AZURE_CLIENT_ID", "the application (client) id of the service principal, or the user-assigned managed identity"},
	{"AZURE_CLIENT_SECRET", "the service-principal secret, for the client-secret arm of the chain"},
	{"AZURE_CLIENT_CERTIFICATE_PATH", "path to the PEM or PKCS12 certificate, for the client-certificate arm"},
	{"AZURE_CLIENT_CERTIFICATE_PASSWORD", "password protecting that certificate file, when it is encrypted"},
	{"AZURE_CLIENT_SEND_CERTIFICATE_CHAIN", "sends the whole certificate chain, which subject-name/issuer authentication requires"},
	{"AZURE_USERNAME", "the username for the resource-owner-password arm of EnvironmentCredential"},
	{"AZURE_PASSWORD", "the password for that same arm"},
	{"AZURE_ADDITIONALLY_ALLOWED_TENANTS", "extra tenants a credential may acquire tokens for, beyond its own"},
	{"AZURE_AUTHORITY_HOST", "the Entra authority host, which a sovereign or air-gapped cloud changes"},
	{"AZURE_TOKEN_CREDENTIALS", "selects which arms of the default chain are built at all"},
	{"AZURE_FEDERATED_TOKEN_FILE", "path to the projected token WorkloadIdentityCredential exchanges"},
	{"AZURE_REGIONAL_AUTHORITY_NAME", "pins the token authority region; the literal TryAutoDetect makes MSAL detect one"},

	// The managed-identity arm, which azidentity delegates to MSAL.
	{"IDENTITY_ENDPOINT", "the managed-identity token endpoint injected by App Service, Container Apps and Arc"},
	{"IDENTITY_HEADER", "the secret header value that endpoint requires"},
	{"IDENTITY_SERVER_THUMBPRINT", "the Service Fabric managed-identity server thumbprint"},
	{"IMDS_ENDPOINT", "the Azure Arc instance-metadata endpoint, which pairs with IDENTITY_ENDPOINT"},
	{"MSI_ENDPOINT", "the legacy App Service managed-identity endpoint"},
	{"MSI_SECRET", "the secret paired with the legacy endpoint"},
	{"AZURE_POD_IDENTITY_AUTHORITY_HOST", "the AAD Pod Identity host; declared for completeness, and read by nothing at the pinned SDK versions"},
	{"DEFAULT_IDENTITY_CLIENT_ID", "the client id a system-assigned-by-default managed-identity host injects"},

	// Region detection. Both are SILENT degrades when absent: the SDK picks a
	// different token authority and reports nothing either way, so the only
	// remedy is to let the operator's setting reach this process.
	{"MSAL_FORCE_REGION", "forces the token authority region on every confidential-client construction; absent, the region is chosen without the operator's setting"},
	{"REGION_NAME", "the region MSAL's auto-detection reads when a region name is TryAutoDetect; absent, it falls through to an IMDS probe and then to the non-regional authority"},

	// SDK logging.
	{"AZURE_SDK_GO_LOGGING", "turns on the Azure SDK's own logging, which goes to stderr"},

	// The transport. net/http's proxy resolution reads all six on every
	// platform, and the resolution is memoized once per process, so the entry
	// decides it before the first request.
	{"HTTPS_PROXY", "the proxy for the HTTPS calls this collector makes to the management plane"},
	{"https_proxy", "the lowercase spelling of the same, which the Go proxy resolver reads too"},
	{"HTTP_PROXY", "the proxy for plain HTTP, read by the same resolver"},
	{"http_proxy", "the lowercase spelling of the same"},
	{"NO_PROXY", "the hosts that bypass the proxy"},
	{"no_proxy", "the lowercase spelling of the same"},

	// The three developer-tool arms of the chain shell out, so the child needs
	// to be able to find the tool.
	{"PATH", "how the AzureCLI, AzureDeveloperCLI and AzurePowerShell arms of the chain find the tool they shell out to"},
}

// declaredEnvPosixHome is HOME, which the three developer-tool arms need to
// find their cached credentials. AZURE_CONFIG_DIR is an ALTERNATIVE to it,
// read by the az process rather than by azidentity, not an addition.
var declaredEnvPosixHome = []EnvVar{
	{"HOME", "where the AzureCLI and AzureDeveloperCLI arms find their cached credentials (AZURE_CONFIG_DIR replaces it rather than adding to it)"},
}

// declaredEnvUnixTrustRoots is the pair crypto/x509 reads to find the CA bundle
// on the unix build list. Absent on a host whose bundle is not at a compiled-in
// default, TLS to the management plane fails at the handshake — loudly, but
// with an error about certificates rather than about configuration.
var declaredEnvUnixTrustRoots = []EnvVar{
	{"SSL_CERT_FILE", "the CA bundle file crypto/x509 verifies the management plane's certificate against"},
	{"SSL_CERT_DIR", "the CA bundle directory it reads when no single file is named"},
}

// declaredEnvWindows is the pair read only on Windows. Both are SILENT or
// near-silent when absent: azidentity's Windows developer-credential arm
// returns credentialUnavailable naming SYSTEMROOT, and the Azure Arc
// managed-identity path reads ProgramData without complaining.
var declaredEnvWindows = []EnvVar{
	{"SYSTEMROOT", "where azidentity's Windows developer-credential arm finds the shell it runs; without it that arm reports itself unavailable"},
	{"ProgramData", "where the Azure Arc managed-identity path looks for its key file"},
}

// excludedEnv is every name the credential and transport closure reads that
// this collector's entry deliberately does not declare, each with the cost.
//
// GOOGLE_APPLICATION_CREDENTIALS IS THE CONTROL ON THIS TABLE. It is in the
// module's dependency closure, reached through the MCP SDK's transitive
// x/oauth2 dependency, and a census that declared every name it found would
// have declared it. The reason to exclude it is a path argument — this
// collector builds no Google credential — rather than a preference.
var excludedEnv = []EnvVar{
	{"OS", "the SDK composes a User-Agent token from it on Windows; omitting it costs a generic token on management-plane calls and changes nothing else"},
	{"REQUEST_METHOD", "the proxy resolver treats its presence as a CGI environment; declaring it would CHANGE behavior rather than enable any"},
	{"GOOGLE_APPLICATION_CREDENTIALS", "read by x/oauth2, which is in the closure through the MCP SDK; this collector builds no Google credential, so nothing reads it on any path taken here"},
	{"SYSTEM_OIDCREQUESTURI", "read by AzurePipelinesCredential, which is not one of the arms DefaultAzureCredential builds"},
	{"HOSTALIASES", "its only effect is to select the cgo resolver; omitting it preserves the default resolver choice rather than degrading it"},
	{"LOCALDOMAIN", "same: resolver selection only"},
	{"RES_OPTIONS", "same: resolver selection only"},
	{"PATHEXT", "read by Windows LookPath; absent, it falls back to the compiled-in .com .exe .bat .cmd and still resolves cmd.exe"},
	{"APPDATA", "the Windows arm of os.UserHomeDir, whose only caller in this closure is behind a build constraint this module never builds under"},
	{"USER", "the os/user username fallback, behind that same constraint"},
}

// ExampleEntry renders the config-file entry an operator writes to install this
// collector on the named target OS, as pretty-printed JSON.
//
// IT IS A GENERATOR RATHER THAN A DOCUMENT so the entry in README.md cannot
// drift from the table above: a test renders this and compares it to the
// README's own block, and the comparison fails when either side moves.
func ExampleEntry(goos, command string) (string, error) {
	vars, err := EnvironmentNames(goos)
	if err != nil {
		return "", err
	}
	var b []byte
	b = append(b, "{\n"...)
	b = append(b, "  \"azure\": {\n"...)
	b = append(b, "    \"type\": \"stdio\",\n"...)
	b = append(b, fmt.Sprintf("    \"command\": %q,\n", command)...)
	b = append(b, "    \"tool\": \"collect\",\n"...)
	b = append(b, "    \"behavior\": {\n"...)
	b = append(b, "      \"summarizable\": true,\n"...)
	b = append(b, "      \"embeddable\": true,\n"...)
	b = append(b, "      \"syncable\": true,\n"...)
	b = append(b, fmt.Sprintf("      \"bm25_fields\": [%q, %q, %q]\n", "summary", "symbol_name", "content")...)
	b = append(b, "    },\n"...)
	b = append(b, "    \"env\": {\n"...)
	for i, v := range vars {
		sep := ","
		if i == len(vars)-1 {
			sep = ""
		}
		b = append(b, fmt.Sprintf("      %q: %q%s\n", v.Name, "", sep)...)
	}
	b = append(b, "    }\n"...)
	b = append(b, "  }\n"...)
	b = append(b, "}\n"...)
	return string(b), nil
}
