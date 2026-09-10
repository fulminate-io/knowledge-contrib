// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/storage/armstorage"
)

// sub_storage.go — storage accounts and the file shares inside them.

// storageSub walks the subscription's storage accounts.
type storageSub struct{ subBase }

func (s *storageSub) Collect(ctx context.Context) (subResult, error) {
	accounts, err := armstorage.NewAccountsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("storage accounts client: %w", err)
	}
	shares, err := armstorage.NewFileSharesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("file shares client: %w", err)
	}

	var out subResult
	err = drain(ctx, accounts.NewListPager(nil), func(page armstorage.AccountsClientListResponse) error {
		for _, account := range page.Value {
			if account == nil || account.ID == nil {
				continue
			}
			r, err := storageAccountResource(account)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, storageAccountEdges(account)...)
			if err := s.collectFileShares(ctx, shares, account, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing storage accounts: %w", err)
	}
	return out, nil
}

func storageAccountResource(account *armstorage.Account) (resource, error) {
	content, err := marshalContent(account)
	if err != nil {
		return resource{}, fmt.Errorf("projecting storage account %s: %w", ptr(account.ID), err)
	}
	r := resource{
		id:           ptr(account.ID),
		name:         ptr(account.Name),
		resourceType: rtStorageAccount,
		region:       ptr(account.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if account.Kind != nil {
		r.metadata["kind"] = string(*account.Kind)
	}
	if account.SKU != nil && account.SKU.Name != nil {
		r.metadata["skuName"] = string(*account.SKU.Name)
	}
	if p := account.Properties; p != nil {
		if p.AccessTier != nil {
			r.metadata["accessTier"] = string(*p.AccessTier)
		}
		if p.EnableHTTPSTrafficOnly != nil {
			r.metadata["httpsTrafficOnly"] = strconv.FormatBool(*p.EnableHTTPSTrafficOnly)
		}
		if p.AllowBlobPublicAccess != nil {
			r.metadata["allowBlobPublicAccess"] = strconv.FormatBool(*p.AllowBlobPublicAccess)
		}
	}
	return r, nil
}

// storageAccountEdges draws the account's customer-managed key and the subnets
// its network rules admit.
func storageAccountEdges(account *armstorage.Account) []edge {
	if account.Properties == nil {
		return nil
	}
	id := ptr(account.ID)
	var out []edge
	if enc := account.Properties.Encryption; enc != nil && enc.KeyVaultProperties != nil {
		kvp := enc.KeyVaultProperties
		out = append(out, keyVaultKeyEdges(id, ptr(kvp.KeyVaultURI), ptr(kvp.KeyName), ptr(kvp.KeyVersion))...)
	}
	if rules := account.Properties.NetworkRuleSet; rules != nil {
		for _, rule := range rules.VirtualNetworkRules {
			if rule == nil {
				continue
			}
			// The field is named for a virtual network and holds a SUBNET id:
			// a storage network rule admits one subnet, not a whole network.
			if subnetID := ptr(rule.VirtualNetworkResourceID); subnetID != "" {
				out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
			}
		}
	}
	return out
}

// collectFileShares enumerates the file shares of one storage account.
func (s *storageSub) collectFileShares(
	ctx context.Context, client *armstorage.FileSharesClient, account *armstorage.Account, out *subResult,
) error {
	rg := armResourceGroup(ptr(account.ID))
	name := ptr(account.Name)
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListPager(rg, name, nil), func(page armstorage.FileSharesClientListResponse) error {
		for _, share := range page.Value {
			if share == nil || share.ID == nil {
				continue
			}
			r, err := fileShareResource(share, account)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, containsEdges(ptr(account.ID), ptr(share.ID))...)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing file shares of account %s: %w", name, err)
	}
	return nil
}

func fileShareResource(share *armstorage.FileShareItem, account *armstorage.Account) (resource, error) {
	content, err := marshalContent(share)
	if err != nil {
		return resource{}, fmt.Errorf("projecting file share %s: %w", ptr(share.ID), err)
	}
	r := resource{
		id:           ptr(share.ID),
		name:         ptr(share.Name),
		resourceType: rtFileShare,
		region:       ptr(account.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := share.Properties; p != nil {
		if p.ShareQuota != nil {
			r.metadata["shareQuotaGiB"] = strconv.Itoa(int(*p.ShareQuota))
		}
		if p.AccessTier != nil {
			r.metadata["accessTier"] = string(*p.AccessTier)
		}
		if p.EnabledProtocols != nil {
			r.metadata["enabledProtocols"] = string(*p.EnabledProtocols)
		}
	}
	return r, nil
}
