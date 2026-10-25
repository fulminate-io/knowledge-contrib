// SPDX-License-Identifier: Apache-2.0

// Package cdpattach is the shared connection to the user's own running Chrome
// for the collectors that read through it. It discovers the browser through
// the DevToolsActivePort file in a Chrome user data directory, holds one
// browser-level DevTools Protocol connection, opens and closes tabs of its own,
// reads in place in a tab the user has open, and runs in-page fetches with the
// page's own session.
//
// One reader goroutine owns every read on the connection. It hands each reply
// to the call waiting on that reply's id and each event to the subscriptions
// that match it. Writes are serialized under one mutex. A call returns as soon
// as its context is done; a reply that arrives later for that id is dropped.
// The connection closes only through [Browser.Detach] or [Browser.Close].
package cdpattach

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"sort"
	"strings"
	"sync"

	"github.com/gobwas/ws"
	"github.com/gobwas/ws/wsutil"
)

// ErrClosed is returned by a call made on, or waiting on, a connection whose
// reader has stopped.
var ErrClosed = errors.New("the DevTools connection to Chrome is closed")

// ProtocolError is an error reply to a DevTools Protocol command.
type ProtocolError struct {
	Method  string
	Code    int
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("%s: %s (code %d)", e.Method, e.Message, e.Code)
}

// wireError is the error member of a command reply.
type wireError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// reply is one command reply as the reader decoded it.
type reply struct {
	result json.RawMessage
	err    *ProtocolError
}

// subscription receives a signal for each event of method on session whose
// params satisfy match. The channel holds at most one signal.
type subscription struct {
	session string
	method  string
	match   func(json.RawMessage) bool
	ch      chan struct{}
}

// Browser is one browser-level DevTools connection.
type Browser struct {
	conn net.Conn
	rw   io.ReadWriter

	wmu sync.Mutex

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan reply
	subs    map[*subscription]struct{}
	readErr error

	done      chan struct{}
	closeOnce sync.Once

	// before is the user's page targets at Attach, targetId -> URL; own is the
	// set of targets this connection created.
	before map[string]string
	own    map[string]bool
}

// lockedRW reads from r and writes to conn under wmu, so a control frame the
// reader answers never interleaves with a command a caller writes.
type lockedRW struct {
	r    io.Reader
	conn net.Conn
	wmu  *sync.Mutex
}

func (l lockedRW) Read(p []byte) (int, error) { return l.r.Read(p) }

func (l lockedRW) Write(p []byte) (int, error) {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	return l.conn.Write(p)
}

// Attach dials the browser endpoint wsURL, starts the reader, turns on target
// discovery and records the user's page targets. A failed dial is a
// [*DialError] carrying port, never the endpoint path.
func Attach(ctx context.Context, wsURL string, port int) (*Browser, error) {
	conn, br, _, err := ws.Dial(ctx, wsURL)
	if err != nil {
		return nil, &DialError{Port: port, Err: err}
	}
	var r io.Reader = conn
	if br != nil {
		r = io.MultiReader(br, conn)
	}
	b := &Browser{
		conn:    conn,
		pending: map[int64]chan reply{},
		subs:    map[*subscription]struct{}{},
		done:    make(chan struct{}),
		own:     map[string]bool{},
	}
	b.rw = lockedRW{r: bufio.NewReader(r), conn: conn, wmu: &b.wmu}
	go b.read()
	if _, err := b.Call(ctx, "Target.setDiscoverTargets", map[string]any{"discover": true}, ""); err != nil {
		b.Close()
		return nil, err
	}
	b.before, err = b.Pages(ctx)
	if err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

// read is the reader goroutine. It runs until a read fails, then records the
// failure, wakes every waiting call and returns.
func (b *Browser) read() {
	defer close(b.done)
	for {
		data, err := wsutil.ReadServerText(b.rw)
		if err != nil {
			b.mu.Lock()
			b.readErr = err
			b.mu.Unlock()
			return
		}
		var msg struct {
			ID        int64           `json:"id"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
			SessionID string          `json:"sessionId"`
			Result    json.RawMessage `json:"result"`
			Error     *wireError      `json:"error"`
		}
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			b.deliver(msg.ID, msg.Result, msg.Error)
			continue
		}
		if msg.Method != "" {
			b.publish(msg.SessionID, msg.Method, msg.Params)
		}
	}
}

// deliver hands a reply to the call registered on id, when one still is.
func (b *Browser) deliver(id int64, result json.RawMessage, perr *wireError) {
	b.mu.Lock()
	ch, ok := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if !ok {
		return
	}
	r := reply{result: result}
	if perr != nil {
		r.err = &ProtocolError{Code: perr.Code, Message: perr.Message}
	}
	ch <- r
}

// publish signals every subscription matching the event, without blocking.
func (b *Browser) publish(session, method string, params json.RawMessage) {
	b.mu.Lock()
	var hit []*subscription
	for s := range b.subs {
		if s.session == session && s.method == method && (s.match == nil || s.match(params)) {
			hit = append(hit, s)
		}
	}
	b.mu.Unlock()
	for _, s := range hit {
		select {
		case s.ch <- struct{}{}:
		default:
		}
	}
}

// subscribe registers a subscription and returns it with its cancel function.
func (b *Browser) subscribe(session, method string, match func(json.RawMessage) bool) (*subscription, func()) {
	s := &subscription{session: session, method: method, match: match, ch: make(chan struct{}, 1)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s, func() {
		b.mu.Lock()
		delete(b.subs, s)
		b.mu.Unlock()
	}
}

// Call sends one command on session ("" is the browser session) and waits for
// its reply, ctx, or the end of the connection, whichever comes first.
func (b *Browser) Call(ctx context.Context, method string, params map[string]any, session string) (json.RawMessage, error) {
	if params == nil {
		params = map[string]any{}
	}
	ch := make(chan reply, 1)
	b.mu.Lock()
	if b.readErr != nil {
		b.mu.Unlock()
		return nil, fmt.Errorf("%s: %w", method, ErrClosed)
	}
	b.nextID++
	id := b.nextID
	b.pending[id] = ch
	b.mu.Unlock()
	forget := func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}
	msg := map[string]any{"id": id, "method": method, "params": params}
	if session != "" {
		msg["sessionId"] = session
	}
	data, err := json.Marshal(msg)
	if err != nil {
		forget()
		return nil, fmt.Errorf("%s: encoding the command: %w", method, err)
	}
	if err := wsutil.WriteClientText(b.rw, data); err != nil {
		forget()
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	select {
	case r := <-ch:
		if r.err != nil {
			r.err.Method = method
			return nil, r.err
		}
		return r.result, nil
	case <-ctx.Done():
		forget()
		return nil, ctx.Err()
	case <-b.done:
		forget()
		return nil, fmt.Errorf("%s: %w", method, ErrClosed)
	}
}

// Pages lists the page targets as targetId -> URL.
func (b *Browser) Pages(ctx context.Context) (map[string]string, error) {
	res, err := b.Call(ctx, "Target.getTargets", nil, "")
	if err != nil {
		return nil, err
	}
	var r struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return nil, fmt.Errorf("Target.getTargets: decoding the reply: %w", err)
	}
	out := map[string]string{}
	for _, t := range r.TargetInfos {
		if t.Type == "page" {
			out[t.TargetID] = t.URL
		}
	}
	return out, nil
}

// UserPages returns the page targets the user had open at Attach,
// targetId -> URL.
func (b *Browser) UserPages() map[string]string {
	return maps.Clone(b.before)
}

// Detach checks that every page the user had open at Attach is still open at
// the same URL and that every tab this connection opened is closed, then
// closes the connection. A changed user tab or an own tab left open is an
// error naming it; the connection is closed either way.
func (b *Browser) Detach(ctx context.Context) error {
	defer b.Close()
	after, err := b.Pages(ctx)
	if err != nil {
		return err
	}
	var bad []string
	for id, u := range b.before {
		if b.own[id] {
			continue
		}
		if got, ok := after[id]; !ok {
			bad = append(bad, "a tab at "+u+" was closed")
		} else if got != u {
			bad = append(bad, "a tab at "+u+" now shows "+got)
		}
	}
	for id := range b.own {
		if u, ok := after[id]; ok {
			bad = append(bad, "the collector's own tab at "+u+" is still open")
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return errors.New("the user's tabs changed during the collect: " + strings.Join(bad, "; "))
	}
	return nil
}

// Close closes the connection and waits for the reader to return. It is safe
// to call more than once.
func (b *Browser) Close() {
	b.closeOnce.Do(func() { _ = b.conn.Close() })
	<-b.done
}

// attach opens a flat session on targetID.
func (b *Browser) attach(ctx context.Context, targetID string, own bool) (*Tab, error) {
	res, err := b.Call(ctx, "Target.attachToTarget", map[string]any{"targetId": targetID, "flatten": true}, "")
	if err != nil {
		return nil, err
	}
	var a struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(res, &a); err != nil || a.SessionID == "" {
		return nil, fmt.Errorf("Target.attachToTarget: the reply names no session: %s", res)
	}
	return &Tab{b: b, TargetID: targetID, SessionID: a.SessionID, own: own}, nil
}
