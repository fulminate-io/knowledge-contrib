// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// secret_test.go — THE SECRET WALKER'S OWN FILE, split out of walk_test.go
// because its input classes are a subject rather than one more converter row.
//
// A Secret is the one kind whose CONTRIBUTION IS AN ALLOWLIST rather than a
// conversion: the graph a collect lands in is indexed and replicated, so what
// may reach it is decided name by name in secret.go and asserted here against
// the shapes a real cluster produces.

// TestWalk_SecretValuesNeverReachTheGraph is the security assertion on the
// Secret walker, driven over THE INPUT CLASSES A REAL CLUSTER PRODUCES.
//
// THE FIRST VERSION OF THIS TEST WAS DEFEATED BY AN ORDINARY SECRET. Its
// fixture carried no annotations, so it never saw the path that actually
// leaks: a Secret created with `kubectl apply` carries the whole submitted
// manifest, base64 data block included, in
// kubectl.kubernetes.io/last-applied-configuration, and the shared
// annotation-to-metadata lift put it in a metadata value as well as in the
// body. Both were measured on this module's own walk. The fixtures below carry
// that annotation, a managed-fields entry, and StringData, and each asserts
// absence in BOTH spellings across the body and every metadata value.
//
// THE ASSERTION REPORTS A NON-REVERSIBLE PROPERTY. It never prints a value; it
// asserts that neither spelling of one appears anywhere on the node.
func TestWalk_SecretValuesNeverReachTheGraph(t *testing.T) {
	const password = "hunter2-this-value-must-not-appear-anywhere"
	const username = "admin-user-also-must-not-appear"
	const tokenValue = "stringdata-token-must-not-appear"

	encodedPassword := base64.StdEncoding.EncodeToString([]byte(password))
	encodedUsername := base64.StdEncoding.EncodeToString([]byte(username))

	// What `kubectl apply` leaves behind: the entire manifest, data and all.
	lastApplied := `{"apiVersion":"v1","kind":"Secret","metadata":{"name":"db-creds",` +
		`"namespace":"prod"},"type":"Opaque","data":{"password":"` + encodedPassword +
		`","username":"` + encodedUsername + `"}}`

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db-creds",
			Namespace: "prod",
			Labels:    map[string]string{"app": "db"},
			Annotations: map[string]string{
				"kubectl.kubernetes.io/last-applied-configuration": lastApplied,
				// An annotation on the allowlist, which MUST survive: it names a
				// ServiceAccount rather than carrying material.
				"kubernetes.io/service-account.name": "db-sa",
				// A controller's annotation echoing a value back, which stands
				// for every path nobody has thought of yet.
				"vendor.example.com/last-synced-payload": `{"password":"` + password + `"}`,
			},
			ManagedFields: []metav1.ManagedFieldsEntry{{
				Manager:    "kubectl-client-side-apply",
				Operation:  metav1.ManagedFieldsOperationUpdate,
				FieldsType: "FieldsV1",
				FieldsV1: &metav1.FieldsV1{
					Raw: []byte(`{"f:data":{"f:password":{}},"f:metadata":{"f:annotations":{` +
						`"f:kubectl.kubernetes.io/last-applied-configuration":{}}}}`),
				},
			}},
		},
		Type:       corev1.SecretTypeOpaque,
		Data:       map[string][]byte{"password": []byte(password), "username": []byte(username)},
		StringData: map[string]string{"token": tokenValue},
	}

	nodes, _, _ := runWalk(t, newFakeBundle(t, []runtime.Object{secret}, nil, emptyDynamic()))
	got := nodeByID(t, nodes, "prod/Secret/db-creds")

	// THE SHAPE IS KEPT: names and counts, which are non-reversible.
	assert.Equal(t, "password,token,username", got.Metadata["key_names"])
	assert.Equal(t, "3", got.Metadata["key_count"])
	assert.Equal(t, "Opaque", got.Metadata["secret_type"])
	assert.Equal(t, "db", got.Metadata["label/app"], "labels are kept")
	assert.Equal(t, "db-sa", got.Metadata["annotation/kubernetes.io/service-account.name"],
		"the allowlisted annotation names a ServiceAccount and survives")

	// THE VALUES ARE NOT, IN EITHER SPELLING, ANYWHERE ON THE NODE.
	forbidden := []string{
		password, encodedPassword,
		username, encodedUsername,
		tokenValue, base64.StdEncoding.EncodeToString([]byte(tokenValue)),
	}
	for _, spelling := range forbidden {
		require.NotEmpty(t, spelling)
		assert.NotContains(t, got.Content, spelling,
			"a secret value must never reach the node body, in any encoding")
		for k, v := range got.Metadata {
			assert.NotContains(t, v, spelling,
				"a secret value must never reach metadata key %q", k)
		}
	}

	// AND THE TWO CARRIERS THAT DEFEATED THE FIRST VERSION ARE GONE WHOLE, not
	// merely value-free: the apply annotation and the managed-fields entry.
	assert.NotContains(t, got.Metadata, "annotation/kubectl.kubernetes.io/last-applied-configuration",
		"the apply annotation is dropped rather than filtered")
	assert.NotContains(t, got.Metadata, "annotation/vendor.example.com/last-synced-payload",
		"an annotation nobody allowlisted is dropped, whatever it carries")
	assert.NotContains(t, got.Content, "managedFields",
		"managed fields are absent from the body by construction")
	assert.NotContains(t, got.Content, "last-applied-configuration")
}

// TestWalk_SecretAllowlistIsExhaustive pins the allowlist itself. Growing it is
// a deliberate decision that the new name carries no credential material, and
// this test is where that decision is recorded.
func TestWalk_SecretAllowlistIsExhaustive(t *testing.T) {
	assert.Equal(t, map[string]bool{
		"kubernetes.io/service-account.name": true,
		"kubernetes.io/service-account.uid":  true,
	}, secretAnnotationAllowlist,
		"every allowlisted annotation carries a NAME or an identifier, never secret material")
}

// TestWalk_SecretWithNoAnnotationsStillConverts is the control: the allowlist
// does not depend on an annotation being present, and the ordinary Secret still
// produces a node with its shape intact.
func TestWalk_SecretWithNoAnnotationsStillConverts(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "plain", Namespace: "prod"},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data:       map[string][]byte{".dockerconfigjson": []byte("{}")},
	}
	nodes, _, _ := runWalk(t, newFakeBundle(t, []runtime.Object{secret}, nil, emptyDynamic()))
	got := nodeByID(t, nodes, "prod/Secret/plain")
	assert.Equal(t, ".dockerconfigjson", got.Metadata["key_names"])
	assert.Equal(t, "kubernetes.io/dockerconfigjson", got.Metadata["secret_type"])
	assert.Contains(t, got.Content, `"name":"plain"`, "identity survives the allowlist")
}
