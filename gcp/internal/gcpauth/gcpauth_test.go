// SPDX-License-Identifier: Apache-2.0

package gcpauth_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/oauth2/google"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpauth"
)

// gcpauth_test.go — the credential arms, driven through an injected finder.
//
// THE FINDER IS INJECTED RATHER THAN THE ENVIRONMENT MANIPULATED, and that is a
// deliberate limit on what these tests claim. The real chain reads the home
// directory, and a test that relocated a process's home to provoke a miss would
// be reaching outside its own module for a result it can get honestly. What is
// asserted here is the CONTRACT this package adds: that a failed lookup fails
// loud and names the chain and its variables. That the provider's own chain
// behaves as documented is the live confirmation's to show, and it is not
// claimed here.

func TestFindNamesTheCredentialChainWhenTheLookupFails(t *testing.T) {
	_, err := gcpauth.Find(t.Context(), func(context.Context, ...string) (*google.Credentials, error) {
		return nil, errors.New("could not find default credentials")
	})
	if err == nil {
		t.Fatal("a failed credential lookup was reported as success")
	}
	for _, want := range []string{
		"Application Default Credentials",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"metadata",
		"environment block",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error does not mention %q, which an operator needs to fix it: %v", want, err)
		}
	}
	// The provider's own message survives, so an operator can still see what the
	// chain actually complained about.
	if !strings.Contains(err.Error(), "could not find default credentials") {
		t.Errorf("the underlying cause was discarded: %v", err)
	}
}

// A finder that returns neither a credential nor an error has said nothing, and
// proceeding on it would surface as an unauthenticated call against every API
// rather than as a credential problem.
func TestFindRefusesANilCredentialWithNoError(t *testing.T) {
	_, err := gcpauth.Find(t.Context(), func(context.Context, ...string) (*google.Credentials, error) {
		return nil, nil
	})
	if err == nil {
		t.Fatal("a nil credential with no error was accepted")
	}
	if !strings.Contains(err.Error(), "neither a credential nor an error") {
		t.Errorf("the error does not describe what happened: %v", err)
	}
}

func TestFindReturnsTheCredentialItWasGiven(t *testing.T) {
	want := &google.Credentials{ProjectID: "proj-a"}
	got, err := gcpauth.Find(t.Context(), func(_ context.Context, scopes ...string) (*google.Credentials, error) {
		// The scope is asserted here because it is the collector's whole
		// authority: a token that could write is a capability this collector has
		// no use for and an operator has no way to withhold.
		if len(scopes) != 1 || !strings.HasSuffix(scopes[0], "cloud-platform.read-only") {
			t.Errorf("requested scopes %v, want exactly the read-only platform scope", scopes)
		}
		return want, nil
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got != want {
		t.Error("Find returned a different credential from the one the finder produced")
	}
}
