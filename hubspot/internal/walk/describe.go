// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"fmt"
	"os"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// TokenVariable is the environment variable the HubSpot token is read from.
const TokenVariable = "HUBSPOT_ACCESS_TOKEN"

// objectTypes are the CRM object types the walk reads, in the order it reads
// their associations.
var objectTypes = []string{"contacts", "companies", "deals", "tickets", "calls", "emails", "meetings", "notes", "tasks"}

// nodeTypeOf is each object type's node type.
var nodeTypeOf = map[string]string{
	"contacts":  "hubspot-contact",
	"companies": "hubspot-company",
	"deals":     "hubspot-deal",
	"tickets":   "hubspot-ticket",
	"calls":     "hubspot-call",
	"emails":    "hubspot-email",
	"meetings":  "hubspot-meeting",
	"notes":     "hubspot-note",
	"tasks":     "hubspot-task",
}

// NodeList is the node type of a list.
const NodeList = "hubspot-list"

// The edge types this collector emits.
const (
	// EdgeAssociatedWith joins a record to a record associated with it; the
	// association types ride Evidence as JSON.
	EdgeAssociatedWith = "ASSOCIATED_WITH"
	// EdgeHasMember joins a list to each of its members.
	EdgeHasMember = "HAS_MEMBER"
)

// NodeType is the node type of a record of objectType.
func NodeType(objectType string) string { return nodeTypeOf[objectType] }

// NodeTypes is every node type the walk emits, sorted.
func NodeTypes() []string {
	return []string{"hubspot-call", "hubspot-company", "hubspot-contact", "hubspot-deal", "hubspot-email",
		NodeList, "hubspot-meeting", "hubspot-note", "hubspot-task", "hubspot-ticket"}
}

// EdgeTypes is every edge type the walk emits, sorted.
func EdgeTypes() []string { return []string{EdgeAssociatedWith, EdgeHasMember} }

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
			Description: "a HubSpot Service Key or legacy private-app access token with the CRM read scopes",
		}},
	}
}

// token reads HUBSPOT_ACCESS_TOKEN. Absent and present-but-empty are both
// refused, naming the variable and the reference form an entry carries.
func token() (string, error) {
	value := os.Getenv(TokenVariable)
	if value == "" {
		return "", fmt.Errorf(
			"hubspot: %s is not set. It carries the HubSpot token this collector reads with; the config entry's env "+
				"block names it as the reference %q and the collector's credential file holds the value",
			TokenVariable, "${"+TokenVariable+"}")
	}
	return value, nil
}
