// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"slices"
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/slack/internal/slackapi"
)

// ChannelID is the node id of a Slack conversation.
func ChannelID(channel string) string { return "slack:channel:" + channel }

// MessageID is the node id of the message with timestamp ts in channel.
func MessageID(channel, ts string) string { return "slack:message:" + channel + ":" + ts }

// UserID is the node id of a Slack user.
func UserID(user string) string { return "slack:user:" + user }

// graph accumulates the nodes and edges of one walk, keyed by node id so a
// message read twice (history and search, or a thread parent read again by
// conversations.replies) is emitted once.
type graph struct {
	nodes   map[string]framework.Node
	edges   map[string]framework.Edge
	posters map[string]bool
}

func newGraph() *graph {
	return &graph{
		nodes:   map[string]framework.Node{},
		edges:   map[string]framework.Edge{},
		posters: map[string]bool{},
	}
}

func (g *graph) addEdge(from, to, kind string) {
	g.edges[from+"\x00"+kind+"\x00"+to] = framework.Edge{FromID: from, ToID: to, Type: kind}
}

// addChannel emits the channel node read by conversations.info, replacing a
// stub a search match emitted for the same id.
func (g *graph) addChannel(c slackapi.Channel) {
	meta := map[string]string{
		"channel_id":  c.ID,
		"kind":        channelKind(c),
		"is_private":  strconv.FormatBool(c.IsPrivate),
		"is_archived": strconv.FormatBool(c.IsArchived),
	}
	setIf(meta, "name", c.Name)
	setIf(meta, "topic", c.Topic.Value)
	setIf(meta, "purpose", c.Purpose.Value)
	if c.Created > 0 {
		meta["created"] = strconv.FormatInt(c.Created, 10)
	}
	g.nodes[ChannelID(c.ID)] = framework.Node{
		ID: ChannelID(c.ID), Type: NodeChannel, SymbolName: c.Name, Metadata: meta,
	}
}

// addChannelStub emits a channel node from a search match's channel when no
// conversations.info read emitted one.
func (g *graph) addChannelStub(c slackapi.MatchChannel) {
	if _, ok := g.nodes[ChannelID(c.ID)]; ok {
		return
	}
	meta := map[string]string{"channel_id": c.ID}
	setIf(meta, "name", c.Name)
	g.nodes[ChannelID(c.ID)] = framework.Node{ID: ChannelID(c.ID), Type: NodeChannel, SymbolName: c.Name, Metadata: meta}
}

// addMessage emits a message of channel with its BELONGS_TO edge, a POSTED_BY
// edge when the message names a user, and a REPLIES_TO edge to the thread
// parent when it is a reply. A message already emitted is left as it is.
func (g *graph) addMessage(channel string, m slackapi.Message) {
	id := MessageID(channel, m.TS)
	if _, ok := g.nodes[id]; ok {
		return
	}
	meta := map[string]string{"channel_id": channel, "ts": m.TS}
	setIf(meta, "user", m.User)
	setIf(meta, "bot_id", m.BotID)
	setIf(meta, "username", m.Username)
	setIf(meta, "subtype", m.Subtype)
	setIf(meta, "thread_ts", m.ThreadTS)
	setIf(meta, "permalink", m.Permalink)
	if m.ReplyCount > 0 {
		meta["reply_count"] = strconv.Itoa(m.ReplyCount)
	}
	g.nodes[id] = framework.Node{ID: id, Type: NodeMessage, Content: m.Text, Metadata: meta}
	g.addEdge(id, ChannelID(channel), EdgeBelongsTo)
	if m.User != "" {
		g.addEdge(id, UserID(m.User), EdgePostedBy)
		g.posters[m.User] = true
	}
	if isReply(m) {
		g.addEdge(id, MessageID(channel, m.ThreadTS), EdgeRepliesTo)
	}
}

// addUser emits the user node users.info read.
func (g *graph) addUser(u slackapi.User) {
	meta := map[string]string{
		"user_id": u.ID,
		"is_bot":  strconv.FormatBool(u.IsBot),
		"deleted": strconv.FormatBool(u.Deleted),
	}
	setIf(meta, "team_id", u.TeamID)
	setIf(meta, "name", u.Name)
	setIf(meta, "real_name", firstNonEmpty(u.RealName, u.Profile.RealName))
	setIf(meta, "display_name", u.Profile.DisplayName)
	setIf(meta, "title", u.Profile.Title)
	setIf(meta, "tz", u.TZ)
	g.nodes[UserID(u.ID)] = framework.Node{
		ID: UserID(u.ID), Type: NodeUser, SymbolName: firstNonEmpty(u.Profile.DisplayName, u.RealName, u.Name), Metadata: meta,
	}
}

// addUserStub emits a user node carrying only the id, for a poster whose
// users.info read did not answer.
func (g *graph) addUserStub(user string) {
	g.nodes[UserID(user)] = framework.Node{ID: UserID(user), Type: NodeUser, Metadata: map[string]string{"user_id": user}}
}

// result returns the nodes sorted by id and the edges sorted by from, type, to.
func (g *graph) result() ([]framework.Node, []framework.Edge) {
	nodeIDs := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		nodeIDs = append(nodeIDs, id)
	}
	slices.Sort(nodeIDs)
	nodes := make([]framework.Node, 0, len(nodeIDs))
	for _, id := range nodeIDs {
		nodes = append(nodes, g.nodes[id])
	}
	edgeKeys := make([]string, 0, len(g.edges))
	for k := range g.edges {
		edgeKeys = append(edgeKeys, k)
	}
	slices.Sort(edgeKeys)
	edges := make([]framework.Edge, 0, len(edgeKeys))
	for _, k := range edgeKeys {
		edges = append(edges, g.edges[k])
	}
	return nodes, edges
}

// sortedPosters is every user id a message named, sorted.
func (g *graph) sortedPosters() []string {
	out := make([]string, 0, len(g.posters))
	for u := range g.posters {
		out = append(out, u)
	}
	slices.Sort(out)
	return out
}

// isReply reports whether m is a reply inside a thread rather than the
// thread's parent or a message outside any thread.
func isReply(m slackapi.Message) bool { return m.ThreadTS != "" && m.ThreadTS != m.TS }

// isThreadParent reports whether m starts a thread that has replies.
func isThreadParent(m slackapi.Message) bool {
	return m.ReplyCount > 0 && (m.ThreadTS == "" || m.ThreadTS == m.TS)
}

func channelKind(c slackapi.Channel) string {
	switch {
	case c.IsIM:
		return "im"
	case c.IsMPIM:
		return "mpim"
	case c.IsGroup:
		return "group"
	default:
		return "channel"
	}
}

func setIf(meta map[string]string, key, value string) {
	if value != "" {
		meta[key] = value
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
