// SPDX-License-Identifier: Apache-2.0

// Package ghclients builds the authenticated provider clients a real walk
// enumerates through. It is the ONE place in this module that reads the
// environment and the one place that holds a credential.
//
// NOTHING HERE IS REACHED BY A TEST OF THE WALK. Every enumeration is written
// against the two narrow interfaces in the collect package, so the whole
// collector is exercised over recorded responses with no credential and no
// network; what lives here is the wiring from those interfaces to the SDK's own
// service values, plus the token read.
package ghclients

import (
	"context"
	"fmt"
	"os"

	gogithub "github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/enventry"
)

// API builds the provider clients for one walk.
//
// THE TOKEN IS READ ONCE, HERE, AND GOES NOWHERE ELSE. It reaches the SDK's
// client constructor and nothing else in this module: no node, no metadata
// value, no log line, no error message, and no argument vector — this collector
// has no flags at all, so there is no shape in which a caller could pass one on a
// command line for another process on the machine to read.
func API(ctx context.Context) (collect.API, func(), error) {
	token, err := Token(os.LookupEnv)
	if err != nil {
		return collect.API{}, nil, err
	}
	client := gogithub.NewClient(oauth2.NewClient(ctx,
		oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})))
	// THE RELEASE IS A NO-OP AND IT IS RETURNED ANYWAY. These clients hold an
	// HTTP client and nothing that needs closing, but the walk's contract is that
	// what it opens it releases; returning nil here would make a later client
	// that DOES need releasing an edit in two places instead of one.
	return collect.API{Repos: client.Repositories, Actions: client.Actions}, func() {}, nil
}

// Lookup is the environment reader, taken as a parameter so the resolution order
// and the failure are testable without mutating the process environment.
type Lookup func(name string) (string, bool)

// Token resolves the provider credential.
//
// THE ORDER IS FIXED AND THE FAILURE NAMES BOTH VARIABLES. A missing token is a
// refusal rather than an anonymous walk: an unauthenticated read of an
// organization returns a fraction of what a member sees and none of what this
// collector is for, so it would land a graph that looks like a small
// organization rather than a failed collect.
//
// A VARIABLE THAT IS PRESENT AND EMPTY IS A MISSING TOKEN, and the distinction
// matters because the transport preserves it: a config entry may declare a name
// with an empty value, so this collector can be handed the name without the
// secret. That is the same input as no token at all as far as the provider is
// concerned, and it fails the same way — which is what keeps an operator from
// reading "GITHUB_TOKEN is set" and concluding the collector has a credential.
func Token(lookup Lookup) (string, error) {
	for _, name := range enventry.Names() {
		if value, ok := lookup(name); ok && value != "" {
			return value, nil
		}
	}
	return "", fmt.Errorf(
		"no GitHub token: this collector reads %s, falling back to %s, and neither is set to a "+
			"non-empty value in the environment its config entry declares. A stdio collector "+
			"receives exactly what its entry names, so declare one of them in the entry as a "+
			"reference to your own environment — %s — and set that variable in the environment "+
			"the daemon serves collects from. The token's value belongs in no config file",
		enventry.PrimaryTokenVariable, enventry.FallbackTokenVariable,
		`"`+enventry.PrimaryTokenVariable+`": "${`+enventry.PrimaryTokenVariable+`}"`)
}
