// SPDX-License-Identifier: Apache-2.0

// Package frameworktest carries the TEST-SIDE gates every collector module
// owes, so a collector inherits them instead of re-authoring them: the
// goroutine-leak guard, the streamable-HTTP session drain that guard needs, and
// the test-cache fence walker.
//
// IT IS A SEPARATE PACKAGE FROM framework ON PURPOSE. goleak is a test
// dependency; a helper importing it from the framework package itself would
// link goleak into every collector BINARY. Nothing here is reachable from a
// collector's serving path.
package frameworktest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"
)

// VerifyNoGoroutineLeaks is the goleak gate a collector package installs from
// its own TestMain:
//
//	func TestMain(m *testing.M) { frameworktest.VerifyNoGoroutineLeaks(m) }
//
// THE PER-TEST SYMPTOM IS NOTHING AT ALL, which is why this has to be a
// package-level gate rather than an assertion someone remembers to write: a
// leaked goroutine does not fail the test that leaked it, it fails whatever runs
// after it, or nothing at all until the leak becomes a resource exhaustion in a
// long-lived collector process.
//
// The allowlist is deliberately EMPTY, and a collector that needs an entry does
// not add it here — it writes its own TestMain with the entry, naming the
// goroutine and saying why its lifetime legitimately exceeds the test that
// started it. A shared allowlist would spend one collector's excuse on all nine.
//
// A collector serving over streamable HTTP in its tests must drain the server's
// sessions before the gate runs; see [DrainServerSessions] for what leaks
// otherwise.
func VerifyNoGoroutineLeaks(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// DrainServerSessions closes every session an mcp.Server still holds.
//
// IT IS NOT OPTIONAL FOR AN HTTP TEST, and the reason is a leak neither obvious
// teardown reaches: after a streamable-HTTP test closes its client session AND
// closes the httptest server, the SDK's own streamableServerConn.Read goroutine
// is still running, and an empty goleak allowlist reports it. Draining the
// server's sessions is what ends it. Call this before the test returns, on
// every test that serves over HTTP.
func DrainServerSessions(srv *mcp.Server) {
	if srv == nil {
		return
	}
	for ss := range srv.Sessions() {
		_ = ss.Close()
	}
}

// FenceTestCacheOnLinkedTree opens every file under a testdata symlink and
// returns how many it opened, failing the test when it opened fewer than
// minFiles.
//
// WHAT IT IS FOR. `go test` keys a package's stored result on the files a run
// opened, but the go tool DROPS any opened name that does not resolve inside the
// tested package's own module root. A collector test whose subject lives outside
// its module — the client's checked-in contract schemas, a source tree it builds
// — is therefore cacheable against a subject it never read, and the symptom is a
// stored PASS on a broken subject rather than a failure. A symlink under the
// reading module's own testdata is the remedy: the opened NAME stays inside the
// module while os.Stat follows the link to the real file's size and modification
// time, so an edit on the far side invalidates the key.
//
// TWO PLACEMENT RULES A CALLER MUST KEEP, both measured rather than reasoned:
//
//   - CALL IT FROM EACH TEST, never from TestMain. The go tool installs the hook
//     that records opened files when it parses -test.testlogfile, which happens
//     inside m.Run, so a fence in TestMain opens its files outside the recording
//     window and reaches no cache key at all — while looking, in the diff and in
//     a green run, exactly like a fence that works.
//   - WRAP IT IN A LOCAL FUNCTION NAMED fenceTestCache..., and call the wrapper.
//     The repository's fence-placement census and its corpus check both key on
//     that name prefix at the CALL SITE, and neither can see a prefix that lives
//     on the far side of a package qualifier. Go has no lowercase exported name,
//     so the prefix travels on the wrapper.
//
// THE FLOOR IS A KNOWN POSITIVE. A fence that opened nothing is
// indistinguishable from no fence at all and fails exactly as silently, so
// minFiles must be a real count of what the link serves — a dangling link, a
// link materialized as a text stub by a checkout without symlink support, and a
// link pointing at an empty directory all read as zero.
func FenceTestCacheOnLinkedTree(t *testing.T, link string, minFiles int) int {
	t.Helper()
	opened, err := openLinkedTree(link)
	if err != nil {
		t.Fatalf("test-cache fence: %v", err)
	}
	if opened < minFiles {
		t.Fatalf("test-cache fence: %s served %d files, fewer than the %d this fence requires; "+
			"a link that resolves to nothing fences nothing, and every test in this package would then be "+
			"cacheable against a subject it never read", link, opened, minFiles)
	}
	return opened
}

// openLinkedTree recurses with os.ReadDir, NOT filepath.WalkDir. WalkDir lstats
// its root: handed a symlink it yields one non-directory entry and stops, so the
// walk would silently open nothing. os.Open — which ReadDir uses — follows the
// link, and every name built here stays under the caller's own testdata, which
// is what keeps the opens inside the module where the cache key can see them.
func openLinkedTree(dir string) (int, error) {
	opened := 0
	if err := walkLinkedTree(dir, &opened); err != nil {
		return opened, err
	}
	return opened, nil
}

func walkLinkedTree(dir string, opened *int) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		name := entry.Name()
		path := filepath.Join(dir, name)
		if entry.IsDir() {
			// A nested testdata tree holds corpora in the tens of thousands of
			// files and decides nothing about the subject; .git holds neither.
			if name == "testdata" || name == ".git" {
				continue
			}
			if err := walkLinkedTree(path, opened); err != nil {
				return err
			}
			continue
		}
		if !isFencedName(name) {
			continue
		}
		f, err := os.Open(path) //nolint:gosec // a path built from the caller's own testdata link
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
		_ = f.Close()
		*opened++
	}
	return nil
}

// isFencedName reports whether a file decides what the linked subject is. Go
// source and module files decide what a build produces; a .json file is what the
// contract schemas are.
func isFencedName(name string) bool {
	switch {
	case strings.HasSuffix(name, ".go"),
		strings.HasSuffix(name, ".json"),
		name == "go.mod",
		name == "go.sum":
		return true
	default:
		return false
	}
}
