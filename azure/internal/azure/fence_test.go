// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"os"
	"testing"
)

// fence_test.go — the test-cache fence for the one checked-in file this
// package's tests read.
//
// WHAT IT IS FOR. `go test` keys a package's stored result on the files a run
// opened. Every other test here reads hand-built values and needs no fence; the
// README documentation test reads README.md, which lives at the module root
// rather than in this package, and without an explicit open the stored result
// would survive an edit to the README — reporting a PASS for a document the run
// never looked at.
//
// TWO PLACEMENT RULES, both of which this call site keeps:
//
//   - IT IS CALLED FROM THE TEST, never from TestMain. The go tool installs the
//     hook that records opened files while parsing its own test flags, which
//     happens inside m.Run, so a fence in TestMain opens its file outside the
//     recording window and reaches no cache key at all — while looking, in the
//     diff and in a green run, exactly like a fence that works.
//   - IT IS NAMED fenceTestCache..., because the repository's fence-placement
//     census keys on that prefix at the call site.
//
// NO SYMLINK IS NEEDED HERE, unlike a fence over another module's tree: the go
// tool drops an opened name that does not resolve inside the tested package's
// own MODULE root, and README.md is inside this module's root.
//
// THE PATH IS A LITERAL CONSTANT rather than a value a helper computes, and that
// is not a style preference: the workspace's cache-blindness census proves a
// read in-module only when its path resolves to a literal, and a helper's return
// lands in the unprovable residue instead. A fence whose own read the census
// cannot see is exactly the shape this file exists to avoid.

// readmeRelPath is the module README, relative to this package's directory,
// which is where `go test` runs.
const readmeRelPath = "../../README.md"

// fenceTestCacheOnReadme opens the module's README so this package's stored
// test result is keyed on it.
func fenceTestCacheOnReadme(t *testing.T) {
	t.Helper()
	f, err := os.Open(readmeRelPath) //nolint:gosec // a checked-in path literal, relative to this package
	if err != nil {
		t.Fatalf("test-cache fence: opening %s: %v", readmeRelPath, err)
	}
	defer func() { _ = f.Close() }()

	// A FLOOR, on the same reasoning the framework's own fence gives: a fence
	// that opened an empty file is indistinguishable from no fence and fails
	// exactly as silently.
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("test-cache fence: stat %s: %v", readmeRelPath, err)
	}
	const minBytes = 1024
	if info.Size() < minBytes {
		t.Fatalf("test-cache fence: %s is %d bytes, fewer than the %d this fence requires; "+
			"a file that resolves to nothing fences nothing", readmeRelPath, info.Size(), minBytes)
	}
}
