// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"encoding/json"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// reviewer.go — the node a required reviewer resolves to.
//
// WHY THIS IS ITS OWN FILE AND ITS OWN SIGNATURE. The provider hands a reviewer
// back as an opaque value that is a user OR a team, so reading it means
// re-encoding it and decoding the one field each kind carries. The source
// provider's version of this returned ONE string and absorbed every failure into
// the empty one: a reviewer it could not encode, one it could not decode, one of
// a kind it did not model and one that was simply absent all produced exactly the
// same answer, and its caller — which skipped on empty — could not tell "there is
// no reviewer here" from "I lost this reviewer". A corpus check flags that shape.
//
// SO THE TWO OUTCOMES ARE SEPARATED HERE. A failure to encode or decode is an
// ERROR and the walk refuses; an absent reviewer, or one of a kind this collector
// does not model, is the ZERO resource with no error, which the caller reads as
// "nothing to point at" and skips deliberately.

// reviewerDetail is a reviewer TEAM node's Content: what kind of reviewer it is
// and the identity the provider named it by.
//
// A REVIEWER USER IS BUILT BY [userResource] INSTEAD, and this type is what is
// left once it is. A user is named by four different fields across this walk and
// the same person arrives from more than one of them, so their node has to be the
// same node whichever field produced it; a team is named by a protection rule and
// by nothing else.
type reviewerDetail struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// The two reviewer kinds the provider names in a protection rule.
const (
	reviewerKindUser = "User"
	reviewerKindTeam = "Team"
)

// reviewerResource converts one required reviewer into the node the
// REQUIRES_APPROVAL edge points at.
//
// It returns the zero Resource and a nil error when there is nothing to point
// at; the caller emits no edge in that case.
func reviewerResource(org string, reviewer *gogithub.RequiredReviewer) (ghgraph.Resource, error) {
	if reviewer == nil || reviewer.Reviewer == nil {
		return ghgraph.Resource{}, nil
	}
	identity, err := reviewerIdentity(reviewer)
	if err != nil {
		return ghgraph.Resource{}, err
	}

	switch reviewer.GetType() {
	case reviewerKindUser:
		// THE SAME NODE A RUN'S ACTOR AND A DEPLOYMENT'S CREATOR PRODUCE. A
		// person who approves a deployment is a person who runs workflows, so
		// this arm hands the same four fields to the same constructor rather
		// than building a second, thinner shape for one login to arrive as.
		//
		// THE KIND IS THE PROTECTION RULE'S OWN WORD, not the user object's:
		// what the rule asserted is that this reviewer is a user, and a
		// required reviewer is never a bot.
		return userResource(org, providerUser{
			Login:   identity.Login,
			ID:      identity.ID,
			Kind:    reviewerKindUser,
			HTMLURL: identity.HTMLURL,
		})
	case reviewerKindTeam:
		if identity.Slug == "" {
			return ghgraph.Resource{}, nil
		}
		return reviewerNode(org, ghgraph.ResourceTypeTeam,
			ghgraph.TeamID(org, identity.Slug), reviewerKindTeam, identity.Slug)
	default:
		// A kind this collector does not model. It is not an error — the
		// provider is free to add one — but nothing is invented for it either.
		return ghgraph.Resource{}, nil
	}
}

// reviewerIdentity is what the provider's opaque reviewer value says about who
// the reviewer is.
//
// THE ROUND TRIP THROUGH JSON IS THE ONLY WAY IN. The SDK types the field as an
// empty interface holding a user or a team, so there is no field to read and no
// type to assert that covers both; re-encoding it and decoding the names is what
// the source provider does and it is what this does. Both halves of the trip
// return their error rather than a zero value.
//
// IT DECODES THE USER'S NUMERIC ID AND URL ALONGSIDE THE NAMES, and the decode
// target is what makes that safe: the shape below names the fields it wants, so
// re-encoding the provider's whole user value and decoding this out of it carries
// those fields and no others — the address on that value has nowhere to land.
func reviewerIdentity(reviewer *gogithub.RequiredReviewer) (reviewerIdentityFields, error) {
	var identity reviewerIdentityFields
	raw, err := json.Marshal(reviewer.Reviewer)
	if err != nil {
		return identity, fmt.Errorf(
			"github-environments: re-encoding a %q reviewer to read its identity: %w",
			reviewer.GetType(), err)
	}
	if err := json.Unmarshal(raw, &identity); err != nil {
		return identity, fmt.Errorf(
			"github-environments: decoding the identity of a %q reviewer: %w",
			reviewer.GetType(), err)
	}
	return identity, nil
}

// reviewerIdentityFields is the decode target: a user's login, id and URL, or a
// team's slug.
type reviewerIdentityFields struct {
	Login   string `json:"login"`
	Slug    string `json:"slug"`
	ID      int64  `json:"id"`
	HTMLURL string `json:"html_url"`
}

// reviewerNode builds a reviewer TEAM node.
func reviewerNode(org, resourceType, id, kind, name string) (ghgraph.Resource, error) {
	content, err := marshalDetail("github-environments", name, reviewerDetail{Kind: kind, Name: name})
	if err != nil {
		return ghgraph.Resource{}, err
	}
	return ghgraph.Resource{
		ID:           id,
		Name:         name,
		ResourceType: resourceType,
		Content:      content,
		Metadata:     map[string]string{"org": org},
	}, nil
}
