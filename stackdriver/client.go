// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"

	"cloud.google.com/go/logging/logadmin"
)

// client.go — BUILDING THE CLOUD LOGGING READ CLIENT, and the two failures an
// operator has to be able to tell apart.
//
// A COLLECT CAN FAIL TO REACH GOOGLE FOR TWO UNRELATED REASONS, and reporting
// either as the other sends the operator to the wrong place. Application
// Default Credentials may be absent or unusable, which is a credential problem
// they fix with gcloud or a key file. Or the TLS handshake may fail because the
// child's environment did not carry the trust-store or proxy names its config
// entry would have listed, which is a transport problem no amount of
// re-authenticating fixes. The two messages below name which one happened, and
// the dial error is never dressed up as a missing credential.

// readCloudLogging is the real read: build a client from Application Default
// Credentials, drain the entries iterator under the query's bound, close.
//
// NO OTHER CREDENTIAL MODE EXISTS, and their absence is the requirement rather
// than an omission. The knowledge client's own Cloud Logging adapter accepts an
// inline credential JSON blob and a service-account file path before falling
// back to ADC; a collector taking either would be a place operator secrets live
// in a config file the daemon reads and spawns from. This collector is passed no
// credential at all.
func readCloudLogging(ctx context.Context, projectID string, q logQuery) (drainResult, error) {
	client, err := logadmin.NewClient(ctx, projectID)
	if err != nil {
		return drainResult{}, credentialError(projectID, err)
	}
	defer func() {
		// A close failure after a completed read is worth saying out loud and is
		// not worth failing the collect over: the entries are already read.
		if closeErr := client.Close(); closeErr != nil {
			logDiagnostic("closing the Cloud Logging client for project %s: %v", projectID, closeErr)
		}
	}()
	return collectEntries(ctx, client, projectID, q)
}

// credentialError wraps a client-construction failure with what an operator can
// act on: that this collector authenticates with Application Default
// Credentials only, and the three places ADC looks for one.
func credentialError(projectID string, err error) error {
	return fmt.Errorf(
		"stackdriver: could not build a Cloud Logging client for project %s using "+
			"Application Default Credentials, which is the only credential source this collector has. "+
			"ADC reads GOOGLE_APPLICATION_CREDENTIALS, then the gcloud well-known file under the home "+
			"directory, then the metadata server; a collector spawned with none of those names in its "+
			"config entry's env block sees none of them. This is a CREDENTIAL failure, not a network "+
			"one: %w", projectID, err)
}

// logDiagnostic writes one line to STDERR.
//
// STDOUT IS THE PROTOCOL STREAM on the stdio transport this collector is
// installed under: a single stray byte written there is not a log line, it is a
// corrupt JSON-RPC frame, and it surfaces to the operator as an opaque handshake
// failure with nothing pointing at the print that caused it. Every diagnostic
// this module writes goes through here.
func logDiagnostic(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "stackdriver collector: "+format+"\n", args...)
}
