// SPDX-License-Identifier: Apache-2.0

package cdpattach

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// Tab is a tab this package attached to: one the collector opened itself (own)
// or one the user has open.
type Tab struct {
	b         *Browser
	TargetID  string
	SessionID string
	own       bool
}

// Own reports whether the collector opened this tab.
func (t *Tab) Own() bool { return t.own }

// OpenTab opens a background tab at about:blank in the user's default browser
// context, attaches to it, turns on focus emulation for it, and enables the
// Runtime domain so [Tab.WaitFor] observes each new document.
func (b *Browser) OpenTab(ctx context.Context) (*Tab, error) {
	res, err := b.Call(ctx, "Target.createTarget", map[string]any{"url": "about:blank", "background": true}, "")
	if err != nil {
		return nil, err
	}
	var c struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(res, &c); err != nil || c.TargetID == "" {
		return nil, fmt.Errorf("Target.createTarget: the reply names no target: %s", res)
	}
	b.own[c.TargetID] = true
	t, err := b.attach(ctx, c.TargetID, true)
	if err != nil {
		return nil, err
	}
	if _, err := b.Call(ctx, "Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, t.SessionID); err != nil {
		return t, err
	}
	if _, err := b.Call(ctx, "Runtime.enable", nil, t.SessionID); err != nil {
		return t, err
	}
	return t, nil
}

// AttachTab attaches to a page the user had open at Attach, for reading in
// place. The tab is never navigated or closed through the returned value;
// [Tab.Release] detaches from it.
func (b *Browser) AttachTab(ctx context.Context, targetID string) (*Tab, error) {
	if _, ok := b.before[targetID]; !ok || b.own[targetID] {
		return nil, fmt.Errorf("target %s is not a page the user had open when the collector attached", targetID)
	}
	return b.attach(ctx, targetID, false)
}

// AddInitScript enables the Page domain on the collector's own tab and
// installs source so it runs in every document the tab loads from then on,
// before the document's own scripts. A user tab is refused.
func (t *Tab) AddInitScript(ctx context.Context, source string) error {
	if !t.own {
		return errors.New("refusing to install a script in a tab the user has open")
	}
	if _, err := t.b.Call(ctx, "Page.enable", nil, t.SessionID); err != nil {
		return err
	}
	_, err := t.b.Call(ctx, "Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": source}, t.SessionID)
	return err
}

// Navigate points the collector's own tab at url and returns once the
// navigation committed. A user tab is refused.
func (t *Tab) Navigate(ctx context.Context, url string) error {
	if !t.own {
		return errors.New("refusing to navigate a tab the user has open")
	}
	res, err := t.b.Call(ctx, "Page.navigate", map[string]any{"url": url}, t.SessionID)
	if err != nil {
		return err
	}
	var r struct {
		ErrorText string `json:"errorText"`
	}
	if err := json.Unmarshal(res, &r); err != nil {
		return fmt.Errorf("decoding the Page.navigate reply: %w", err)
	}
	if r.ErrorText != "" {
		return fmt.Errorf("navigating to %s failed: %s", url, r.ErrorText)
	}
	return nil
}

// ScriptError is an exception a page script threw.
type ScriptError struct{ Text string }

func (e *ScriptError) Error() string { return "the page script threw: " + e.Text }

// Eval evaluates expr in the tab, awaiting a promise it returns, and decodes
// the value into out (nil discards it).
func (t *Tab) Eval(ctx context.Context, expr string, out any) error {
	res, err := t.b.Call(ctx, "Runtime.evaluate", map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true}, t.SessionID)
	if err != nil {
		return err
	}
	var ev struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := json.Unmarshal(res, &ev); err != nil {
		return fmt.Errorf("decoding the Runtime.evaluate reply: %w", err)
	}
	if ev.ExceptionDetails != nil {
		text := ev.ExceptionDetails.Exception.Description
		if text == "" {
			text = ev.ExceptionDetails.Text
		}
		return &ScriptError{Text: text}
	}
	if out == nil || len(ev.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(ev.Result.Value, out)
}

// CheckEvent is the window event that makes a pending WaitFor re-run its
// predicate; a script in the page dispatches it when state the predicate
// reads changes without a DOM mutation.
const CheckEvent = "cdpattach:check"

// waitScript, formatted with a predicate body (JavaScript statements that
// return a value when the condition holds and null otherwise) and the check
// event's name, is a promise that resolves to that value, re-checking on every
// DOM mutation, on the load event and on that event.
const waitScript = `new Promise((resolve, reject) => {
  const pred = () => { %[1]s };
  let obs = null;
  const check = () => {
    let v;
    try { v = pred(); } catch (e) { if (obs) obs.disconnect(); reject(e); return true; }
    if (v === null || v === undefined) return false;
    if (obs) obs.disconnect();
    resolve(v);
    return true;
  };
  if (check()) return;
  obs = new MutationObserver(check);
  obs.observe(document, {subtree: true, childList: true, attributes: true, characterData: true});
  window.addEventListener('load', check);
  window.addEventListener(%[2]q, check);
})`

// WaitFor evaluates the predicate body in the tab until it returns a non-null
// value, and decodes that value into out. On the collector's own tab a
// navigation that replaces the document re-runs the predicate in the new one.
// It waits on ctx alone.
func (t *Tab) WaitFor(ctx context.Context, predicate string, out any) error {
	expr := fmt.Sprintf(waitScript, predicate, CheckEvent)
	for {
		created, cancelCreated := t.b.subscribe(t.SessionID, "Runtime.executionContextCreated", nil)
		gone, cancelGone := t.b.subscribe("", "Target.detachedFromTarget", func(p json.RawMessage) bool {
			var d struct {
				SessionID string `json:"sessionId"`
			}
			return json.Unmarshal(p, &d) == nil && d.SessionID == t.SessionID
		})
		err := t.Eval(ctx, expr, out)
		var perr *ProtocolError
		if err == nil || !t.own || !errors.As(err, &perr) {
			cancelCreated()
			cancelGone()
			return err
		}
		next := false
		select {
		case <-created.ch:
			next = true
		case <-gone.ch:
			err = fmt.Errorf("the collector's tab closed while waiting for the page: %w", err)
		case <-ctx.Done():
			err = ctx.Err()
		case <-t.b.done:
			err = ErrClosed
		}
		cancelCreated()
		cancelGone()
		if !next {
			return err
		}
	}
}

// Request is one in-page fetch. CookieHeaders maps a request header name to a
// cookie name: the header is set, in the page, to that cookie's value with
// surrounding double quotes removed, and a cookie the page does not hold
// fails the fetch naming the cookie. A nil Body sends none.
type Request struct {
	Method        string
	URL           string
	Headers       map[string]string
	CookieHeaders map[string]string
	Body          []byte
}

// Response is an in-page fetch's answer: the HTTP status, the content type,
// the final URL after redirects, and the body bytes exactly as received.
type Response struct {
	Status      int
	ContentType string
	URL         string
	Body        []byte
}

// fetchScript runs one fetch in the page with the page's credentials and
// returns {status, type, url, b64}. The request descriptor is a JSON literal.
const fetchScript = `(async () => {
  const req = %s;
  const headers = Object.assign({}, req.headers || {});
  for (const [header, cookie] of Object.entries(req.cookieHeaders || {})) {
    const pair = document.cookie.split('; ').find(p => p.startsWith(cookie + '='));
    if (pair === undefined) throw new Error('the page holds no ' + cookie + ' cookie');
    headers[header] = pair.slice(cookie.length + 1).replace(/^"|"$/g, '');
  }
  const init = {method: req.method, headers, credentials: 'include'};
  if (req.body !== null) init.body = Uint8Array.from(atob(req.body), c => c.charCodeAt(0));
  const r = await fetch(req.url, init);
  const buf = new Uint8Array(await r.arrayBuffer());
  let s = '';
  for (let i = 0; i < buf.length; i += 0x8000) s += String.fromCharCode.apply(null, buf.subarray(i, i + 0x8000));
  return {status: r.status, type: r.headers.get('content-type') || '', url: r.url, b64: btoa(s)};
})()`

// Fetch runs req in the tab's page and returns its answer. The page's own
// cookies ride the request; the collector process never holds them.
func (t *Tab) Fetch(ctx context.Context, req Request) (Response, error) {
	desc := map[string]any{
		"method": req.Method, "url": req.URL, "headers": req.Headers, "cookieHeaders": req.CookieHeaders, "body": nil,
	}
	if req.Body != nil {
		desc["body"] = base64.StdEncoding.EncodeToString(req.Body)
	}
	raw, err := json.Marshal(desc)
	if err != nil {
		return Response{}, fmt.Errorf("encoding the fetch of %s: %w", req.URL, err)
	}
	var r struct {
		Status int    `json:"status"`
		Type   string `json:"type"`
		URL    string `json:"url"`
		B64    string `json:"b64"`
	}
	if err := t.Eval(ctx, fmt.Sprintf(fetchScript, raw), &r); err != nil {
		return Response{}, err
	}
	body, err := base64.StdEncoding.DecodeString(r.B64)
	if err != nil {
		return Response{}, fmt.Errorf("decoding the body of %s: %w", req.URL, err)
	}
	return Response{Status: r.Status, ContentType: r.Type, URL: r.URL, Body: body}, nil
}

// Release ends the collector's use of the tab. An own tab is closed and
// Release returns once the browser reports it destroyed; a user tab is
// detached from and left open.
func (t *Tab) Release(ctx context.Context) error {
	if !t.own {
		_, err := t.b.Call(ctx, "Target.detachFromTarget", map[string]any{"sessionId": t.SessionID}, "")
		return err
	}
	destroyed, cancel := t.b.subscribe("", "Target.targetDestroyed", func(p json.RawMessage) bool {
		var d struct {
			TargetID string `json:"targetId"`
		}
		return json.Unmarshal(p, &d) == nil && d.TargetID == t.TargetID
	})
	defer cancel()
	if _, err := t.b.Call(ctx, "Target.closeTarget", map[string]any{"targetId": t.TargetID}, ""); err != nil {
		return err
	}
	select {
	case <-destroyed.ch:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.b.done:
		return ErrClosed
	}
}
