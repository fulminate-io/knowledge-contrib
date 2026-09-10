// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// walk_config.go — ConfigMaps, Secrets, ServiceAccounts and the four RBAC
// kinds.
//
// THE SECRET WALKER IS THE ONE WITH A SECURITY OBLIGATION, and it is stated at
// its own function rather than here.

func walkConfigMaps(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().ConfigMaps("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "ConfigMap", err)
	}
	noteContinue(w, "ConfigMap", list.Continue)
	for i := range list.Items {
		cm := &list.Items[i]
		if err := add(w, metaOf(cm.ObjectMeta), "ConfigMap", map[string]string{
			"key_count": itoa(int32(len(cm.Data))),
			"key_names": strings.Join(sortedUnique(mapKeys(cm.Data)), ","),
		}, cm); err != nil {
			return err
		}
	}
	return nil
}

// walkSecrets enumerates Secrets THROUGH AN ALLOWLIST OF WHAT MAY REACH THE
// GRAPH, which is secret.go's subject and is documented there in full.
//
// THE SHORT VERSION, AND WHY IT IS NOT A STRIP LIST. This walker used to nil
// Data and StringData on a copy of the object and marshal the rest. That was
// defeated by the most ordinary Secret in any cluster: one created with
// `kubectl apply` carries the whole submitted manifest, data block included, in
// an annotation, and the value reached both the node body and a metadata value.
// The graph a collect lands in is summarized, embedded and synced, so that is a
// credential in a searchable, replicated store. Now nothing reaches the node
// except the fields secret.go names one by one.
func walkSecrets(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().Secrets("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Secret", err)
	}
	noteContinue(w, "Secret", list.Continue)
	for i := range list.Items {
		s := &list.Items[i]
		keys := sortedUnique(append(mapKeys(s.Data), mapKeys(s.StringData)...))

		body, meta := redactSecret(s, keys)

		if err := add(w, meta, "Secret", map[string]string{
			"secret_type": string(s.Type),
			"key_names":   strings.Join(keys, ","),
			"key_count":   itoa(int32(len(keys))),
		}, body); err != nil {
			return err
		}
	}
	return nil
}

// The three cloud workload-identity annotations, lifted to first-class metadata
// keys by the ServiceAccount walker.
//
// THEY ARE READ IN ONE PLACE ON PURPOSE. Both the ASSUMES_IDENTITY proxy family
// and the WORKLOAD_IDENTITY linkage family key on these three bindings, and
// three provider-specific annotation names read in several places is exactly
// how one provider quietly stops working while the other two keep passing.
const (
	annotationIRSARoleARN    = "eks.amazonaws.com/role-arn"
	annotationGCPServiceAcct = "iam.gke.io/gcp-service-account"
	annotationAzureClientID  = "azure.workload.identity/client-id"
	metaKeyIRSARoleARN       = "irsa_role_arn"
	metaKeyGCPServiceAccount = "gcp_service_account"
	metaKeyAzureClientID     = "azure_client_id"
)

func walkServiceAccounts(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.CoreV1().ServiceAccounts("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "ServiceAccount", err)
	}
	noteContinue(w, "ServiceAccount", list.Continue)
	for i := range list.Items {
		sa := &list.Items[i]
		extra := map[string]string{}
		if v := sa.Annotations[annotationIRSARoleARN]; v != "" {
			extra[metaKeyIRSARoleARN] = v
		}
		if v := sa.Annotations[annotationGCPServiceAcct]; v != "" {
			extra[metaKeyGCPServiceAccount] = v
		}
		if v := sa.Annotations[annotationAzureClientID]; v != "" {
			extra[metaKeyAzureClientID] = v
		}
		if err := add(w, metaOf(sa.ObjectMeta), "ServiceAccount", extra, sa); err != nil {
			return err
		}
		id := resourceID(sa.Namespace, "ServiceAccount", sa.Name)
		for _, ref := range sa.Secrets {
			if ref.Name == "" {
				continue
			}
			w.addEdges(Edge{FromID: id, ToID: resourceID(sa.Namespace, "Secret", ref.Name), Type: edgeMountsSecret})
		}
		for _, ref := range sa.ImagePullSecrets {
			if ref.Name == "" {
				continue
			}
			w.addEdges(Edge{
				FromID: id,
				ToID:   resourceID(sa.Namespace, "Secret", ref.Name),
				Type:   edgeMountsSecret,
				Method: "imagePullSecret",
			})
		}
	}
	return nil
}

func walkRoles(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.RbacV1().Roles("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "Role", err)
	}
	noteContinue(w, "Role", list.Continue)
	for i := range list.Items {
		r := &list.Items[i]
		if err := add(w, metaOf(r.ObjectMeta), "Role", map[string]string{
			"rule_count": itoa(int32(len(r.Rules))),
		}, r); err != nil {
			return err
		}
	}
	return nil
}

func walkClusterRoles(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.RbacV1().ClusterRoles().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "ClusterRole", err)
	}
	noteContinue(w, "ClusterRole", list.Continue)
	for i := range list.Items {
		r := &list.Items[i]
		if err := add(w, metaOf(r.ObjectMeta), "ClusterRole", map[string]string{
			"rule_count": itoa(int32(len(r.Rules))),
		}, r); err != nil {
			return err
		}
	}
	return nil
}

func walkRoleBindings(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.RbacV1().RoleBindings("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "RoleBinding", err)
	}
	noteContinue(w, "RoleBinding", list.Continue)
	for i := range list.Items {
		rb := &list.Items[i]
		if err := add(w, metaOf(rb.ObjectMeta), "RoleBinding", map[string]string{
			"role_ref_kind": rb.RoleRef.Kind,
			"role_ref_name": rb.RoleRef.Name,
		}, rb); err != nil {
			return err
		}
		w.addEdges(roleBindingEdges(
			resourceID(rb.Namespace, "RoleBinding", rb.Name), rb.Namespace, rb.RoleRef, rb.Subjects)...)
	}
	return nil
}

func walkClusterRoleBindings(ctx context.Context, c clientBundle, w *walkResult) error {
	list, err := c.typed.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listErr(w, "ClusterRoleBinding", err)
	}
	noteContinue(w, "ClusterRoleBinding", list.Continue)
	for i := range list.Items {
		crb := &list.Items[i]
		if err := add(w, metaOf(crb.ObjectMeta), "ClusterRoleBinding", map[string]string{
			"role_ref_kind": crb.RoleRef.Kind,
			"role_ref_name": crb.RoleRef.Name,
		}, crb); err != nil {
			return err
		}
		w.addEdges(roleBindingEdges(
			resourceID("", "ClusterRoleBinding", crb.Name), "", crb.RoleRef, crb.Subjects)...)
	}
	return nil
}

// roleBindingEdges renders the two edges a binding states.
//
// BINDS_SUBJECT IS SERVICE-ACCOUNT-ONLY, AND THAT IS THE GATE. A binding to a
// User or a Group names an identity that is not an object in this cluster, so
// there is no node to point at and no edge is emitted — a fixture using a User
// subject must produce a BINDS_ROLE edge and NO BINDS_SUBJECT edge.
//
// THE ROLE REFERENCE'S SCOPE IS NOT THE BINDING'S. A namespaced RoleBinding may
// bind a ClusterRole, and its target id then carries no namespace; building
// every role target in the binding's namespace points half of them at nothing.
func roleBindingEdges(bindingID, namespace string, ref rbacv1.RoleRef, subjects []rbacv1.Subject) []Edge {
	var out []Edge
	if ref.Name != "" {
		kind := ref.Kind
		if kind == "" {
			kind = "Role"
		}
		roleNamespace := namespace
		if clusterScopedKinds[kind] {
			roleNamespace = ""
		}
		out = append(out, Edge{
			FromID: bindingID,
			ToID:   resourceID(roleNamespace, kind, ref.Name),
			Type:   edgeBindsRole,
		})
	}
	for _, s := range subjects {
		if s.Kind != "ServiceAccount" || s.Name == "" {
			continue
		}
		ns := s.Namespace
		if ns == "" {
			ns = namespace
		}
		if ns == "" {
			// A ServiceAccount subject with no namespace, in a cluster-scoped
			// binding, names no resolvable object.
			continue
		}
		out = append(out, Edge{
			FromID: bindingID,
			ToID:   resourceID(ns, "ServiceAccount", s.Name),
			Type:   edgeBindsSubject,
		})
	}
	return out
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
