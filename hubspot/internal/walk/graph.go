// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/hubapi"
)

// RecordID is the node id of the record id of objectType.
func RecordID(objectType, id string) string { return "hubspot:" + objectType + ":" + id }

// ListID is the node id of a list.
func ListID(id string) string { return "hubspot:list:" + id }

// nameProperties are the properties each object type's display name is read
// from, in order; a contact's first and last names join.
var nameProperties = map[string][]string{
	"companies": {"name", "domain"},
	"deals":     {"dealname"},
	"tickets":   {"subject"},
	"calls":     {"hs_call_title"},
	"emails":    {"hs_email_subject"},
	"meetings":  {"hs_meeting_title"},
	"tasks":     {"hs_task_subject"},
}

// bodyProperties are the properties each object type's text content is read
// from.
var bodyProperties = map[string]string{
	"calls":    "hs_call_body",
	"emails":   "hs_email_text",
	"meetings": "hs_meeting_body",
	"notes":    "hs_note_body",
	"tasks":    "hs_task_body",
	"tickets":  "content",
	"deals":    "description",
}

// graph accumulates the nodes and edges of one walk, keyed by id.
type graph struct {
	nodes map[string]framework.Node
	edges map[string]framework.Edge
}

func newGraph() *graph {
	return &graph{nodes: map[string]framework.Node{}, edges: map[string]framework.Edge{}}
}

func (g *graph) addEdge(e framework.Edge) {
	g.edges[e.FromID+"\x00"+e.Type+"\x00"+e.ToID] = e
}

// addRecord emits a record node carrying every property it was read with.
func (g *graph) addRecord(objectType string, r hubapi.Record) {
	meta := map[string]string{"record_id": string(r.ID), "object_type": objectType, "archived": strconv.FormatBool(r.Archived)}
	setIf(meta, "created_at", r.CreatedAt)
	setIf(meta, "updated_at", r.UpdatedAt)
	for name, value := range r.Properties {
		if value != nil {
			setIf(meta, "property:"+name, *value)
		}
	}
	g.nodes[RecordID(objectType, string(r.ID))] = framework.Node{
		ID: RecordID(objectType, string(r.ID)), Type: NodeType(objectType),
		SymbolName: displayName(objectType, r.Properties), Content: property(r.Properties, bodyProperties[objectType]),
		Metadata: meta,
	}
}

// addList emits the node of list id.
func (g *graph) addList(id string, l hubapi.List) {
	meta := map[string]string{"list_id": id}
	setIf(meta, "object_type_id", l.ObjectTypeID)
	g.nodes[ListID(id)] = framework.Node{ID: ListID(id), Type: NodeList, SymbolName: l.Name, Metadata: meta}
}

// hasMember is the HAS_MEMBER edge from list id to a member of objectType.
func hasMember(id, objectType, member string) framework.Edge {
	return framework.Edge{FromID: ListID(id), ToID: RecordID(objectType, member), Type: EdgeHasMember}
}

// addAssociation emits an ASSOCIATED_WITH edge carrying the association types
// as JSON evidence.
func (g *graph) addAssociation(fromType, fromID, toType, toID string, types []hubapi.AssociationType) error {
	evidence, err := json.Marshal(types)
	if err != nil {
		return fmt.Errorf("hubspot: encoding the association types of %s %s and %s %s: %w", fromType, fromID, toType, toID, err)
	}
	g.addEdge(framework.Edge{
		FromID: RecordID(fromType, fromID), ToID: RecordID(toType, toID), Type: EdgeAssociatedWith, Evidence: string(evidence),
	})
	return nil
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
	keys := make([]string, 0, len(g.edges))
	for k := range g.edges {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	edges := make([]framework.Edge, 0, len(keys))
	for _, k := range keys {
		edges = append(edges, g.edges[k])
	}
	return nodes, edges
}

func displayName(objectType string, props map[string]*string) string {
	if objectType == "contacts" {
		name := strings.TrimSpace(property(props, "firstname") + " " + property(props, "lastname"))
		if name != "" {
			return name
		}
		return property(props, "email")
	}
	for _, p := range nameProperties[objectType] {
		if v := property(props, p); v != "" {
			return v
		}
	}
	return ""
}

func property(props map[string]*string, name string) string {
	if v, ok := props[name]; ok && v != nil {
		return *v
	}
	return ""
}

func setIf(meta map[string]string, key, value string) {
	if value != "" {
		meta[key] = value
	}
}
