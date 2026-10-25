// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// cdpattach_calls_test.go — the init script install, and a call cancelled while its reply is withheld.

// TestAddInitScriptRunsBeforeThePagesOwnScripts installs a script that sets a
// mark, loads a page whose own inline script reads the mark, and asserts the
// page saw it, in this document and the next; a user tab is refused.
func TestAddInitScriptRunsBeforeThePagesOwnScripts(t *testing.T) {
	f := newFixture(t)
	c := startChrome(t, f, true)
	user := c.open(t, f.url("/page"), "Page")
	b := attach(t, c)
	tab, err := b.OpenTab(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := tab.AddInitScript(t.Context(), `window.__initMark = 'installed'`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := tab.Navigate(t.Context(), f.url("/early")); err != nil {
			t.Fatal(err)
		}
		var seen string
		if err := tab.WaitFor(t.Context(), `return window.__seenByPage || null;`, &seen); err != nil {
			t.Fatal(err)
		}
		if seen != "installed" {
			t.Errorf("the page's own script saw the mark as %q, want installed: the init script did not run first", seen)
		}
	}
	ut, err := b.AttachTab(t.Context(), user)
	if err != nil {
		t.Fatal(err)
	}
	if err := ut.AddInitScript(t.Context(), `1`); err == nil {
		t.Errorf("AddInitScript on a user tab succeeded; it must be refused")
	}
	if err := ut.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := tab.Release(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := b.Detach(t.Context()); err != nil {
		t.Fatal(err)
	}
}

// holdingPeer is a websocket peer standing in for Chrome: it answers every
// command at once except Hold.reply, whose reply it sends when release is
// closed.
type holdingPeer struct {
	srv      *httptest.Server
	held     chan int64
	release  chan struct{}
	released atomic.Bool
	// destroy gates Target.targetDestroyed: the peer sends it after answering
	// Target.closeTarget once destroy is closed.
	destroy   chan struct{}
	destroyed atomic.Bool
	wg        sync.WaitGroup
}

func newHoldingPeer(t *testing.T) *holdingPeer {
	t.Helper()
	p := &holdingPeer{held: make(chan int64, 1), release: make(chan struct{}), destroy: make(chan struct{})}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, _, err := ws.UpgradeHTTP(r, w)
		if err != nil {
			return
		}
		var wmu sync.Mutex
		write := func(v any) {
			raw, err := json.Marshal(v)
			if err != nil {
				panic(err)
			}
			wmu.Lock()
			defer wmu.Unlock()
			_ = wsutil.WriteServerText(conn, raw)
		}
		defer conn.Close()
		for {
			data, err := wsutil.ReadClientText(conn)
			if err != nil {
				return
			}
			var msg struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			_ = json.Unmarshal(data, &msg)
			switch msg.Method {
			case "Hold.reply":
				p.held <- msg.ID
				p.wg.Go(func() {
					<-p.release
					write(map[string]any{"id": msg.ID, "result": map[string]any{"held": true}})
				})
			case "Target.createTarget":
				write(map[string]any{"id": msg.ID, "result": map[string]any{"targetId": "own-1"}})
			case "Target.attachToTarget":
				write(map[string]any{"id": msg.ID, "result": map[string]any{"sessionId": "session-1"}})
			case "Target.closeTarget":
				write(map[string]any{"id": msg.ID, "result": map[string]any{"success": true}})
				write(map[string]any{"method": "Probe.closeAnswered", "params": map[string]any{}})
				p.wg.Go(func() {
					<-p.destroy
					write(map[string]any{"method": "Target.targetDestroyed", "params": map[string]any{"targetId": "own-1"}})
				})
			case "Target.getTargets":
				write(map[string]any{"id": msg.ID, "result": map[string]any{"targetInfos": []any{}}})
			default:
				write(map[string]any{"id": msg.ID, "result": map[string]any{"method": msg.Method}})
			}
		}
	}))
	t.Cleanup(func() { p.releaseHeld(); p.destroyNow(); p.srv.Close(); p.wg.Wait() })
	return p
}

// releaseHeld sends the withheld reply; it is safe to call more than once.
func (p *holdingPeer) releaseHeld() {
	if p.released.CompareAndSwap(false, true) {
		close(p.release)
	}
}

// destroyNow lets the peer send Target.targetDestroyed; it is safe to call
// more than once.
func (p *holdingPeer) destroyNow() {
	if p.destroyed.CompareAndSwap(false, true) {
		close(p.destroy)
	}
}

// TestACancelledCallReturnsAndTheConnectionStaysUsable cancels a call whose
// reply the peer withholds and asserts it returns ctx's error, that the next
// call answers with its own reply after the withheld one is released, and
// that Close leaves no goroutine behind (TestMain's leak check).
func TestACancelledCallReturnsAndTheConnectionStaysUsable(t *testing.T) {
	p := newHoldingPeer(t)
	port := p.srv.Listener.Addr().(*net.TCPAddr).Port
	b, err := Attach(t.Context(), fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/x", port), port)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := b.Call(ctx, "Hold.reply", nil, "")
		done <- err
	}()
	<-p.held
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the cancelled call returned %v, want context.Canceled", err)
		}
	case <-time.After(waitFence):
		t.Fatalf("a call whose context was cancelled did not return within %s", waitFence)
	}
	p.releaseHeld()
	res, err := b.Call(t.Context(), "Next.call", nil, "")
	if err != nil {
		t.Fatalf("the call after the cancelled one: %v", err)
	}
	var got struct {
		Method string `json:"method"`
		Held   bool   `json:"held"`
	}
	if err := json.Unmarshal(res, &got); err != nil || got.Held || got.Method != "Next.call" {
		t.Errorf("the next call answered %s, want its own reply", res)
	}
}

// quietFence is how long a row waits to observe that a call has not returned.
const quietFence = 500 * time.Millisecond

// TestReleaseWaitsForTheBrowserToReportTheTabDestroyed releases an own tab on
// a peer that answers Target.closeTarget at once and withholds
// Target.targetDestroyed: Release has not returned quietFence after the peer
// has sent the answer, and returns nil once the event arrives.
func TestReleaseWaitsForTheBrowserToReportTheTabDestroyed(t *testing.T) {
	p := newHoldingPeer(t)
	port := p.srv.Listener.Addr().(*net.TCPAddr).Port
	b, err := Attach(t.Context(), fmt.Sprintf("ws://127.0.0.1:%d/devtools/browser/x", port), port)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	tab, err := b.OpenTab(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// The peer sends Probe.closeAnswered right after the closeTarget answer;
	// the reader publishes it after it has routed the answer to Release's call.
	answered, stop := b.subscribe("", "Probe.closeAnswered", nil)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- tab.Release(t.Context()) }()
	<-answered.ch
	select {
	case err := <-done:
		t.Fatalf("Release returned %v with the destroyed event withheld %s after the closeTarget answer; it must wait for the event", err, quietFence)
	case <-time.After(quietFence):
	}
	p.destroyNow()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Release once the destroyed event arrived = %v, want nil", err)
		}
	case <-time.After(waitFence):
		t.Fatalf("Release did not return within %s of the destroyed event", waitFence)
	}
}
