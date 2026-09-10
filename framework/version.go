// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"fmt"
	"io"
	"os"
)

// version.go — the build stamp every collector binary reports, and the ONE
// argument the framework interprets on its own behalf.
//
// IT LIVES HERE RATHER THAN IN EIGHT MAINS. Every collector's main is one call
// to [ServeStdio]; putting the switch in the entry point gives every binary the
// same answer to --version without a module touching argv, which is what keeps
// the eight of them from drifting into eight spellings.

// version is the collector binary's version, injected at build time via:
//
//	go build -ldflags "-X <module path>/cmd/collectors/framework.version=v1.2.3"
//
// THE IMPORT PATH IN THAT FLAG MUST NAME THE MODULE BEING BUILT, and a wrong one
// is SILENT: -X whose symbol or path does not resolve links clean and leaves this
// default in place, so a build that stamps nothing is indistinguishable from a
// correct one until the binary is run. The only honest check is running it and
// reading what it prints.
var version = "dev"

// versionArg is the single argument this package interprets. It is matched
// EXACTLY and only in FIRST position, mirroring cmd/knowledge's own pre-parse
// switch: a config entry may carry args of its own, the daemon passes them to
// the collector verbatim, and a looser match would eat one of them and serve
// nothing.
const versionArg = "--version"

// versionRequested reports whether an argv asks for the version banner. args is
// a whole argv, os.Args-shaped, so index 0 is the program name.
func versionRequested(args []string) bool {
	return len(args) > 1 && args[1] == versionArg
}

// printVersion writes the version banner: the binary family, the tool this
// collector serves, and the stamp.
//
// THE BANNER CARRIES MORE THAN THE BARE STAMP, and that is a property the
// install script's up-to-date check depends on rather than decoration. That
// check reads the LAST FIELD of the FIRST LINE and compares it exactly; a
// one-token banner would let any extraction heuristic pass, which is how a green
// suite hides a broken comparison.
func printVersion(w io.Writer, tool string) {
	fmt.Fprintf(w, "knowledge-collector %s %s\n", tool, version)
}

// serveVersion answers a --version argv, if that is what this argv is, and
// reports whether it answered.
//
// IT WRITES TO STDOUT AND THEN THE ENTRY POINT RETURNS WITHOUT SERVING. Stdout
// is the JSON-RPC protocol stream once a session starts, so the banner is only
// ever written on the path where no session will exist.
func serveVersion[P any](c Collector[P]) bool {
	if !versionRequested(os.Args) {
		return false
	}
	printVersion(os.Stdout, versionBannerTool(c))
	return true
}

// versionBannerTool resolves the tool name for the banner without building the
// server, so a collector whose schemas do not compile still reports its version.
func versionBannerTool[P any](c Collector[P]) string {
	if c == nil {
		return DefaultToolName
	}
	return defaultedToolName(c.Tool().Name)
}

// defaultedToolName applies the framework's one naming default. It is the single
// site both the served tool and the version banner resolve through, so the two
// cannot disagree about what this collector is called.
func defaultedToolName(name string) string {
	if name == "" {
		return DefaultToolName
	}
	return name
}
