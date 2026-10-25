// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// cdpattach_test.go — the helper's rows: discovery states, the attach and its
// Detach guards, in-place reads, the in-page fetch, tab release, and a call
// cancelled while its reply is withheld.

func attach(t *testing.T, c *chrome) *Browser {
	t.Helper()
	ep := c.endpoint(t)
	b, err := Attach(t.Context(), ep.URL, ep.Port)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	t.Cleanup(b.Close)
	return b
}

func discoverState(t *testing.T, dir string) State {
	t.Helper()
	_, err := Discover(dir)
	var de *DiscoverError
	if !errors.As(err, &de) {
		t.Fatalf("Discover(%s) = %v, want a *DiscoverError", dir, err)
	}
	return de.State
}

// TestDiscoverNamesEachStateOfTheUsersChrome reads a missing directory, a
// directory no Chrome holds, a Chrome running without remote debugging, a
// stale DevToolsActivePort beside a live and a dead holder, and a malformed
// file.
func TestDiscoverNamesEachStateOfTheUsersChrome(t *testing.T) {
	if got := discoverState(t, filepath.Join(t.TempDir(), "absent")); got != NoUserDataDir {
		t.Errorf("a missing directory reads %s, want %s", got, NoUserDataDir)
	}
	if got := discoverState(t, t.TempDir()); got != ChromeNotRunning {
		t.Errorf("an empty directory reads %s, want %s", got, ChromeNotRunning)
	}

	off := startChrome(t, nil, false)
	if got := discoverState(t, off.dir); got != DebuggingOff {
		t.Errorf("a Chrome running with remote debugging off reads %s, want %s", got, DebuggingOff)
	}
	// A stale advertisement beside the live holder: the dial is refused.
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	stalePort := closed.Addr().(*net.TCPAddr).Port
	_ = closed.Close()
	stale := fmt.Sprintf("%d\n/devtools/browser/00000000-0000-4000-8000-000000000000", stalePort)
	if err := os.WriteFile(filepath.Join(off.dir, "DevToolsActivePort"), []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}
	ep, err := Discover(off.dir)
	if err != nil {
		t.Fatalf("Discover over a stale file: %v", err)
	}
	_, dialErr := Attach(t.Context(), ep.URL, ep.Port)
	var de *DiscoverError
	if !errors.As(ClassifyDial(off.dir, dialErr), &de) || de.State != DebuggingOff {
		t.Errorf("a refused dial with a live holder classifies as %v, want %s", ClassifyDial(off.dir, dialErr), DebuggingOff)
	}
	if strings.Contains(ClassifyDial(off.dir, dialErr).Error(), "/devtools/browser/") {
		t.Errorf("the classified error carries the endpoint path: %v", ClassifyDial(off.dir, dialErr))
	}
	off.stop()
	if !errors.As(ClassifyDial(off.dir, dialErr), &de) || de.State != ChromeNotRunning {
		t.Errorf("a refused dial with a dead holder classifies as %v, want %s", ClassifyDial(off.dir, dialErr), ChromeNotRunning)
	}

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "DevToolsActivePort"), []byte("not-a-port\n/devtools/browser/x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := discoverState(t, bad); got != BadActivePort {
		t.Errorf("a malformed DevToolsActivePort reads %s, want %s", got, BadActivePort)
	}
}

// TestClassifyDialNamesAnHTTP403AsAllowDeclined dials a websocket endpoint
// that answers the upgrade with HTTP 403, as Chrome's approval mode answers a
// declined connection.
func TestClassifyDialNamesAnHTTP403AsAllowDeclined(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "Connection rejected", http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	_, err := Attach(t.Context(), fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/x", port), port)
	var de *DiscoverError
	if !errors.As(ClassifyDial("/profile", err), &de) || de.State != AllowDeclined {
		t.Fatalf("a 403 dial classifies as %v, want %s", ClassifyDial("/profile", err), AllowDeclined)
	}
	if !strings.Contains(de.Error(), "Allow") {
		t.Errorf("the declined message does not name the Allow step: %v", de)
	}
}

// TestClassifyDialNamesApprovalModesRefusalAsAllowDeclined dials a path the
// DevTools server refuses on a real Chrome in approval mode, which answers
// HTTP 403 as it does a declined connection, and asserts the dial classifies
// as the Allow prompt declined.
func TestClassifyDialNamesApprovalModesRefusalAsAllowDeclined(t *testing.T) {
	c := startApprovalModeChrome(t)
	_, err := Attach(t.Context(), fmt.Sprintf("ws://127.0.0.1:%d/devtools/page/00000000", c.port), c.port)
	var de *DiscoverError
	if !errors.As(ClassifyDial(c.dir, err), &de) || de.State != AllowDeclined {
		t.Fatalf("approval mode's refusal classifies as %v (dial error %v), want %s", ClassifyDial(c.dir, err), err, AllowDeclined)
	}
}

// TestDetachRefusesAUserTabChangedOrAnOwnTabLeftOpen attaches, navigates a
// user tab from outside, and asserts Detach names it; then leaves an own tab
// open and asserts Detach names that.
func TestDetachRefusesAUserTabChangedOrAnOwnTabLeftOpen(t *testing.T) {
	f := newFixture(t)
	c := startChrome(t, f, true)
	user := c.open(t, f.url("/page"), "Page")

	b := attach(t, c)
	if _, err := b.Call(t.Context(), "Target.activateTarget", map[string]any{"targetId": user}, ""); err != nil {
		t.Fatal(err)
	}
	other, err := Attach(t.Context(), c.endpoint(t).URL, c.port)
	if err != nil {
		t.Fatal(err)
	}
	ut, err := other.AttachTab(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Call(t.Context(), "Page.navigate", map[string]any{"url": f.url("/later")}, ut.SessionID); err != nil {
		t.Fatal(err)
	}
	_ = ut.Release(t.Context())
	other.Close()
	err = b.Detach(t.Context())
	if err == nil || !strings.Contains(err.Error(), f.url("/page")) {
		t.Errorf("Detach after a user tab was navigated = %v, want an error naming the tab", err)
	}

	b2 := attach(t, c)
	if _, err := b2.OpenTab(t.Context()); err != nil {
		t.Fatal(err)
	}
	err = b2.Detach(t.Context())
	if err == nil || !strings.Contains(err.Error(), "own tab") {
		t.Errorf("Detach with an own tab left open = %v, want an error naming it", err)
	}
}

// TestAnInPlaceReadAndFetchLeaveTheUsersTabAsItWas reads a user tab in place,
// runs an in-page fetch there, and asserts the tab's id, URL, document and an
// in-page mark are unchanged and the page was not reloaded.
func TestAnInPlaceReadAndFetchLeaveTheUsersTabAsItWas(t *testing.T) {
	f := newFixture(t)
	c := startChrome(t, f, true)
	c.open(t, f.url("/login"), "Signed in")
	user := c.open(t, f.url("/page"), "Page")
	before := c.tabs(t)
	loads := len(f.requests("/page"))

	b := attach(t, c)
	tab, err := b.AttachTab(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Eval(t.Context(), `window.__cdpMark = 'm1'; 1`, nil); err != nil {
		t.Fatal(err)
	}
	resp, err := tab.Fetch(t.Context(), Request{Method: http.MethodGet, URL: "/binary"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != http.StatusOK || !bytes.Equal(resp.Body, binaryBody) {
		t.Errorf("the in-place fetch answered %d with %d bytes, want 200 with the %d fixture bytes", resp.Status, len(resp.Body), len(binaryBody))
	}
	var mark string
	if err := tab.Eval(t.Context(), `window.__cdpMark`, &mark); err != nil || mark != "m1" {
		t.Errorf("the in-page mark reads %q (%v) after the fetch, want m1: the document was replaced", mark, err)
	}
	if err := tab.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.Detach(t.Context()); err != nil {
		t.Errorf("Detach after an in-place read: %v", err)
	}
	if after := c.tabs(t); !maps.Equal(before, after) {
		t.Errorf("the user's tabs changed:\nbefore %v\nafter  %v", before, after)
	}
	if got := len(f.requests("/page")); got != loads {
		t.Errorf("the user's page was loaded %d more times", got-loads)
	}
}

// TestFetchSendsItsRequestAndKeepsTheCookieInThePage runs a POST with a body
// and a header, and a csrf header read from the JSESSIONID cookie in the page,
// through a recording proxy, and asserts the fixture saw all three while the
// DevTools traffic the helper sent never carried the cookie value.
func TestFetchSendsItsRequestAndKeepsTheCookieInThePage(t *testing.T) {
	f := newFixture(t)
	c := startChrome(t, f, true)
	c.open(t, f.url("/login"), "Signed in")
	proxy := newRecordingProxy(t, c.port)
	ep := c.endpoint(t)
	b, err := Attach(t.Context(), strings.Replace(ep.URL, fmt.Sprintf(":%d/", ep.Port), fmt.Sprintf(":%d/", proxy.port()), 1), proxy.port())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Close)
	tab, err := b.OpenTab(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(t.Context(), f.url("/page")); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"q":"é \n x"}`)
	resp, err := tab.Fetch(t.Context(), Request{
		Method: http.MethodPost, URL: "/echo", Body: body,
		Headers:       map[string]string{"x-probe": "p1", "content-type": "application/json"},
		CookieHeaders: map[string]string{"csrf-token": "JSESSIONID"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var echo struct{ Method, Csrf, X, Body string }
	if err := json.Unmarshal(resp.Body, &echo); err != nil {
		t.Fatalf("decoding the echo %q: %v", resp.Body, err)
	}
	if echo.Method != http.MethodPost || echo.X != "p1" || echo.Body != string(body) {
		t.Errorf("the fixture saw %+v, want method POST, x-probe p1 and the body", echo)
	}
	if echo.Csrf != sessionCookieValue {
		t.Errorf("csrf-token reached the fixture as %q, want the JSESSIONID value with its quotes stripped %q", echo.Csrf, sessionCookieValue)
	}
	if _, err := tab.Fetch(t.Context(), Request{Method: http.MethodGet, URL: "/echo", CookieHeaders: map[string]string{"csrf-token": "NO_SUCH_COOKIE"}}); err == nil || !strings.Contains(err.Error(), "NO_SUCH_COOKIE") {
		t.Errorf("a fetch naming a cookie the page lacks = %v, want an error naming the cookie", err)
	}
	if err := tab.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
	sent := proxy.clientText(t)
	if !strings.Contains(sent, "JSESSIONID") {
		t.Fatalf("the decoded DevTools traffic does not carry the cookie name the fetch script reads; the decoder read nothing")
	}
	if strings.Contains(sent, sessionCookieValue) {
		t.Errorf("the DevTools traffic the helper sent carries the JSESSIONID value")
	}
}

// TestWaitForFollowsTheOwnTabAcrossANavigation waits on a predicate that only
// a later document satisfies, after a page that replaces itself, and on a
// predicate a DOM mutation satisfies.
func TestWaitForFollowsTheOwnTabAcrossANavigation(t *testing.T) {
	f := newFixture(t)
	c := startChrome(t, f, true)
	b := attach(t, c)
	tab, err := b.OpenTab(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.Navigate(t.Context(), f.url("/hop")); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := tab.WaitFor(t.Context(), `return document.getElementById('p') ? location.pathname : null;`, &path); err != nil || path != "/page" {
		t.Errorf("WaitFor across the client redirect = %q, %v; want /page", path, err)
	}
	var fired bool
	waitCtx, cancelWait := context.WithCancel(t.Context())
	defer cancelWait()
	done := make(chan error, 1)
	go func() {
		done <- tab.WaitFor(waitCtx, `window.__checkedOnce = true; return window.__stateFlag ? true : null;`, &fired)
	}()
	// Once the pending WaitFor has checked once, set the flag with no DOM
	// mutation and dispatch the check event.
	setFlag := `new Promise(r => { const go = () => window.__checkedOnce ? r() : setTimeout(go, 0); go(); }).then(() => { window.__stateFlag = 1; window.dispatchEvent(new Event(` + strconv.Quote(CheckEvent) + `)); })`
	if err := tab.Eval(t.Context(), setFlag, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("WaitFor on page state that changed with no DOM mutation and the check event: %v", err)
		}
		if !fired {
			t.Errorf("WaitFor on page state that changed with no DOM mutation and the check event decoded %v, want true", fired)
		}
	case <-time.After(waitFence):
		cancelWait()
		<-done
		t.Errorf("WaitFor did not re-check its predicate within %s of the %s event", waitFence, CheckEvent)
	}
	if err := tab.Navigate(t.Context(), f.url("/later")); err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := tab.WaitFor(t.Context(), `return document.getElementById('ready') ? true : null;`, &ready); err != nil || !ready {
		t.Errorf("WaitFor on a DOM mutation = %v, %v; want true", ready, err)
	}
	if err := tab.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestReleaseClosesTheOwnTabOnly opens an own tab, releases it, and asserts
// the browser no longer lists it while the user's tab is still open.
func TestReleaseClosesTheOwnTabOnly(t *testing.T) {
	f := newFixture(t)
	c := startChrome(t, f, true)
	user := c.open(t, f.url("/page"), "Page")
	b := attach(t, c)
	tab, err := b.OpenTab(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AttachTab(t.Context(), tab.TargetID); err == nil {
		t.Errorf("AttachTab on the collector's own open tab succeeded; it attaches only to pages the user had open")
	}
	if err := tab.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	tabs := c.tabs(t)
	if _, open := tabs[tab.TargetID]; open {
		t.Errorf("the own tab %s is still listed after Release", tab.TargetID)
	}
	if _, open := tabs[user]; !open {
		t.Errorf("the user's tab %s was closed", user)
	}
	ut, err := b.AttachTab(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	if err := ut.Navigate(t.Context(), f.url("/later")); err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Errorf("Navigate on a user tab = %v; it must be refused", err)
	}

	if err := ut.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, open := c.tabs(t)[user]; !open {
		t.Errorf("releasing the user's tab closed it")
	}
	if err := b.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// TestEnvironmentAndUserDataDir asserts the exported declaration and the
// directory resolution: set, unset with HOME, set and empty, neither.
func TestEnvironmentAndUserDataDir(t *testing.T) {
	env := Environment()
	if len(env) != 2 || env[0].Name != "KNOWLEDGE_CHROME_USER_DATA_DIR" || env[0].Class != "path" || !env[0].EmptySensitive ||
		env[1].Name != "HOME" || env[1].Class != "path" || env[1].EmptySensitive {
		t.Errorf("Environment() = %+v, want KNOWLEDGE_CHROME_USER_DATA_DIR (path, empty-sensitive) and HOME (path)", env)
	}
	lookup := func(m map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
	}
	if dir, err := UserDataDir(lookup(map[string]string{UserDataDirEnv: "/p", "HOME": "/h"})); err != nil || dir != "/p" {
		t.Errorf("a set directory resolves to %q, %v; want /p", dir, err)
	}
	want, _ := DefaultUserDataDir("/h")
	if dir, err := UserDataDir(lookup(map[string]string{"HOME": "/h"})); err != nil || dir != want || !strings.HasPrefix(dir, "/h/") {
		t.Errorf("unset resolves to %q, %v; want Chrome's default under /h", dir, err)
	}
	if _, err := UserDataDir(lookup(map[string]string{UserDataDirEnv: "", "HOME": "/h"})); err == nil || !strings.Contains(err.Error(), UserDataDirEnv) {
		t.Errorf("set and empty = %v, want an error naming %s", err, UserDataDirEnv)
	}
	if _, err := UserDataDir(lookup(map[string]string{})); err == nil || !strings.Contains(err.Error(), "HOME") || !strings.Contains(err.Error(), UserDataDirEnv) {
		t.Errorf("neither set = %v, want an error naming both", err)
	}
}
