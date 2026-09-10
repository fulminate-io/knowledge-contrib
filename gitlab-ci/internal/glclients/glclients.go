// SPDX-License-Identifier: Apache-2.0

// Package glclients builds the authenticated provider client a real walk
// enumerates through. It is the ONE place in this module that reads the
// environment and the one place that holds a credential.
//
// NOTHING HERE IS REACHED BY A TEST OF THE WALK. Every enumeration is written
// against the narrow interfaces in the collect package, so the whole collector is
// exercised over recorded responses with no credential and no network; what lives
// here is the wiring from those interfaces to the SDK's own service values, the
// token read, and the instance selector.
package glclients

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/enventry"
)

// DefaultBaseURL is the instance this collector talks to when its selector names
// none.
//
// THE TRAILING SLASH IS PART OF THE VALUE and not a typo. The selector is applied
// only when it DIFFERS from this string, so a selector set to the hosted
// instance's URL without the slash takes the explicit-base-URL path while one
// with it does not. Both reach the same host; the two differ in whether the SDK
// is handed an override, and the string is written here exactly as the source
// provider wrote it so the two agree.
const DefaultBaseURL = "https://gitlab.com/"

// API builds the provider client for one walk.
//
// THE TOKEN IS READ ONCE, HERE, AND GOES NOWHERE ELSE. It reaches the SDK's
// client constructor and nothing else in this module: no node, no metadata value,
// no log line, no error message, and no argument vector — this collector has no
// flags at all, so there is no shape in which a caller could pass one on a
// command line for another process on the machine to read.
func API(_ context.Context) (collect.API, func(), error) {
	token, err := Token(os.LookupEnv)
	if err != nil {
		return collect.API{}, nil, err
	}
	baseURL, err := BaseURL(os.LookupEnv)
	if err != nil {
		return collect.API{}, nil, err
	}

	client, err := newClient(token, baseURL)
	if err != nil {
		return collect.API{}, nil, err
	}
	// THE RELEASE IS A NO-OP AND IT IS RETURNED ANYWAY. This client holds an HTTP
	// client and nothing that needs closing, but the walk's contract is that what
	// it opens it releases; returning nil here would make a later client that DOES
	// need releasing an edit in two places instead of one.
	return services(client), func() {}, nil
}

// OverridesDefault reports whether a resolved base URL is one the SDK must be
// told about.
//
// IT IS AN EXACT COMPARISON, and it is a function rather than an inline
// condition so that the decision is observable. The source provider applies the
// base-URL option only when the value DIFFERS from its default, so a selector set
// to exactly the hosted instance's URL is handed to the SDK differently from one
// set to the same host without the trailing slash — and a rewrite reading
// "override whenever the variable is non-empty" changes that silently. A test
// drives this predicate directly; nothing about an applied ClientOptionFunc is
// inspectable after the fact.
func OverridesDefault(baseURL string) bool { return baseURL != DefaultBaseURL }

// newClient builds the SDK client, applying the base URL only when the selector
// chose one other than the default.
func newClient(token, baseURL string) (*gl.Client, error) {
	var opts []gl.ClientOptionFunc
	if OverridesDefault(baseURL) {
		opts = append(opts, gl.WithBaseURL(baseURL))
	}
	client, err := gl.NewClient(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("building the GitLab client: %w", err)
	}
	return client, nil
}

// Lookup is the environment reader, taken as a parameter so the resolution order
// and the failures are testable without mutating the process environment.
type Lookup func(name string) (string, bool)

// Token resolves the provider credential.
//
// THE ORDER IS FIXED AND THE FAILURE NAMES BOTH VARIABLES. A missing token is a
// refusal rather than an anonymous walk: an unauthenticated read of a group
// returns a fraction of what a member sees and none of what this collector is
// for, so it would land a graph that looks like a small group rather than a
// failed collect.
//
// A VARIABLE THAT IS PRESENT AND EMPTY IS A MISSING TOKEN, and the distinction
// matters because the transport preserves it: a config entry may declare a name
// with an empty value, so this collector can be handed the name without the
// secret. That is the same input as no token at all as far as the provider is
// concerned, and it fails the same way — which is what keeps an operator from
// reading that the variable is set and concluding the collector has a credential.
func Token(lookup Lookup) (string, error) {
	for _, name := range enventry.TokenNames() {
		if value, ok := lookup(name); ok && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf(
		"no GitLab token: this collector reads %s, falling back to %s, and neither is set to a "+
			"non-empty value in the environment its config entry declares. A stdio collector "+
			"receives exactly what its entry names, so declare one of them in the entry as a "+
			"reference to your own environment — %s — and set that variable in the environment "+
			"the daemon serves collects from. The token's value belongs in no config file",
		enventry.PrimaryTokenVariable, enventry.FallbackTokenVariable,
		`"`+enventry.PrimaryTokenVariable+`": "${`+enventry.PrimaryTokenVariable+`}"`)
}

// BaseURL resolves the instance selector.
//
// IT IS A SELECTOR AND NOT A CREDENTIAL. An operator running a self-hosted GitLab
// points this at their own host; the token still comes from the environment, and
// nothing about this value is secret.
//
// A VALUE CARRYING USERINFO IS REFUSED BY NAME AND ITS VALUE IS NOT QUOTED BACK.
// A URL of the form https://user:token@host embeds a credential, and this
// collector would then hand it to an SDK that puts it in every request and to any
// diagnostic that prints the base URL. The refusal names the variable and the
// host it was pointing at, which is everything an operator needs to fix it and
// nothing that leaks what they wrote.
func BaseURL(lookup Lookup) (string, error) {
	value, _ := lookup(enventry.BaseURLVariable)
	if strings.TrimSpace(value) == "" {
		// PRESENT AND EMPTY IS TREATED AS UNSET HERE, unlike the token. An empty
		// selector names no host at all, so there is nothing to select and the
		// default instance is the only reading that resolves anything.
		return DefaultBaseURL, nil
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf(
			"%s is not a URL this collector can parse. It is the base URL of the GitLab instance "+
				"to enumerate, such as %s", enventry.BaseURLVariable, DefaultBaseURL)
	}
	if parsed.User != nil {
		return "", fmt.Errorf(
			"%s carries userinfo before the host %q. That embeds a credential in a URL this "+
				"collector hands to an HTTP client and to every diagnostic that prints it, so it is "+
				"refused rather than used: give the host alone and set the token in %s",
			enventry.BaseURLVariable, parsed.Host, enventry.PrimaryTokenVariable)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf(
			"%s names no scheme or no host. It is the base URL of the GitLab instance to "+
				"enumerate, such as %s", enventry.BaseURLVariable, DefaultBaseURL)
	}
	return value, nil
}
