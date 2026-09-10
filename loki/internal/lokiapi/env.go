// SPDX-License-Identifier: Apache-2.0

// Package lokiapi is this collector's Loki reader: the environment it takes its
// credentials and transport settings from, the HTTP client it builds from them,
// the LogQL it constructs, and the backward time-narrowing walk that pages
// through query_range.
package lokiapi

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// env.go — THE WHOLE ENVIRONMENT THIS COLLECTOR READS, declared in one place.
//
// The names are logcli's, because an operator who runs logcli against their
// Loki already has them set and expects them to work here. logcli declares
// exactly nineteen and reads no configuration file, so this list plus the
// address is the entire surface: there is no credential-chain analog to
// AWS's shared config or GCP's application default credentials, and no home
// directory is required.
//
// THE ADDRESS IS NOT HERE. LOKI_ADDR is the nineteenth name logcli declares and
// this collector deliberately does not read it: the endpoint is a required TOOL
// PARAMETER, because the collect names which Loki it is collecting from.
//
// THIS COLLECTOR READS ALL EIGHTEEN. Reading a subset would be admissible, but
// each name left out owes a written statement of what it costs an operator who
// sets it and finds it ignored; reading them all discharges that in one line.
// Names(), below, is what the module's install guide lists in its config entry,
// and a config entry's env block is the child's WHOLE environment — nothing is
// inherited from the daemon — so a name absent from the block is absent here.
//
// TWO NAMES BELOW ARE NOT LOKI'S AND ARE LISTED ANYWAY. SSL_CERT_FILE and
// SSL_CERT_DIR are read by crypto/x509 on the unix platforms, outside any
// module's own dependency closure, so no census of this module can find them.
// A Loki address is an operator-hosted endpoint, commonly behind TLS with a
// private certificate authority; the two names are file paths carrying no
// credential, they are inert when unset and on a platform whose root reader
// ignores them, and an operator whose entry omits them sees a transport error
// naming neither cause. They are declared unconditionally, NOT gated on the
// proxy flag and not conditional on the target platform: the entry decides what
// the child receives, and the platform decides only whether x509 consults it.
const (
	// Credentials.
	EnvUsername        = "LOKI_USERNAME"
	EnvPassword        = "LOKI_PASSWORD"
	EnvBearerToken     = "LOKI_BEARER_TOKEN"
	EnvBearerTokenFile = "LOKI_BEARER_TOKEN_FILE"
	EnvAuthHeader      = "LOKI_AUTH_HEADER"
	EnvOrgID           = "LOKI_ORG_ID"

	// TLS.
	EnvCACertPath     = "LOKI_CA_CERT_PATH"
	EnvTLSSkipVerify  = "LOKI_TLS_SKIP_VERIFY"
	EnvClientCertPath = "LOKI_CLIENT_CERT_PATH"
	EnvClientKeyPath  = "LOKI_CLIENT_KEY_PATH"

	// Transport and request behavior.
	EnvHTTPProxyURL     = "LOKI_HTTP_PROXY_URL"
	EnvEnvProxy         = "LOKI_ENV_PROXY"
	EnvHTTPCompression  = "LOKI_HTTP_COMPRESSION"
	EnvNoCache          = "LOKI_NO_CACHE"
	EnvQueryTags        = "LOKI_QUERY_TAGS"
	EnvClientRetries    = "LOKI_CLIENT_RETRIES"
	EnvClientMinBackoff = "LOKI_CLIENT_MIN_BACKOFF"
	EnvClientMaxBackoff = "LOKI_CLIENT_MAX_BACKOFF"
)

// proxyDelegateNames are the six standard Go proxy variables LOKI_ENV_PROXY
// delegates to. Honoring that flag without these in the config entry's env
// block would make the flag decide nothing, because net/http reads them from
// the process environment and the child's environment is the block.
var proxyDelegateNames = []string{
	"HTTP_PROXY", "http_proxy",
	"HTTPS_PROXY", "https_proxy",
	"NO_PROXY", "no_proxy",
}

// trustRootNames are the two unix trust-root variables crypto/x509 consults.
var trustRootNames = []string{"SSL_CERT_FILE", "SSL_CERT_DIR"}

// lokiNames are the eighteen logcli names this collector reads, in the order
// the ticket groups them.
var lokiNames = []string{
	EnvUsername, EnvPassword, EnvBearerToken, EnvBearerTokenFile, EnvAuthHeader, EnvOrgID,
	EnvCACertPath, EnvTLSSkipVerify, EnvClientCertPath, EnvClientKeyPath,
	EnvHTTPProxyURL, EnvEnvProxy, EnvHTTPCompression, EnvNoCache, EnvQueryTags,
	EnvClientRetries, EnvClientMinBackoff, EnvClientMaxBackoff,
}

// Names returns every environment variable this collector reads: the eighteen
// Loki names, the six proxy delegates and the two trust roots. It is what the
// install guide's example config entry lists, and it is what the census test
// checks the guide against, so the guide cannot drift from the code.
//
// The returned slice is a fresh copy, so a caller cannot reorder the package's
// own list.
func Names() []string {
	out := make([]string, 0, len(lokiNames)+len(proxyDelegateNames)+len(trustRootNames))
	out = append(out, lokiNames...)
	out = append(out, proxyDelegateNames...)
	out = append(out, trustRootNames...)
	return out
}

// LookupFunc reads one environment variable. Every reader below takes one so a
// test can supply an environment without mutating the process's, which matters
// because these tests run in parallel with everything else in the package.
type LookupFunc func(name string) (string, bool)

// OSLookup reads the real process environment.
func OSLookup(name string) (string, bool) { return os.LookupEnv(name) }

// Settings is the environment as this collector understands it, after every
// value has been parsed and every partner requirement checked.
type Settings struct {
	Username        string
	Password        string
	BearerToken     string
	BearerTokenFile string
	// AuthHeader is the header a bearer token is written into. It defaults to
	// Authorization; an operator behind a gateway that reserves that header
	// sets another.
	AuthHeader string
	OrgID      string

	CACertPath     string
	TLSSkipVerify  bool
	ClientCertPath string
	ClientKeyPath  string

	HTTPProxyURL     string
	EnvProxy         bool
	HTTPCompression  bool
	NoCache          bool
	QueryTags        string
	ClientRetries    int
	ClientMinBackoff time.Duration
	ClientMaxBackoff time.Duration
}

// defaultAuthHeader is the header a bearer token goes into when the operator
// names none.
const defaultAuthHeader = "Authorization"

// Default retry behavior when the operator sets none. Retries are bounded and
// small: a collect is one tool call an operator is waiting on, and a Loki that
// is down should say so rather than be waited out.
const (
	defaultClientRetries    = 0
	defaultClientMinBackoff = 500 * time.Millisecond
	defaultClientMaxBackoff = 5 * time.Second
)

// LoadSettings reads and validates the environment.
//
// EVERY MALFORMED VALUE IS AN ERROR NAMING THE VARIABLE AND THE VALUE — never a
// coerced default. A collector that read LOKI_TLS_SKIP_VERIFY=yes as false
// would silently verify certificates an operator meant to skip; one that read
// LOKI_CLIENT_RETRIES=three as zero would silently stop retrying. Both are the
// silent degrade this project's bad-input rule exists to prevent.
//
// EVERY HALF-SET CREDENTIAL PAIR IS AN ERROR NAMING THE MISSING HALF. A
// username with no password would otherwise send an empty password, and a
// client certificate with no key would fall back to an unauthenticated
// handshake — a nil-auth request where the operator asked for an authenticated
// one.
//
// AND A SET-BUT-EMPTY VALUE IS AN ERROR FOR A STRING ON THE SAME TERMS AS FOR A
// BOOLEAN. Clearing a variable to the empty string in a config entry is not the
// same as never setting it, and the entry's block is the child's whole
// environment, so an operator who wrote `"LOKI_BEARER_TOKEN": ""` meant
// something. Treating it as unset sends an UNAUTHENTICATED request and reports
// nothing, which is the coercion this whole file refuses.
func LoadSettings(lookup LookupFunc) (Settings, error) {
	if lookup == nil {
		return Settings{}, fmt.Errorf("lokiapi: LoadSettings needs a lookup function")
	}

	// firstErr keeps the FIRST refusal rather than the last, so the message an
	// operator sees names the earliest variable in the declared order rather
	// than whichever field happens to be assigned last.
	var firstErr error
	get := func(name string) string {
		v, err := envString(lookup, name)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return v
	}

	s := Settings{
		Username:        get(EnvUsername),
		Password:        get(EnvPassword),
		BearerToken:     get(EnvBearerToken),
		BearerTokenFile: get(EnvBearerTokenFile),
		AuthHeader:      get(EnvAuthHeader),
		OrgID:           get(EnvOrgID),
		CACertPath:      get(EnvCACertPath),
		ClientCertPath:  get(EnvClientCertPath),
		ClientKeyPath:   get(EnvClientKeyPath),
		HTTPProxyURL:    get(EnvHTTPProxyURL),
		QueryTags:       get(EnvQueryTags),
	}
	if firstErr != nil {
		return Settings{}, firstErr
	}
	if s.AuthHeader == "" {
		s.AuthHeader = defaultAuthHeader
	}

	var err error
	if s.TLSSkipVerify, err = envBool(lookup, EnvTLSSkipVerify); err != nil {
		return Settings{}, err
	}
	if s.EnvProxy, err = envBool(lookup, EnvEnvProxy); err != nil {
		return Settings{}, err
	}
	if s.HTTPCompression, err = envBool(lookup, EnvHTTPCompression); err != nil {
		return Settings{}, err
	}
	if s.NoCache, err = envBool(lookup, EnvNoCache); err != nil {
		return Settings{}, err
	}
	if s.ClientRetries, err = envNonNegativeInt(lookup, EnvClientRetries, defaultClientRetries); err != nil {
		return Settings{}, err
	}
	if s.ClientMinBackoff, err = envDuration(lookup, EnvClientMinBackoff, defaultClientMinBackoff); err != nil {
		return Settings{}, err
	}
	if s.ClientMaxBackoff, err = envDuration(lookup, EnvClientMaxBackoff, defaultClientMaxBackoff); err != nil {
		return Settings{}, err
	}
	if s.ClientMaxBackoff < s.ClientMinBackoff {
		return Settings{}, fmt.Errorf(
			"lokiapi: %s=%s is shorter than %s=%s; a maximum backoff below the minimum has no meaning",
			EnvClientMaxBackoff, s.ClientMaxBackoff, EnvClientMinBackoff, s.ClientMinBackoff)
	}

	if err := s.checkCredentialPairs(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

// checkCredentialPairs refuses a half-set credential or key pair, naming the
// variable the operator has to set.
func (s Settings) checkCredentialPairs() error {
	switch {
	case s.Username != "" && s.Password == "":
		return fmt.Errorf("lokiapi: %s is set but %s is not; basic auth needs both, and sending an empty password would authenticate as nobody",
			EnvUsername, EnvPassword)
	case s.Password != "" && s.Username == "":
		return fmt.Errorf("lokiapi: %s is set but %s is not; basic auth needs both", EnvPassword, EnvUsername)
	case s.ClientCertPath != "" && s.ClientKeyPath == "":
		return fmt.Errorf("lokiapi: %s is set but %s is not; a client certificate needs its private key, and continuing would dial unauthenticated",
			EnvClientCertPath, EnvClientKeyPath)
	case s.ClientKeyPath != "" && s.ClientCertPath == "":
		return fmt.Errorf("lokiapi: %s is set but %s is not; a client key needs its certificate", EnvClientKeyPath, EnvClientCertPath)
	case s.BearerToken != "" && s.BearerTokenFile != "":
		return fmt.Errorf("lokiapi: %s and %s are both set; they are two spellings of one credential, so which one is meant is undecidable",
			EnvBearerToken, EnvBearerTokenFile)
	}
	return nil
}

// envString reads a string-valued name. An ABSENT name yields the empty string
// and no error, which is how a variable the operator did not set is spelled; a
// PRESENT name carrying the empty string is an error naming the variable, on the
// same terms envBool refuses one.
func envString(lookup LookupFunc, name string) (string, error) {
	raw, ok := lookup(name)
	if !ok {
		return "", nil
	}
	if raw == "" {
		return "", fmt.Errorf(
			"lokiapi: %s is set to the empty string; a name in the config entry's env block is a value the operator chose, "+
				"and an empty one is not the same as leaving the name out — remove the line to leave it unset", name)
	}
	return raw, nil
}

// envBool reads a boolean. It accepts what strconv.ParseBool accepts and
// nothing else.
//
// AN UNSET NAME IS FALSE, and that is stated here rather than carried as a
// per-call default because every boolean this collector reads is a feature an
// operator turns ON: skip verification, honor the environment proxy, negotiate
// compression, bypass the cache. A default of true for any of them would be a
// behavior nobody asked for.
//
// A SET-BUT-EMPTY NAME IS AN ERROR rather than the default: clearing a variable
// to the empty string in a config entry is not the same as never setting it,
// and deciding it silently is the coercion this whole file refuses.
func envBool(lookup LookupFunc, name string) (bool, error) {
	raw, ok := lookup(name)
	if !ok {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("lokiapi: %s=%q is not a boolean; use true or false (1 and 0 are also accepted)", name, raw)
	}
	return v, nil
}

// envNonNegativeInt reads a count. A negative value is refused rather than
// clamped: it means the operator expected something this collector cannot do.
func envNonNegativeInt(lookup LookupFunc, name string, def int) (int, error) {
	raw, ok := lookup(name)
	if !ok {
		return def, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("lokiapi: %s=%q is not a whole number", name, raw)
	}
	if v < 0 {
		return 0, fmt.Errorf("lokiapi: %s=%q is negative; a retry count is zero or more", name, raw)
	}
	return v, nil
}

// envDuration reads a Go duration string such as 500ms or 2s.
func envDuration(lookup LookupFunc, name string, def time.Duration) (time.Duration, error) {
	raw, ok := lookup(name)
	if !ok {
		return def, nil
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("lokiapi: %s=%q is not a duration; write it as Go does, for example 500ms or 2s", name, raw)
	}
	if v < 0 {
		return 0, fmt.Errorf("lokiapi: %s=%q is negative; a backoff is zero or more", name, raw)
	}
	return v, nil
}
