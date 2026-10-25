// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"os"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// TokenVariable is the environment variable the Slack token is read from.
const TokenVariable = "SLACK_TOKEN"

// The node types this collector emits.
const (
	NodeChannel = "slack-channel"
	NodeMessage = "slack-message"
	NodeUser    = "slack-user"
)

// The edge types this collector emits.
const (
	// EdgeBelongsTo joins a message to its channel.
	EdgeBelongsTo = "BELONGS_TO"
	// EdgePostedBy joins a message to the user who posted it.
	EdgePostedBy = "POSTED_BY"
	// EdgeRepliesTo joins a thread reply to its thread's parent message.
	EdgeRepliesTo = "REPLIES_TO"
)

// NodeTypes is every node type the walk emits, sorted.
func NodeTypes() []string { return []string{NodeChannel, NodeMessage, NodeUser} }

// EdgeTypes is every edge type the walk emits, sorted.
func EdgeTypes() []string { return []string{EdgeBelongsTo, EdgePostedBy, EdgeRepliesTo} }

// Describe declares the collector: its suggested behavior, its vocabulary and
// the one secret it reads.
func (c Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(false),
			Embeddable:   new(true),
			Syncable:     new(true),
			EmbedFields:  []string{"symbol_name", "content"},
			Bm25Fields:   []string{"symbol_name", "content"},
		},
		NodeTypes: NodeTypes(),
		EdgeTypes: EdgeTypes(),
		Environment: []framework.EnvDeclaration{{
			Name:        TokenVariable,
			Class:       framework.EnvClassSecret,
			Description: "a Slack bot (xoxb) or user (xoxp) token; search needs a user token with search:read",
		}},
	}
}

// token reads SLACK_TOKEN. Absent and present-but-empty are both refused,
// naming the variable and the reference form an entry carries.
func token() (string, error) {
	value := os.Getenv(TokenVariable)
	if value == "" {
		return "", fmt.Errorf(
			"slack: %s is not set. It carries the Slack token this collector reads with; the config entry's env "+
				"block names it as the reference %q and the collector's credential file holds the value",
			TokenVariable, "${"+TokenVariable+"}")
	}
	return value, nil
}
