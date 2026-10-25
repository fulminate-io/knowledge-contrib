// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/gobwas/ws"
)

// State names what discovery or the dial found about the user's Chrome.
type State string

// The states a [*DiscoverError] carries.
const (
	// NoUserDataDir: the Chrome user data directory does not exist.
	NoUserDataDir State = "no-user-data-dir"
	// ChromeNotRunning: no live Chrome process holds the profile.
	ChromeNotRunning State = "chrome-not-running"
	// DebuggingOff: a live Chrome holds the profile and serves no DevTools
	// endpoint.
	DebuggingOff State = "remote-debugging-off"
	// AllowDeclined: Chrome answered the connection with HTTP 403.
	AllowDeclined State = "allow-declined"
	// BadActivePort: DevToolsActivePort exists and does not hold a port line
	// and a browser path line.
	BadActivePort State = "bad-devtools-active-port"
)

// DiscoverError is a discovery or dial outcome that stops a collect. Its
// message names the step the user takes.
type DiscoverError struct {
	State State
	Dir   string
	// Detail is what was read, for the message; it never holds the endpoint
	// path.
	Detail string
}

func (e *DiscoverError) Error() string {
	switch e.State {
	case NoUserDataDir:
		return fmt.Sprintf("the Chrome user data directory %s does not exist: set %s to the user data directory of the Chrome you are signed in with, or unset it to use Chrome's default (%s)", e.Dir, UserDataDirEnv, e.Detail)
	case ChromeNotRunning:
		return fmt.Sprintf("Chrome is not running on the profile at %s: start Chrome, sign in, turn on remote debugging at chrome://inspect/#remote-debugging, and collect again (%s)", e.Dir, e.Detail)
	case DebuggingOff:
		return fmt.Sprintf("Chrome is running on the profile at %s with remote debugging off: open chrome://inspect/#remote-debugging in that Chrome, turn on remote debugging, and collect again (%s)", e.Dir, e.Detail)
	case AllowDeclined:
		return fmt.Sprintf("Chrome refused the connection to the profile at %s: the Allow prompt was declined; collect again and click Allow when Chrome asks (%s)", e.Dir, e.Detail)
	}
	return fmt.Sprintf("Chrome's DevToolsActivePort in %s could not be read: quit and restart Chrome with remote debugging on, then collect again (%s)", e.Dir, e.Detail)
}

// DialError is a failed websocket dial to the advertised DevTools port.
type DialError struct {
	Port int
	Err  error
}

func (e *DialError) Error() string {
	return fmt.Sprintf("dialing Chrome's DevTools port %d: %v", e.Port, e.Err)
}

func (e *DialError) Unwrap() error { return e.Err }

// Endpoint is the browser endpoint DevToolsActivePort advertises.
type Endpoint struct {
	Port int
	// URL is ws://127.0.0.1:<port>/devtools/browser/<id>.
	URL string
}

// DefaultUserDataDir is Chrome's default user data directory under home, on
// darwin and linux.
func DefaultUserDataDir(home string) (string, error) {
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "Google", "Chrome"), nil
	case "linux":
		return filepath.Join(home, ".config", "google-chrome"), nil
	}
	return "", fmt.Errorf("no default Chrome user data directory is known for %s: set %s", runtime.GOOS, UserDataDirEnv)
}

// lockHolder reads dir/SingletonLock, a symlink to "<host>-<pid>", and
// reports the pid and whether that process is alive. ok is false when there
// is no lock.
func lockHolder(dir string) (pid int, alive, ok bool) {
	target, err := os.Readlink(filepath.Join(dir, "SingletonLock"))
	if err != nil {
		return 0, false, false
	}
	i := strings.LastIndex(target, "-")
	if i < 0 {
		return 0, false, true
	}
	pid, err = strconv.Atoi(target[i+1:])
	if err != nil {
		return 0, false, true
	}
	if pid <= 0 {
		return 0, false, true
	}
	err = syscall.Kill(pid, 0)
	return pid, err == nil || errors.Is(err, syscall.EPERM), true
}

// notListening is the state of a profile whose DevTools endpoint does not
// answer: debugging off when a live Chrome holds it, otherwise not running.
func notListening(dir, detail string) *DiscoverError {
	if pid, alive, ok := lockHolder(dir); ok && alive {
		return &DiscoverError{State: DebuggingOff, Dir: dir, Detail: fmt.Sprintf("Chrome pid %d holds the profile; %s", pid, detail)}
	}
	return &DiscoverError{State: ChromeNotRunning, Dir: dir, Detail: "no live Chrome holds the profile; " + detail}
}

// Discover reads dir/DevToolsActivePort. The file is an advertisement that
// survives Chrome's exit; the dial in [Attach] is what proves it current, and
// [ClassifyDial] names a refused one.
func Discover(dir string) (Endpoint, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return Endpoint{}, &DiscoverError{State: NoUserDataDir, Dir: dir, Detail: err.Error()}
	}
	if !st.IsDir() {
		return Endpoint{}, &DiscoverError{State: NoUserDataDir, Dir: dir, Detail: "not a directory"}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
	if err != nil {
		return Endpoint{}, notListening(dir, "no DevToolsActivePort file")
	}
	lines := strings.Fields(string(raw))
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "/devtools/browser/") {
		return Endpoint{}, &DiscoverError{State: BadActivePort, Dir: dir, Detail: fmt.Sprintf("%d fields where a port and a browser path were expected", len(lines))}
	}
	port, err := strconv.Atoi(lines[0])
	if err != nil || port <= 0 || port > 65535 {
		return Endpoint{}, &DiscoverError{State: BadActivePort, Dir: dir, Detail: fmt.Sprintf("the port field %q is not a port", lines[0])}
	}
	return Endpoint{Port: port, URL: "ws://127.0.0.1:" + lines[0] + lines[1]}, nil
}

// ClassifyDial names the state behind a failed [Attach] dial to the profile
// at dir: a refused connection is debugging off or Chrome not running, and an
// HTTP 403 is the Allow prompt declined. Any other error is returned as is.
func ClassifyDial(dir string, err error) error {
	var de *DialError
	if !errors.As(err, &de) {
		return err
	}
	var status ws.StatusError
	if errors.As(err, &status) && int(status) == 403 {
		return &DiscoverError{State: AllowDeclined, Dir: dir, Detail: fmt.Sprintf("port %d answered HTTP 403", de.Port)}
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return notListening(dir, fmt.Sprintf("DevToolsActivePort names port %d and nothing accepts on it", de.Port))
	}
	return err
}
