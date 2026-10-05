// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"
)

// credentialFreeBlock is the child environment block for a collect that must
// find NO credential, whatever the machine running the suite holds.
//
// AN EMPTY BLOCK IS NOT ENOUGH. With HOME absent, the AWS SDK resolves the home
// directory from the user database instead, so on a machine whose operator has
// a shared credentials file an empty block still finds it, and the collect
// signs a real request to AWS with the operator's credential. Naming both
// shared files at paths that do not exist replaces the SDK's defaults, so the
// chain has nothing to read and fails before any request is sent.
func credentialFreeBlock(t *testing.T) map[string]string {
	t.Helper()
	dir := t.TempDir()
	return map[string]string{
		"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "absent-credentials"),
		"AWS_CONFIG_FILE":             filepath.Join(dir, "absent-config"),
	}
}
