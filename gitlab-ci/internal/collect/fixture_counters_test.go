// SPDX-License-Identifier: Apache-2.0

package collect_test

import "fmt"

// fixture_counters_test.go — WHAT EACH PAGINATED READ WAS ASKED FOR, recorded by
// the recorded provider so a test can assert on the REQUEST and not only on the
// nodes that came back.
//
// THREE THINGS ARE RECORDED, and each catches a different way a paging loop can
// be wrong while its output looks right:
//
//   - HOW MANY REQUESTS. A full last page costs ONE request under the provider's
//     own next-page marker and TWO under a reader that stops on a short page, and
//     both produce identical nodes. Without the count the two are the same test.
//   - WHAT PAGE SIZE. A collector that asked for seven items a page would return
//     exactly the same nodes over more round trips, so the size never reaches the
//     graph and no node count can see it. It reaches the provider, which is where
//     this reads it.
//   - WHETHER A PAGE WAS SERVED TWICE. This one is a guard on the INSTRUMENT
//     rather than on the collector: a loop that re-requests a page it has already
//     read never terminates, and a fake that cheerfully served page one forever
//     turned a mutation into a TEN-MINUTE HANG instead of a failed assertion. A
//     hang is not a red. So the repeat is refused, the enumeration reports the
//     refusal, and the row fails in milliseconds saying what happened.

// readLog is what one paginated read was asked for.
type readLog struct {
	// requests is how many pages were asked for.
	requests int
	// perPage is the page size the collector asked for, which must be the
	// provider's own maximum for every read that pages to exhaustion.
	perPage int64
	// served is the set of page numbers already answered, page 0 and page 1
	// normalised to the same page because the provider treats them as one.
	served map[int64]bool
}

// record logs one request and refuses a page already served.
//
// THE REFUSAL IS THE INSTRUMENT'S OWN ASSERTION. A collector that re-requests a
// page it has read is in a loop that cannot end; answering it would hide the
// defect behind a timeout, and a timeout tells a reader nothing about which loop
// or which condition. The error names both.
func (f *fakeAPI) record(read string, page, perPage int64) error {
	if f.reads == nil {
		f.reads = map[string]*readLog{}
	}
	log, known := f.reads[read]
	if !known {
		log = &readLog{served: map[int64]bool{}}
		f.reads[read] = log
	}
	log.requests++
	log.perPage = perPage

	normalised := max(page, 1)
	if log.served[normalised] {
		return fmt.Errorf(
			"the %s read asked for page %d a second time. A loop that re-requests a page it has "+
				"already read does not terminate: it is reading the item count rather than the "+
				"provider's own next-page marker, and the marker on the last page is zero",
			read, normalised)
	}
	log.served[normalised] = true
	return nil
}

// newWalk clears the log, and every run helper calls it before driving an
// enumeration.
//
// THE REPEAT REFUSAL IS SCOPED TO ONE WALK, and it has to be. Each walk builds
// its own project discovery, so a second walk over the same recorded provider
// legitimately lists the group's projects again — that is one request in each of
// two walks, not a loop. What is never legitimate is the same page twice inside
// ONE walk.
func (f *fakeAPI) newWalk() { f.reads = nil }

// asked is the log for one read, or an empty one when the read never happened,
// so an assertion on a read that was never made says zero rather than panicking.
func (f *fakeAPI) asked(read string) readLog {
	if log, known := f.reads[read]; known {
		return *log
	}
	return readLog{served: map[int64]bool{}}
}

// The read names the fixture logs under. They are written out rather than derived
// so a row in the boundary table names the same read the fake does, and a typo in
// either is a zero rather than a silent match.
const (
	readGroupProjects  = "group projects"
	readSubgroups      = "subgroups"
	readEnvironments   = "environments"
	readGroupRunners   = "group runners"
	readProjectRunners = "project runners"
	readGroupVariables = "group variables"
	readProjectVars    = "project variables"
	readJobs           = "jobs under a pipeline run"
)

// scoped names a per-project read, so two projects' reads are logged apart.
func scoped(read string, key any) string { return fmt.Sprintf("%s:%v", read, key) }

// projectKey is the numeric project id a per-project call was made with.
func projectKey(pid any) int64 {
	if id, ok := pid.(int64); ok {
		return id
	}
	return 0
}

// groupKey is the group path a group-scoped call was made with.
func groupKey(gid any) string {
	if path, ok := gid.(string); ok {
		return path
	}
	return ""
}
