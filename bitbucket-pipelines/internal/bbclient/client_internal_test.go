// SPDX-License-Identifier: Apache-2.0

package bbclient

import (
	"testing"
	"time"
)

// client_internal_test.go — the CONSTRUCTED PROPERTIES of the client the shipped
// binary builds.
//
// WHY AN INTERNAL TEST. The base URL and the timeout are unexported fields, and
// the only other way to observe them is to make a request — which would be a
// request to the real provider from a test suite that must never reach the
// network. Reading the constructed value is the honest observation; the timeout
// in particular cannot be observed by waiting for it without spending thirty
// seconds of suite time per run.

// TestTheShippedConstructorPointsAtTheProviderWithTheTimeout.
func TestTheShippedConstructorPointsAtTheProviderWithTheTimeout(t *testing.T) {
	client := New("user", "app-password")

	if client.baseURL != DefaultBaseURL {
		t.Errorf("the shipped client's base URL is %q, want %q", client.baseURL, DefaultBaseURL)
	}
	if DefaultBaseURL != "https://api.bitbucket.org/2.0" {
		t.Errorf("the base URL constant is %q; it is the provider's REST v2.0 root", DefaultBaseURL)
	}
	if client.httpClient == nil {
		t.Fatal("the shipped client carries no HTTP client")
	}
	if client.httpClient.Timeout != DefaultTimeout {
		t.Errorf("the shipped client's timeout is %s, want %s",
			client.httpClient.Timeout, DefaultTimeout)
	}
	if DefaultTimeout != 30*time.Second {
		t.Errorf("the timeout constant is %s; the source provider bounds a request at 30s",
			DefaultTimeout)
	}
	if client.username != "user" || client.appPassword != "app-password" {
		t.Errorf("the constructor did not carry the credential through")
	}
}

// TestThePageMaximumIsTheProvidersOwn. It is what the pagination appends and
// what the capped enumeration takes a minimum against, so a wrong value is
// either a request the provider refuses or a page smaller than it allows.
func TestThePageMaximumIsTheProvidersOwn(t *testing.T) {
	if MaxPagelen != 100 {
		t.Errorf("the page maximum is %d, want the provider's own 100", MaxPagelen)
	}
	if MaxRetries != 3 {
		t.Errorf("the retry budget is %d, want 3 — with the unconditional final request that is "+
			"four attempts on a persistently rate-limited URL", MaxRetries)
	}
}
