// SPDX-License-Identifier: Apache-2.0

package collect

import (
	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// user.go — THE PERSON A WALKED OBJECT NAMES, and the ONE place a user node is
// built.
//
// FOUR FIELDS ACROSS THE WALK NAME A PERSON: an environment's required reviewers,
// a workflow run's actor, a workflow run's triggering actor, and a deployment's
// creator. Each of them rides a response this collector already fetches, so the
// whole class costs no additional call to the source provider — and the source
// provider itself dropped all four.
//
// WHY ONE CONSTRUCTOR AND NOT A LITERAL AT EACH SITE. The same login arrives from
// more than one enumeration: a person who reviews a deployment also runs
// workflows. The graph builder deduplicates on the id and keeps one copy, so four
// sites building the node four ways would put four subtly different nodes into
// the race and let the winner depend on which goroutine finished. One constructor
// makes the node a function of what the provider said about the person and of
// nothing else.
//
// WHAT IT REFUSES TO CARRY. The provider's user object has an `email` field and a
// run's head commit has an author with another; a person's address is not
// inventory of an organization's CI/CD and this collector reads neither. The
// allowlist below is the whole node, and a test asserts over the raw bytes of a
// whole collect that no address reaches any of them.

// providerUser is the identity a walked object names a person by.
//
// IT IS AN ALLOWLIST AND THAT IS ITS WHOLE PURPOSE. The provider's own user type
// carries some forty fields, an address among them, and a converter that passed
// that value along would carry every one of them into the graph — including any
// added by a later version of the provider's API, silently. This struct names the
// four this collector keeps, so a field reaches a node only by being written here.
type providerUser struct {
	// Login is the provider's own spelling of the person's name, and the id the
	// node is keyed on: see [ghgraph.UserID].
	Login string
	// ID is the provider's numeric identity. It is STABLE ACROSS A RENAME where
	// the login is not, so it is carried in the node's detail — a consumer that
	// needs to follow a person through a rename has it, and the id spelling every
	// existing traversal names is unchanged.
	ID int64
	// Kind is `User` or `Bot`. A bot's activity is a materially different fact
	// about a pipeline from a person's, and the two are indistinguishable by
	// login alone once an organization has more than one bot.
	Kind string
	// HTMLURL is where a reader goes to see the person on the provider.
	HTMLURL string
}

// userFrom reads the allowlist off the provider's user object.
//
// EVERY READ GOES THROUGH THE SDK'S NIL-SAFE ACCESSORS, so an absent user and an
// absent field are both the zero value rather than a panic; an absent login is
// what [userResource] turns into "there is nobody to point at".
func userFrom(user *gogithub.User) providerUser {
	return providerUser{
		Login:   user.GetLogin(),
		ID:      user.GetID(),
		Kind:    user.GetType(),
		HTMLURL: user.GetHTMLURL(),
	}
}

// userDetail is the user node's Content.
//
// ITS FIRST TWO FIELDS AND THEIR ORDER ARE THE REVIEWER NODE'S, carried verbatim:
// a consumer that already stores this collector's reviewer nodes reads `kind` and
// `name`, and the two fields this round adds are appended rather than woven in.
// The optional ones are omitted when the provider named none, so a user the
// provider described sparsely produces a shorter document rather than a document
// full of empty strings.
type userDetail struct {
	Kind    string `json:"kind,omitempty"`
	Name    string `json:"name"`
	ID      int64  `json:"id,omitempty"`
	HTMLURL string `json:"html_url,omitempty"`
}

// userResource converts one named person into the node every edge that names them
// points at.
//
// IT RETURNS THE ZERO RESOURCE AND A NIL ERROR WHEN THERE IS NOBODY TO POINT AT,
// which is the convention the reviewer reader already established and the caller
// already reads: an object whose person field is absent, or carries no login, is
// an object that names nobody. That is not a failure — a scheduled run has no
// person behind it — and a node keyed on the empty login would be one node
// standing for every anonymous object in the organization.
func userResource(org string, user providerUser) (ghgraph.Resource, error) {
	if user.Login == "" {
		return ghgraph.Resource{}, nil
	}
	content, err := marshalDetail("github-users", user.Login, userDetail{
		Kind:    user.Kind,
		Name:    user.Login,
		ID:      user.ID,
		HTMLURL: user.HTMLURL,
	})
	if err != nil {
		return ghgraph.Resource{}, err
	}
	return ghgraph.Resource{
		ID:           ghgraph.UserID(org, user.Login),
		Name:         user.Login,
		ResourceType: ghgraph.ResourceTypeUser,
		Content:      content,
		Metadata:     map[string]string{"org": org},
	}, nil
}

// namedUser is one person a converter found and the class of edge that names
// them, so a converter with more than one such field emits them in one loop
// rather than in two blocks that can drift apart.
type namedUser struct {
	user     *gogithub.User
	edgeType string
}

// userRelations mints the node for each named person and the edge from the object
// that named them, skipping the ones that name nobody.
func userRelations(org, fromID string, named []namedUser) (ghgraph.Result, error) {
	var out ghgraph.Result
	for _, one := range named {
		resource, err := userResource(org, userFrom(one.user))
		if err != nil {
			return ghgraph.Result{}, err
		}
		if resource.ID == "" {
			// The provider named nobody in this field. No node and no edge: an
			// edge to a guessed id would resolve to nothing while looking exactly
			// like one that resolves.
			continue
		}
		out.Resources = append(out.Resources, resource)
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID: fromID,
			ToID:   resource.ID,
			Type:   one.edgeType,
		})
	}
	return out, nil
}
