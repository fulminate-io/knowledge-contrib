// SPDX-License-Identifier: Apache-2.0

// Package gcpauth resolves the credential this collector authenticates with.
//
// THERE IS ONE SOURCE AND IT IS THE MACHINE'S OWN. Application Default
// Credentials is the provider's own standard chain — an explicit credential file
// named by the environment, then the account a developer logged in with, then
// the metadata server on an instance — and this collector adds nothing to it:
// no flag, no config field, no credential of its own. A caller passes no secret,
// so there is no secret for a caller to leak.
//
// A MISSING CREDENTIAL FAILS LOUD AND NAMES THE CHAIN. The provider's own error
// is accurate but says nothing about what an operator should do, and an operator
// who has never heard of the chain reads it as an unexplained refusal.
package gcpauth

import (
	"context"
	"fmt"

	"golang.org/x/oauth2/google"
)

// readOnlyScope is the ONLY scope this collector requests. It enumerates
// infrastructure and writes nothing, so a token that could write would be a
// capability it has no use for and an operator has no way to withhold.
const readOnlyScope = "https://www.googleapis.com/auth/cloud-platform.read-only"

// Finder resolves a credential. It is a parameter so a caller can substitute one
// in a test; production passes nothing and gets the provider's own chain.
type Finder func(ctx context.Context, scopes ...string) (*google.Credentials, error)

// Find resolves the credential this collector runs as.
//
// finder is optional: nil means the provider's own Application Default
// Credentials chain, which is what production uses.
func Find(ctx context.Context, finder Finder) (*google.Credentials, error) {
	if finder == nil {
		finder = google.FindDefaultCredentials
	}
	creds, err := finder(ctx, readOnlyScope)
	if err != nil {
		return nil, fmt.Errorf(
			"no usable credential: this collector authenticates ONLY through Application Default "+
				"Credentials, which looks for the file named by GOOGLE_APPLICATION_CREDENTIALS, then the "+
				"logged-in account's well-known file under the home directory, then the instance metadata "+
				"server. Every one of those names must be present in this collector's own environment "+
				"block for the child process to see it: %w", err)
	}
	if creds == nil {
		// A finder that returns neither a credential nor an error has told this
		// collector nothing, and proceeding would surface later as an
		// unauthenticated call against every API.
		return nil, fmt.Errorf(
			"no usable credential: the Application Default Credentials lookup returned neither a " +
				"credential nor an error")
	}
	return creds, nil
}
