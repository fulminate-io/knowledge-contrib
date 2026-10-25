// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/gobwas/ws"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// harness_test.go — a scratch headless Chrome on its own user data directory,
// a TLS fixture it reaches as testHost through host-resolver rules, and a
// recording proxy for the DevTools connection.

func TestMain(m *testing.M) { frameworktest.VerifyNoGoroutineLeaks(m) }

const (
	testHost  = "www.cdpattach.test"
	waitFence = 20 * time.Second
	// chromeStartGuard bounds the wait for a scratch Chrome to come up, which
	// otherwise ends when Chrome is up or has exited.
	chromeStartGuard = 2 * time.Minute
)

// chromeBinary locates Chrome: CHROME_BIN first, then the per-OS locations.
func chromeBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_BIN"); p != "" {
		return p
	}
	if runtime.GOOS == "darwin" {
		p := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Fatalf("no Chrome on this machine: these rows drive a real Chrome; set CHROME_BIN to its executable")
	return ""
}

// fixture is the TLS site the scratch Chrome reaches as testHost. It records
// every request.
type fixture struct {
	srv *httptest.Server
	mu  sync.Mutex
	got []recorded
}

type recorded struct {
	Method, Path string
	Header       http.Header
	Body         []byte
}

// binaryBody holds every byte value, embedded newlines and invalid UTF-8.
var binaryBody = func() []byte {
	b := make([]byte, 0, 600)
	for i := range 256 {
		b = append(b, byte(i))
	}
	b = append(b, []byte("\n12:T5,ab\ncd\r\n\xff\xfe\xc3\x28")...)
	for i := 255; i >= 0; i-- {
		b = append(b, byte(i))
	}
	return b
}()

const sessionCookieValue = "ajax:7461626c65-cookie"

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.got = append(f.got, recorded{Method: r.Method, Path: r.URL.RequestURI(), Header: r.Header.Clone(), Body: body})
	f.mu.Unlock()
	switch r.URL.Path {
	case "/login":
		w.Header().Add("Set-Cookie", `JSESSIONID="`+sessionCookieValue+`"; Path=/; Secure`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Signed in</title><p>signed in</p>`)
	case "/page":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Page</title><p id="p">page</p>`)
	case "/later":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Later</title><script>setTimeout(() => { const d = document.createElement('div'); d.id = 'ready'; document.body.appendChild(d); }, 50)</script>`)
	case "/hop":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Hop</title><script>location.replace('/page')</script>`)
	case "/early":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><title>Early</title><script>window.__seenByPage = window.__initMark || 'absent'</script>`)
	case "/binary":
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(binaryBody)
	case "/echo":
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"method": r.Method, "csrf": r.Header.Get("Csrf-Token"), "x": r.Header.Get("X-Probe"), "body": string(body)}); err != nil {
			panic(err)
		}
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) requests(path string) []recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recorded
	for _, r := range f.got {
		if r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

func (f *fixture) url(path string) string { return "https://" + testHost + path }

// chrome is a scratch Chrome. t is the test that started it, which a failed
// stop reports to; log is the file Chrome's output is written to; exited is
// closed once the watchdog has exited and been reaped; stopped is set once stop
// has run.
type chrome struct {
	t       *testing.T
	dir     string
	log     string
	port    int
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	exited  chan struct{}
	stopped bool
}

// startChrome launches a scratch Chrome on a fresh directory, with remote
// debugging on a picked port when debugging is set, and waits for it.
func startChrome(t *testing.T, f *fixture, debugging bool) *chrome {
	t.Helper()
	dir := t.TempDir()
	return startChromeIn(t, f, dir, debugging)
}

func startChromeIn(t *testing.T, f *fixture, dir string, debugging bool, extra ...string) *chrome {
	t.Helper()
	args := []string{
		"--headless=new", "--no-first-run", "--no-default-browser-check",
		"--use-mock-keychain", "--password-store=basic", "--ignore-certificate-errors",
		"--user-data-dir=" + dir,
	}
	if f != nil {
		args = append(args, "--host-resolver-rules=MAP "+testHost+" "+strings.TrimPrefix(f.srv.URL, "https://")+", MAP * ~NOTFOUND")
	}
	if debugging {
		args = append(args, "--remote-debugging-port=0")
	}
	args = append(args, extra...)
	args = append(args, "about:blank")
	logPath := filepath.Join(t.TempDir(), "chrome.log")
	logFile, err := os.Create(logPath) //nolint:gosec // a t.TempDir() path.
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("/bin/sh", append([]string{"-c", chromeWatchdog, chromeBinary(t)}, args...)...) //nolint:gosec // the test's own arguments.
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the scratch Chrome: %v", err)
	}
	c := &chrome{t: t, dir: dir, log: logPath, cmd: cmd, stdin: stdin, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(c.exited) }()
	t.Cleanup(func() { c.stop(); _ = logFile.Close() })
	c.awaitStart(t, func() bool {
		if !debugging {
			_, err := os.Readlink(filepath.Join(dir, "SingletonLock"))
			return err == nil
		}
		raw, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		if err != nil {
			return false
		}
		lines := strings.Fields(string(raw))
		if len(lines) != 2 {
			return false
		}
		c.port, _ = strconv.Atoi(lines[0])
		return true
	})
	return c
}

// awaitStart polls ready every 20 ms until it reports true. It fails t, with
// Chrome's log, when the watchdog exits first, which it does when Chrome
// exits, or when chromeStartGuard passes first.
func (c *chrome) awaitStart(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(chromeStartGuard)
	for !ready() {
		select {
		case <-c.exited:
			tail, _ := os.ReadFile(c.log) //nolint:gosec // a t.TempDir() path.
			t.Fatalf("the scratch Chrome (watchdog pid %d) exited before it came up (%v); its log:\n%s", c.cmd.Process.Pid, c.cmd.ProcessState, tail)
		default:
		}
		if time.Now().After(deadline) {
			tail, _ := os.ReadFile(c.log) //nolint:gosec // a t.TempDir() path.
			t.Fatalf("the scratch Chrome (watchdog pid %d) did not come up within %s; its log:\n%s", c.cmd.Process.Pid, chromeStartGuard, tail)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// chromeWatchdog is the /bin/sh script the scratch Chrome runs under, as the
// leader of its own process group. It starts Chrome and waits for it, and so
// exits when Chrome exits. A background reader reads the script's stdin and,
// at end of file, sends SIGTERM to the whole process group; stdin reaches end
// of file when stop closes the pipe, when the watchdog has exited and been
// reaped, or when the test process exits by any path, a timeout panic
// included.
const chromeWatchdog = `exec 3<&0; "$0" "$@" 3<&- & pid=$!; (read -r _ <&3; kill -s TERM -- "-$$" 2>/dev/null) & wait "$pid"`

// stop closes the watchdog's stdin, waits until the watchdog has exited, and
// then waits until no process is left in the watchdog's process group, which
// holds every process Chrome started. A group still populated after waitFence
// is killed and reported as a failure of c.t.
func (c *chrome) stop() {
	if c.stopped {
		return
	}
	c.stopped = true
	_ = c.stdin.Close()
	<-c.exited
	if err := awaitGroupGone(c.cmd.Process.Pid, waitFence); err != nil {
		c.t.Errorf("stopping the scratch Chrome on %s: %v", c.dir, err)
	}
}

// awaitGroupGone waits until process group pgid has no member. When members
// remain after guard, it sends the group SIGKILL, waits up to guard again, and
// returns an error naming the group.
func awaitGroupGone(pgid int, guard time.Duration) error {
	if groupGone(pgid, guard) {
		return nil
	}
	_ = syscall.Kill(-pgid, syscall.SIGKILL)
	if groupGone(pgid, guard) {
		return fmt.Errorf("process group %d still had members %s after its leader exited, and was sent SIGKILL", pgid, guard)
	}
	return fmt.Errorf("process group %d still had members %s after its leader exited and %s after SIGKILL", pgid, guard, guard)
}

// groupGone polls every 20 ms until signal 0 to process group pgid answers
// ESRCH, which it does once the group has no member, and reports whether that
// happened within guard.
func groupGone(pgid int, guard time.Duration) bool {
	deadline := time.Now().Add(guard)
	for !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH) {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
	return true
}

type target struct{ ID, Type, URL, Title string }

func (c *chrome) list(t *testing.T) []target {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/json/list", c.port)) //nolint:noctx // a test against a scratch Chrome.
	if err != nil {
		t.Fatalf("listing tabs: %v", err)
	}
	defer resp.Body.Close()
	var list []target
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decoding the tab list: %v", err)
	}
	return list
}

func (c *chrome) tabs(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, tab := range c.list(t) {
		if tab.Type == "page" {
			out[tab.ID] = tab.URL
		}
	}
	return out
}

// open opens a user tab at u and waits until its title is title.
func (c *chrome) open(t *testing.T, u, title string) string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, fmt.Sprintf("http://127.0.0.1:%d/json/new?%s", c.port, u), nil) //nolint:noctx // a test.
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("opening a user tab at %s: %v", u, err)
	}
	defer resp.Body.Close()
	var tab struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tab); err != nil {
		t.Fatalf("opening a user tab at %s answered %s: %v", u, resp.Status, err)
	}
	if tab.ID == "" {
		t.Fatalf("opening a user tab at %s answered %s with no tab id", u, resp.Status)
	}
	deadline := time.Now().Add(waitFence)
	for {
		for _, x := range c.list(t) {
			if x.ID == tab.ID && x.Title == title {
				return tab.ID
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the user tab at %s did not reach title %q within %s", u, title, waitFence)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// endpoint reads the scratch Chrome's DevToolsActivePort through Discover.
func (c *chrome) endpoint(t *testing.T) Endpoint {
	t.Helper()
	ep, err := Discover(c.dir)
	if err != nil {
		t.Fatalf("Discover on a running scratch Chrome: %v", err)
	}
	return ep
}

// recordingProxy forwards one TCP port to another and keeps every byte the
// client sent.
type recordingProxy struct {
	ln   net.Listener
	mu   sync.Mutex
	sent bytes.Buffer
	wg   sync.WaitGroup
}

func newRecordingProxy(t *testing.T, to int) *recordingProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &recordingProxy{ln: ln}
	p.wg.Go(func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", to))
			if err != nil {
				_ = client.Close()
				continue
			}
			p.wg.Go(func() {
				_, _ = io.Copy(io.MultiWriter(server, p), client)
				_ = server.Close()
			})
			p.wg.Go(func() {
				_, _ = io.Copy(client, server)
				_ = client.Close()
			})
		}
	})
	t.Cleanup(func() { _ = ln.Close(); p.wg.Wait() })
	return p
}

func (p *recordingProxy) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.sent.Write(b)
}

func (p *recordingProxy) port() int { return p.ln.Addr().(*net.TCPAddr).Port }

// clientText decodes the recorded client stream: the HTTP upgrade request,
// then every masked websocket frame, unmasked, concatenated.
func (p *recordingProxy) clientText(t *testing.T) string {
	t.Helper()
	p.mu.Lock()
	raw := bytes.Clone(p.sent.Bytes())
	p.mu.Unlock()
	_, frames, found := bytes.Cut(raw, []byte("\r\n\r\n"))
	if !found {
		t.Fatalf("the recorded client stream holds no upgrade request")
	}
	r := bufio.NewReader(bytes.NewReader(frames))
	var out bytes.Buffer
	for {
		h, err := ws.ReadHeader(r)
		if err != nil {
			break
		}
		payload := make([]byte, h.Length)
		if _, err := io.ReadFull(r, payload); err != nil {
			break
		}
		if h.Masked {
			ws.Cipher(payload, h.Mask, 0)
		}
		out.Write(payload)
		out.WriteByte('\n')
	}
	return out.String()
}

// approvalPort is the port Chrome's approval mode listens on.
const approvalPort = 9222

// startApprovalModeChrome launches a scratch Chrome with no remote-debugging
// flag and the chrome://inspect toggle's preference set in its Local State,
// which starts the DevTools server in approval mode on approvalPort. The row
// is skipped, naming the port, when another process already holds that port:
// the row never dials a Chrome it did not start.
func startApprovalModeChrome(t *testing.T) *chrome {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", approvalPort))
	if err != nil {
		t.Skipf("port %d is held by another process (%v); the approval-mode row runs only on a free port so it never reaches a Chrome it did not start", approvalPort, err)
	}
	_ = ln.Close()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Local State"), []byte(`{"devtools":{"remote_debugging":{"user-enabled":true}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c := startChromeIn(t, nil, dir, false)
	c.awaitStart(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(dir, "DevToolsActivePort"))
		lines := strings.Fields(string(raw))
		return err == nil && len(lines) == 2 && lines[0] == strconv.Itoa(approvalPort)
	})
	c.port = approvalPort
	return c
}
