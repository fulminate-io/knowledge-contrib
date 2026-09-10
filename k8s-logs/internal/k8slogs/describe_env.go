// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"slices"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe_env.go — WHAT AN INSTALLER MUST DO WITH EVERY ENVIRONMENT NAME THIS
// COLLECTOR READS, and the describe tool's environment declaration built from
// it.
//
// THE DECISION LIVES HERE, BESIDE THE COLLECTOR THAT READS THE NAME. It used to
// live in the installer as a hand-written per-collector table and in a
// disposition file beside it; both are now DERIVED from this map, so a variable
// this collector starts reading is one decision in one place rather than three
// edits in three repositories' worth of files. The disposition file remains the
// frozen review record, and the environment census pins this map against it.
//
// THE FOUR DISPOSITIONS ARE THE CONTRACT'S OWN, and each says what an installed
// entry carries: a path literal, a selector literal when set, a secret in NO
// state whatsoever, and not-carried for a name this collector reads that an
// installed entry deliberately does not declare. Declaring the last kind is the
// point: an operator whose variable is absent can tell a decision from an
// oversight.
// EnvDispositions is that decision, one row per name.
var EnvDispositions = map[string]string{
	"HOME":                    framework.EnvClassPath,
	"HOMEDRIVE":               framework.EnvClassNotCarried,
	"HOMEPATH":                framework.EnvClassNotCarried,
	"KUBECONFIG":              framework.EnvClassPath,
	"KUBERNETES_SERVICE_HOST": framework.EnvClassNotCarried,
	"KUBERNETES_SERVICE_PORT": framework.EnvClassNotCarried,
	"PATH":                    framework.EnvClassPath,
	"SYSTEMROOT":              framework.EnvClassNotCarried,
	"USERPROFILE":             framework.EnvClassNotCarried,
}

// EmptySensitiveNames are the names this collector tells PRESENT AND EMPTY apart
// from ABSENT. There is exactly one, and it is the one the resolver branches on.
//
// WHY THE SERVICE-HOST NAME IS DIFFERENT FROM THE OTHER EIGHT. kubeconfig.go
// reads it with os.LookupEnv and branches on PRESENCE, not on value, so a name
// set to the empty string takes the in-pod arm and produces the in-cluster
// refusal — byte-identical to what a real value produces and different from what
// absence produces. That distinction is the whole point of the branch: a pod with
// a broken service-account mount must not read like a laptop with no kubeconfig.
//
// WHY THAT MAKES IT UNSAFE IN A WORKED ENTRY. A `${NAME:-}` reference resolves to
// the empty string in the process serving the collect, so an operator who copies
// such an entry gets a collector that believes it is in a pod. The installer's
// documentation gate refuses the defaulted form for a marked name and admits it
// for the rest, and this is where it reads the answer.
//
// IT IS A not-carried NAME, and that pairing is why the mark travels on its own
// answer rather than on the installer's class table: that table drops this class
// by construction, so this mark would have nowhere to go.
// empty_sensitive_test.go drives all three states and fails if this set and the
// measured one disagree in either direction.
var EmptySensitiveNames = map[string]bool{
	"KUBERNETES_SERVICE_HOST": true,
}

// DescribedEnvironment renders the describe tool's environment declaration: every
// name above, sorted, each with its disposition and its empty-sensitivity.
//
// SORTED so one unchanged collector renders one byte-identical declaration, and
// so the installer table generated from it does not reorder itself between runs.
func DescribedEnvironment() []framework.EnvDeclaration {
	names := make([]string, 0, len(EnvDispositions))
	for name := range EnvDispositions {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{
			Name:           name,
			Class:          EnvDispositions[name],
			EmptySensitive: EmptySensitiveNames[name],
		})
	}
	return out
}
