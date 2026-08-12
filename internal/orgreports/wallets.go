package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

const orgWalletFields = `id name description type networkId address addresses subsidiaryId isBalanceMonitoringOnly metadata`

const orgWalletsQuery = `query OrgWallets($orgId: ID!) {
  wallets(orgId: $orgId) { ` + orgWalletFields + ` }
}`

const createOrgWalletMutation = `mutation CreateOrgWallet($orgId: ID!, $wallet: WalletInput!, $prems: [WalletPermissionInput]!) {
  createWallet(orgId: $orgId, wallet: $wallet, prems: $prems) { ` + orgWalletFields + ` }
}`

// OrgWallet is the complete discovery-safe wallet shape returned by the org
// GraphQL API. Address and Addresses are both retained because legacy watch
// wallets and modern account-based wallets expose different fields.
type OrgWallet struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Description             string         `json:"description,omitempty"`
	Type                    int            `json:"type,omitempty"`
	NetworkID               string         `json:"networkId,omitempty"`
	Address                 string         `json:"address,omitempty"`
	Addresses               []string       `json:"addresses,omitempty"`
	SubsidiaryID            string         `json:"subsidiaryId,omitempty"`
	IsBalanceMonitoringOnly bool           `json:"isBalanceMonitoringOnly,omitempty"`
	Metadata                map[string]any `json:"metadata,omitempty"`
}

type WalletPermission struct {
	UserID string `json:"userId"`
	Role   int    `json:"role"`
}

func (c *Client) OrgWallets(ctx context.Context, orgID string) ([]OrgWallet, error) {
	request := map[string]any{
		"operationName": "OrgWallets",
		"query":         orgWalletsQuery,
		"variables":     map[string]any{"orgId": orgID},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesMutationURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Wallets []OrgWallet `json:"wallets"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode organization wallets response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	return response.Data.Wallets, nil
}

func (c *Client) CreateOrgWallet(ctx context.Context, orgID string, wallet map[string]any, permissions []WalletPermission) (*OrgWallet, error) {
	if permissions == nil {
		// GraphQL declares prems as a non-null list. A nil Go slice would encode
		// as null and fail validation before the resolver; an empty list is valid.
		permissions = []WalletPermission{}
	}
	request := map[string]any{
		"operationName": "CreateOrgWallet",
		"query":         createOrgWalletMutation,
		"variables": map[string]any{
			"orgId":  orgID,
			"wallet": wallet,
			"prems":  permissions,
		},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesMutationURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Wallet *OrgWallet `json:"createWallet"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode create organization wallet response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	if response.Data.Wallet == nil || response.Data.Wallet.ID == "" {
		return nil, fmt.Errorf("create wallet response did not include a wallet id")
	}
	return response.Data.Wallet, nil
}
