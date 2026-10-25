// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// chromewatchdog_test.go — stop returns once every process of a scratch Chrome
// has exited, and a scratch Chrome's browser dies with the test process that
// started it, whatever ends that process.

const hangEnv = "CDPATTACH_ROWS_HANG_PIDFILE"

// childHangGuard is the -test.timeout of the children of
// TestAScratchChromeDiesWithTheTestProcess and
// TestAScratchChromeThatExitsAtStartFailsItsRowWithItsExit: chromeStartGuard,
// the bound on a scratch Chrome's start, plus waitFence.
const childHangGuard = chromeStartGuard + waitFence

// TestHelperProcessStartsChromeAndHangs is the child of
// TestAScratchChromeDiesWithTheTestProcess: it starts a scratch Chrome, writes
// Chrome's pid to a temporary file it renames to the file the environment
// names, and blocks until it is killed. It skips when that variable is unset.
func TestHelperProcessStartsChromeAndHangs(t *testing.T) {
	pidFile := os.Getenv(hangEnv)
	if pidFile == "" {
		t.Skip("the child half of TestAScratchChromeDiesWithTheTestProcess")
	}
	c := startChrome(t, newFixture(t), true)
	if err := os.WriteFile(pidFile+".tmp", []byte(strconv.Itoa(chromePid(t, c.dir))), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(pidFile+".tmp", pidFile); err != nil {
		t.Fatal(err)
	}
	select {}
}

// TestAScratchChromeDiesWithTheTestProcess runs this test binary as a child
// that starts a scratch Chrome, waits until the child has recorded Chrome's
// pid, kills the child with SIGKILL, and asserts that Chrome is gone within
// waitFence, so no row leaves a browser behind. A child that exits before it
// records the pid fails the row with the child's log.
func TestAScratchChromeDiesWithTheTestProcess(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "chrome.pid")
	logPath := filepath.Join(dir, "child.log")
	logFile, err := os.Create(logPath) //nolint:gosec // a t.TempDir() path.
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestHelperProcessStartsChromeAndHangs$", "-test.timeout="+childHangGuard.String()) //nolint:gosec // this test binary.
	child.Env = append(os.Environ(), hangEnv+"="+pidFile)
	child.Stdout, child.Stderr = logFile, logFile
	if err := child.Start(); err != nil {
		t.Fatalf("starting the child: %v", err)
	}
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	raw, err := awaitPidFile(pidFile, exited)
	if err != nil {
		_ = logFile.Close()
		childLog, _ := os.ReadFile(logPath) //nolint:gosec // a t.TempDir() path.
		t.Fatalf("%v:\n%s", err, childLog)
	}
	_ = child.Process.Kill()
	<-exited
	_ = logFile.Close()
	pid, err := strconv.Atoi(string(raw))
	if err != nil {
		t.Fatalf("the child recorded %q, not a pid", raw)
	}
	deadline := time.Now().Add(waitFence)
	for syscall.Kill(pid, 0) == nil && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if syscall.Kill(pid, 0) == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Fatalf("the scratch Chrome (pid %d) was still running %s after its test process died", pid, waitFence)
	}
}

// TestAScratchChromeThatExitsAtStartFailsItsRowWithItsExit runs this test
// binary as a child whose scratch Chrome is the program false, which exits at
// once, and asserts that the child's row failed naming that exit rather than
// a start that ran out of time.
func TestAScratchChromeThatExitsAtStartFailsItsRowWithItsExit(t *testing.T) {
	child := exec.Command(os.Args[0], "-test.run=^TestHelperProcessStartsChromeAndHangs$", "-test.timeout="+childHangGuard.String()) //nolint:gosec // this test binary.
	child.Env = append(os.Environ(), hangEnv+"="+filepath.Join(t.TempDir(), "chrome.pid"), "CHROME_BIN=false")
	out, err := child.CombinedOutput()
	if err == nil {
		t.Fatalf("the child passed with a Chrome that exits at once:\n%s", out)
	}
	if !strings.Contains(string(out), "exited before it came up") {
		t.Errorf("the child's row failed without naming the Chrome's exit:\n%s", out)
	}
}

// awaitPidFile polls pidFile every 20 ms and returns its content once it exists.
// When exited delivers the child's exit first, it returns an error carrying
// the exit status, and the child has been reaped.
func awaitPidFile(pidFile string, exited <-chan error) ([]byte, error) {
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case runErr := <-exited:
			return nil, fmt.Errorf("the child exited before it recorded its Chrome's pid: %w", runErr)
		case <-tick.C:
			if raw, err := os.ReadFile(pidFile); err == nil { //nolint:gosec // a t.TempDir() path.
				return raw, nil
			}
		}
	}
}

// TestStopReturnsOnceEveryProcessOfTheScratchChromeHasExited starts a scratch
// Chrome, lists the processes descended from its browser process, stops it,
// and asserts that none of them is still running when stop returns.
func TestStopReturnsOnceEveryProcessOfTheScratchChromeHasExited(t *testing.T) {
	c := startChrome(t, newFixture(t), true)
	browser := chromePid(t, c.dir)
	procs := descendants(t, browser)
	if len(procs) == 0 {
		t.Fatalf("the scratch Chrome's browser process (pid %d) has no child process to observe", browser)
	}
	c.stop()
	var running []int
	for _, pid := range append(procs, browser) {
		if syscall.Kill(pid, 0) == nil {
			running = append(running, pid)
		}
	}
	if len(running) > 0 {
		t.Errorf("stop returned while %d of the scratch Chrome's %d processes were still running: %v (browser pid %d)", len(running), len(procs)+1, running, browser)
	}
}

// TestAwaitGroupGoneEndsOnAnEmptyGroupAndKillsOneThatOutlivesTheGuard starts
// two process groups whose leaders exit at once and leave one member running:
// a member that exits after 0.3 s, for which awaitGroupGone returns nil, and a
// member that outlives the guard, for which it returns an error and leaves the
// group with no member.
func TestAwaitGroupGoneEndsOnAnEmptyGroupAndKillsOneThatOutlivesTheGuard(t *testing.T) {
	short := orphanGroup(t, "sleep 0.3")
	if err := awaitGroupGone(short, waitFence); err != nil {
		t.Errorf("awaitGroupGone on a group whose member exits after 0.3 s = %v, want nil", err)
	}
	stuck := orphanGroup(t, "sleep 30")
	err := awaitGroupGone(stuck, 200*time.Millisecond)
	if err == nil {
		t.Errorf("awaitGroupGone on a group whose member runs for 30 s with a 200ms guard = nil, want an error")
	}
	if !errors.Is(syscall.Kill(-stuck, 0), syscall.ESRCH) {
		t.Errorf("process group %d still has a member after awaitGroupGone returned %v", stuck, err)
	}
}

// orphanGroup runs /bin/sh as the leader of a new process group, which starts
// script in the background and exits, and returns the group's id once the
// leader has been reaped.
func orphanGroup(t *testing.T, script string) int {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", script+" & exit 0")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Run(); err != nil {
		t.Fatalf("starting a process group running %q: %v", script, err)
	}
	return cmd.Process.Pid
}

// chromePid returns the pid of the browser process holding the user data
// directory dir, read from the SingletonLock link, whose target ends in
// "-<pid>".
func chromePid(t *testing.T, dir string) int {
	t.Helper()
	target, err := os.Readlink(filepath.Join(dir, "SingletonLock"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(target[strings.LastIndex(target, "-")+1:])
	if err != nil {
		t.Fatalf("the SingletonLock target %q does not end in a pid: %v", target, err)
	}
	return pid
}

// descendants returns the pids of every running process descended from pid,
// read from ps.
func descendants(t *testing.T, pid int) []int {
	t.Helper()
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		t.Fatalf("listing processes with ps: %v", err)
	}
	children := map[int][]int{}
	for line := range strings.Lines(string(out)) {
		f := strings.Fields(line)
		if len(f) != 2 {
			t.Fatalf("ps printed %q, want a pid and a ppid", line)
		}
		p, err := strconv.Atoi(f[0])
		if err != nil {
			t.Fatalf("ps printed %q, whose pid does not parse: %v", line, err)
		}
		pp, err := strconv.Atoi(f[1])
		if err != nil {
			t.Fatalf("ps printed %q, whose ppid does not parse: %v", line, err)
		}
		children[pp] = append(children[pp], p)
	}
	var found []int
	for queue := children[pid]; len(queue) > 0; queue = queue[1:] {
		found = append(found, queue[0])
		queue = append(queue, children[queue[0]]...)
	}
	return found
}
