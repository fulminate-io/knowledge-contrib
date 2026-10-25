// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/slackapi"
)

// Params is what a collect names. Every field carries omitempty, so the
// inferred schema requires none of them; validate refuses a collect that names
// no channel, thread or search.
type Params struct {
	// Channels are conversation ids whose history the walk reads.
	Channels []string `json:"channels,omitempty"`
	// Threads are single threads, each read with conversations.replies.
	Threads []Thread `json:"threads,omitempty"`
	// Search are search.messages queries. They need a user token with the
	// search:read scope.
	Search []string `json:"search,omitempty"`
	// StartTime and EndTime bound every selector, as RFC 3339 timestamps.
	// Either may be omitted.
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`
}

// Thread names one thread by its channel and its parent message's ts.
type Thread struct {
	Channel string `json:"channel"`
	TS      string `json:"ts"`
}

// window is the decoded time bound. A zero time is an open end.
type window struct {
	start time.Time
	end   time.Time
}

// slack renders the window as conversations.history / replies arguments:
// Unix seconds with six fractional digits.
func (w window) slack() slackapi.Window {
	var out slackapi.Window
	if !w.start.IsZero() {
		out.Oldest = slackTS(w.start)
	}
	if !w.end.IsZero() {
		out.Latest = slackTS(w.end)
	}
	return out
}

// contains reports whether the Slack timestamp ts lies inside the window,
// both ends inclusive. A ts that does not parse is outside.
func (w window) contains(ts string) bool {
	at, ok := parseSlackTS(ts)
	if !ok {
		return false
	}
	if !w.start.IsZero() && at.Before(w.start) {
		return false
	}
	if !w.end.IsZero() && at.After(w.end) {
		return false
	}
	return true
}

func (w window) isOpen() bool { return w.start.IsZero() && w.end.IsZero() }

// slackTSPattern is the form of a Slack message timestamp: Unix seconds, a
// dot, and the fractional digits.
var slackTSPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+$`)

func slackTS(t time.Time) string {
	return fmt.Sprintf("%d.%06d", t.Unix(), t.Nanosecond()/1000)
}

func parseSlackTS(ts string) (time.Time, bool) {
	if !slackTSPattern.MatchString(ts) {
		return time.Time{}, false
	}
	secs, frac, _ := strings.Cut(ts, ".")
	s, err := strconv.ParseInt(secs, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	us, err := strconv.ParseInt((frac + "000000")[:6], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(s, us*1000), true
}

// validate refuses a collect that names no target, an empty or malformed
// entry, or an unusable window, naming the param path.
func (p Params) validate() (window, error) {
	if len(p.Channels) == 0 && len(p.Threads) == 0 && len(p.Search) == 0 {
		return window{}, fmt.Errorf(
			"slack: the collect names no target; set at least one of params.channels (conversation ids), " +
				"params.threads ({channel, ts} pairs) or params.search (search.messages queries)")
	}
	for i, c := range p.Channels {
		if strings.TrimSpace(c) == "" {
			return window{}, fmt.Errorf("slack: params.channels[%d] is empty; every entry names a conversation id", i)
		}
	}
	for i, t := range p.Threads {
		if strings.TrimSpace(t.Channel) == "" {
			return window{}, fmt.Errorf("slack: params.threads[%d].channel is empty; it names the thread's conversation id", i)
		}
		if !slackTSPattern.MatchString(t.TS) {
			return window{}, fmt.Errorf(
				"slack: params.threads[%d].ts %q is not a Slack message timestamp such as 1512085950.000216", i, t.TS)
		}
	}
	for i, q := range p.Search {
		if strings.TrimSpace(q) == "" {
			return window{}, fmt.Errorf("slack: params.search[%d] is empty; an empty query would select every message", i)
		}
	}
	var w window
	var err error
	if w.start, err = parseBound("start_time", p.StartTime); err != nil {
		return window{}, err
	}
	if w.end, err = parseBound("end_time", p.EndTime); err != nil {
		return window{}, err
	}
	if !w.start.IsZero() && !w.end.IsZero() && w.end.Before(w.start) {
		return window{}, fmt.Errorf("slack: params.end_time %s is before params.start_time %s, so the window selects nothing",
			p.EndTime, p.StartTime)
	}
	return w, nil
}

func parseBound(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("slack: params.%s %q is not an RFC 3339 timestamp: %w", name, value, err)
	}
	return ts, nil
}
