// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appservice/armappservice/v4"
)

// sub_certificates.go — App Service certificates, where they are stored and who
// issued them.
//
// KEY VAULT'S OWN CERTIFICATES ARE NOT COLLECTED. They live on the vault's data
// plane, which needs a per-vault authorization this collector does not ask for
// and a separate SDK; the certificates here are the ARM-level ones an
// application gateway or a site binding points at.

// certificateAuthorityID names an issuer, which is not an Azure resource.
func certificateAuthorityID(issuer string) string { return "azure:ca/" + issuer }

// certificateSub walks the subscription's App Service certificates.
type certificateSub struct{ subBase }

func (s *certificateSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armappservice.NewCertificatesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("certificates client: %w", err)
	}

	var out subResult
	// One authority serves many certificates; it gets one node.
	seen := map[string]bool{}

	err = drain(ctx, client.NewListPager(nil), func(page armappservice.CertificatesClientListResponse) error {
		for _, cert := range page.Value {
			if cert == nil || cert.ID == nil {
				continue
			}
			r, err := certificateResource(cert)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			edges, authorities := certificateEdges(cert, s.subscriptionID, seen)
			out.edges = append(out.edges, edges...)
			out.resources = append(out.resources, authorities...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing certificates: %w", err)
	}
	return out, nil
}

func certificateResource(cert *armappservice.AppCertificate) (resource, error) {
	content, err := marshalContent(cert)
	if err != nil {
		return resource{}, fmt.Errorf("projecting certificate %s: %w", ptr(cert.ID), err)
	}
	r := resource{
		id:           ptr(cert.ID),
		name:         ptr(cert.Name),
		resourceType: rtWebCertificate,
		region:       ptr(cert.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := cert.Properties; p != nil {
		setIfNotEmpty(r.metadata, "thumbprint", ptr(p.Thumbprint))
		setIfNotEmpty(r.metadata, "subjectName", ptr(p.SubjectName))
		setIfNotEmpty(r.metadata, "issuer", ptr(p.Issuer))
		if p.ExpirationDate != nil {
			// A fixed format, so two collects of an unchanged certificate
			// render the same string.
			r.metadata["expirationDate"] = p.ExpirationDate.UTC().Format("2006-01-02T15:04:05Z")
		}
		if p.Valid != nil {
			r.metadata["valid"] = strconv.FormatBool(*p.Valid)
		}
		if p.KeyVaultSecretStatus != nil {
			r.metadata["keyVaultSecretStatus"] = string(*p.KeyVaultSecretStatus)
		}
	}
	return r, nil
}

// certificateEdges draws where a certificate is stored, what key protects it
// and who issued it.
//
// STORAGE AND ENCRYPTION ARE TWO DIFFERENT EDGES to two different targets: the
// vault holds the certificate, and the SECRET inside the vault is the material.
// A reader asking "which vault would I lose this with" wants the first; one
// asking "what exactly is the material" wants the second.
func certificateEdges(cert *armappservice.AppCertificate, subscriptionID string, seen map[string]bool) ([]edge, []resource) {
	if cert.Properties == nil {
		return nil, nil
	}
	id := ptr(cert.ID)
	var edges []edge
	var authorities []resource

	if vaultID := ptr(cert.Properties.KeyVaultID); vaultID != "" {
		edges = append(edges, edge{from: id, to: vaultID, relation: edgeStoredIn})
		if secret := ptr(cert.Properties.KeyVaultSecretName); secret != "" {
			edges = append(edges, edge{from: id, to: vaultID + "/secrets/" + secret, relation: edgeEncryptsWith})
		}
	}

	if issuer := ptr(cert.Properties.Issuer); issuer != "" {
		caID := certificateAuthorityID(issuer)
		edges = append(edges, edge{from: id, to: caID, relation: edgeIssuedBy})
		if !seen[caID] {
			seen[caID] = true
			authorities = append(authorities, proxy(caID, issuer, rtCertAuthority,
				"a certificate authority is not an Azure resource and has no ARM id",
				"app service certificate issuer", subscriptionID))
		}
	}
	return edges, authorities
}
