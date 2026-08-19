package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OrgRole struct {
	RoleID   string `json:"roleId,omitempty"`
	RoleName string `json:"roleName,omitempty"`
}

type OrgPrincipal struct {
	PrincipalID    string    `json:"principalId,omitempty"`
	PrincipalName  string    `json:"principalName,omitempty"`
	PrincipalEmail string    `json:"principalEmail,omitempty"`
	LastLogin      string    `json:"lastLogin,omitempty"`
	Roles          []OrgRole `json:"roles,omitempty"`
}

type OrgPrincipalsPage struct {
	Items         []OrgPrincipal `json:"items"`
	NextPageToken string         `json:"nextPageToken,omitempty"`
}

type OrgWalletFlags struct {
	SyncStartDateSEC int64 `json:"syncStartDateSEC,omitempty"`
}

type DetailedOrgWallet struct {
	ID                      string         `json:"id"`
	Name                    string         `json:"name"`
	Description             string         `json:"description,omitempty"`
	NetworkID               string         `json:"networkId,omitempty"`
	Address                 string         `json:"address,omitempty"`
	Addresses               []string       `json:"addresses,omitempty"`
	SubsidiaryID            string         `json:"subsidiaryId,omitempty"`
	CreatedSEC              int64          `json:"createdSEC,omitempty"`
	Disabled                bool           `json:"disabled"`
	Flags                   OrgWalletFlags `json:"flags,omitempty"`
	IsBalanceMonitoringOnly bool           `json:"isBalanceMonitoringOnly,omitempty"`
}

type TransactionCountFilters struct {
	WalletIDs []string `json:"walletIds,omitempty"`
	DateRange struct {
		From string `json:"from"`
		To   string `json:"to"`
	} `json:"dateRange"`
	IgnoredStatuses     []string `json:"ignoredStatuses,omitempty"`
	AmountCurrencyNames []string `json:"amountCurrencyNames,omitempty"`
}

type TransactionCount struct {
	All                 int    `json:"all"`
	NeedsCategorization int    `json:"needsCategorization"`
	ToBeReconciled      int    `json:"toBeReconciled"`
	FirstRecordDate     string `json:"firstRecordDate,omitempty"`
}

type ConnectionSyncStatus struct {
	Status               string          `json:"status,omitempty"`
	LastSyncCompletedSEC int64           `json:"lastSyncCompletedSEC,omitempty"`
	Errors               json.RawMessage `json:"errors,omitempty"`
	Warnings             json.RawMessage `json:"warnings,omitempty"`
	IsRunning            bool            `json:"isRunning"`
}

type ConnectionDetails struct {
	ID              string               `json:"id"`
	Provider        string               `json:"provider,omitempty"`
	Name            string               `json:"name,omitempty"`
	LastSyncSEC     int64                `json:"lastSyncSEC,omitempty"`
	IsSetupComplete bool                 `json:"isSetupComplete"`
	IsDisabled      bool                 `json:"isDisabled"`
	IsDeleted       bool                 `json:"isDeleted"`
	AccountCode     string               `json:"accountCode,omitempty"`
	FeeAccountCode  string               `json:"feeAccountCode,omitempty"`
	Status          string               `json:"status,omitempty"`
	SyncStatus      ConnectionSyncStatus `json:"syncStatus,omitempty"`
}

type HistoricPrice struct {
	TimestampSEC int64           `json:"timestampSEC"`
	Status       string          `json:"status,omitempty"`
	Price        json.RawMessage `json:"price,omitempty"`
	Steps        json.RawMessage `json:"steps,omitempty"`
}

type HistoricPricePage struct {
	HasMore       bool            `json:"hasMore"`
	NextPageToken string          `json:"nextPageToken,omitempty"`
	Prices        []HistoricPrice `json:"prices"`
}

func (c *Client) OrgPrincipals(ctx context.Context, orgID string, pageSize int) (*OrgPrincipalsPage, error) {
	if pageSize < 1 || pageSize > 1000 {
		return nil, fmt.Errorf("organization user page size must be between 1 and 1000")
	}
	query := url.Values{"pageSize": {strconv.Itoa(pageSize)}}
	base := strings.TrimSuffix(c.RulesQueryURL, "/graphql-reports")
	endpoint := strings.TrimRight(base, "/") + "/v3/orgs/" + url.PathEscape(orgID) + "/principals/aggregated?" + query.Encode()
	data, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return nil, err
	}
	var response OrgPrincipalsPage
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode organization users response: %w", err)
	}
	return &response, nil
}

func (c *Client) DetailedOrgWallets(ctx context.Context, orgID string) ([]DetailedOrgWallet, error) {
	base, err := c.rawServiceBase(APIServiceApp)
	if err != nil {
		return nil, err
	}
	query := url.Values{
		"loadBalances":          {"false"},
		"loadBalancesFairValue": {"false"},
		"excludeDisabled":       {"false"},
	}
	endpoint := strings.TrimRight(base, "/") + "/orgs/" + url.PathEscape(orgID) + "/wallets?" + query.Encode()
	data, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Items []DetailedOrgWallet `json:"items"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode detailed organization wallets response: %w", err)
	}
	return response.Items, nil
}

func (c *Client) SetOrgWalletDisabled(ctx context.Context, orgID, walletID, name string, disabled bool) error {
	request := map[string]any{
		"operationName": "UpdateWalletStatus",
		"query": `mutation UpdateWalletStatus($orgId: ID!, $walletId: ID!, $name: String, $disabled: Boolean, $addWallets: [String], $removeWallets: [String]) {
  updateWallet(orgId: $orgId, walletId: $walletId, name: $name, disabled: $disabled, addWallets: $addWallets, removeWallets: $removeWallets) { id }
}`,
		"variables": map[string]any{"orgId": orgID, "walletId": walletID, "name": name, "disabled": disabled, "addWallets": []string{}, "removeWallets": []string{}},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesMutationURL, request, true)
	if err != nil {
		return err
	}
	var response struct {
		Data struct {
			Wallet *struct {
				ID string `json:"id"`
			} `json:"updateWallet"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return fmt.Errorf("decode update wallet response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return err
	}
	if response.Data.Wallet == nil || response.Data.Wallet.ID == "" {
		return fmt.Errorf("update wallet response did not include a wallet id")
	}
	return nil
}

func (c *Client) OrganizationTokens(ctx context.Context, orgID string) ([]string, error) {
	query := url.Values{"fieldName": {"amountCurrencyName"}}
	endpoint := strings.TrimRight(c.TransactionsURL, "/") + "/orgs/" + url.PathEscape(orgID) + "/lookups?" + query.Encode()
	data, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Values []any `json:"values"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode organization token lookup response: %w", err)
	}
	values := make([]string, 0, len(response.Values))
	seen := map[string]bool{}
	for _, value := range response.Values {
		item := strings.TrimSpace(fmt.Sprint(value))
		if item != "" && !seen[item] {
			seen[item] = true
			values = append(values, item)
		}
	}
	return values, nil
}

func (c *Client) TransactionCount(ctx context.Context, orgID string, filters TransactionCountFilters) (*TransactionCount, error) {
	endpoint := strings.TrimRight(c.TransactionsURL, "/") + "/orgs/" + url.PathEscape(orgID) + "/transactions/summary_v2?requestContext=txn_load_summary"
	var response TransactionCount
	data, err := c.doEndpoint(ctx, http.MethodPost, endpoint, map[string]any{"filters": filters}, true)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode transaction count response: %w", err)
	}
	return &response, nil
}

func (c *Client) ConnectionDetails(ctx context.Context, orgID string) ([]ConnectionDetails, error) {
	request := map[string]any{
		"operationName": "GetConnections",
		"query": `query GetConnections($orgId: ID!) {
  connections(orgId: $orgId, overrideCache: true) {
    id provider lastSyncSEC isSetupComplete isDisabled isDeleted
    syncStatus { status lastSyncCompletedSEC errors warnings isRunning }
    name accountCode feeAccountCode status
  }
}`,
		"variables": map[string]any{"orgId": orgID},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesMutationURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Connections []ConnectionDetails `json:"connections"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode connections response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	return response.Data.Connections, nil
}

func (c *Client) PublicSymbol(ctx context.Context, symbol string) (json.RawMessage, error) {
	return c.publicJSON(ctx, c.AddressLookupURL+"/symbols/"+url.PathEscape(symbol))
}

func (c *Client) PublicActiveNetworks(ctx context.Context, address string) (json.RawMessage, error) {
	return c.publicJSON(ctx, c.AddressLookupURL+"/address/"+url.PathEscape(address)+"/active-networks")
}

func (c *Client) PublicContract(ctx context.Context, networkID, address string) (json.RawMessage, error) {
	return c.publicJSON(ctx, c.AddressLookupURL+"/v1/networks/"+url.PathEscape(networkID)+"/addresses/"+url.PathEscape(address))
}

func (c *Client) PublicBlockAt(ctx context.Context, networkID, datetime string) (json.RawMessage, error) {
	query := url.Values{"datetime": {datetime}}
	return c.publicJSON(ctx, c.AddressLookupURL+"/networks/"+url.PathEscape(networkID)+"/blocks?"+query.Encode())
}

func (c *Client) PublicBlockTime(ctx context.Context, networkID, blockNumber string) (json.RawMessage, error) {
	data, err := c.doEndpoint(ctx, http.MethodGet, c.AddressLookupURL+"/networks/"+url.PathEscape(networkID)+"/blocks/"+url.PathEscape(blockNumber), nil, false)
	if err != nil {
		return nil, err
	}
	var decoded any
	if json.Unmarshal(data, &decoded) == nil {
		var seconds int64
		switch value := decoded.(type) {
		case float64:
			seconds = int64(value)
		case map[string]any:
			for _, field := range []string{"timestampSEC", "timestamp"} {
				if number, ok := value[field].(float64); ok {
					seconds = int64(number)
					break
				}
			}
		}
		if seconds > 0 {
			return json.Marshal(map[string]any{"timestampSEC": seconds, "timestampISO": time.Unix(seconds, 0).UTC().Format(time.RFC3339)})
		}
	}
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil || seconds <= 0 {
		return nil, fmt.Errorf("block-time service returned an invalid timestamp")
	}
	return json.Marshal(map[string]any{"timestampSEC": seconds, "timestampISO": time.Unix(seconds, 0).UTC().Format(time.RFC3339)})
}

func (c *Client) PublicPrice(ctx context.Context, symbol string, timestampSEC int64, fiat string) (json.RawMessage, error) {
	query := url.Values{"fromSym": {symbol}, "timestampSEC": {strconv.FormatInt(timestampSEC, 10)}, "toFiat": {fiat}}
	return c.publicJSON(ctx, c.PriceLookupURL+"/price?"+query.Encode())
}

func (c *Client) publicJSON(ctx context.Context, endpoint string) (json.RawMessage, error) {
	data, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, false)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("public lookup response was not valid JSON")
	}
	return json.RawMessage(data), nil
}

const historicPriceQuery = `query getHistoricPrice($orgId: ID!, $coin: String!, $fromTimestampSec: Int, $toTimestampSec: Int, $pageSize: Int, $pageToken: String) {
  historicPrices(orgId: $orgId, coin: $coin, fromTimestampSec: $fromTimestampSec, toTimestampSec: $toTimestampSec, pageSize: $pageSize, pageToken: $pageToken) {
    hasMore nextPageToken
    prices {
      ... on HistoricPriceSuccess {
        timestampSEC status
        price {
          type
          ... on PriceDetailCandlestick { price open close high low volume }
          ... on PriceDetailOverride { price }
          ... on PriceDetailCoarseGrain { price }
        }
        steps { detail description source type }
      }
      ... on HistoricPriceFailure { timestampSEC status steps { detail description source type } }
    }
  }
}`

func (c *Client) HistoricPrices(ctx context.Context, orgID, coin string, fromSEC, toSEC int64, pageSize int, pageToken string) (*HistoricPricePage, error) {
	request := map[string]any{
		"operationName": "getHistoricPrice",
		"query":         historicPriceQuery,
		"variables":     map[string]any{"orgId": orgID, "coin": coin, "fromTimestampSec": fromSEC, "toTimestampSec": toSEC, "pageSize": pageSize, "pageToken": pageToken},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesMutationURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Page HistoricPricePage `json:"historicPrices"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode historic prices response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	return &response.Data.Page, nil
}

func (c *Client) DashboardBalance(ctx context.Context, orgID, viewID, asOf string) (json.RawMessage, error) {
	base, err := c.rawServiceBase(APIServiceApp)
	if err != nil {
		return nil, err
	}
	query := url.Values{"asOf": {asOf}, "groupByInventory": {"false"}, "includeSubsidiary": {"false"}}
	endpoint := strings.TrimRight(base, "/") + "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(viewID) + "/balance-paginated?" + query.Encode()
	data, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("dashboard balance response was not valid JSON")
	}
	return json.RawMessage(data), nil
}

func (c *Client) AssetBalanceReport(ctx context.Context, orgID, asOf, currency string) (json.RawMessage, error) {
	reportsURL := strings.TrimSuffix(c.RulesQueryURL, "/graphql-reports") + "/graphql"
	request := map[string]any{
		"operationName": "RunAssetBalanceReport",
		"query":         `mutation RunAssetBalanceReport($orgId: ID!, $reportDetails: ReportDetailsInput!) { runReport(orgId: $orgId, reportDetails: $reportDetails) { data } }`,
		"variables": map[string]any{
			"orgId": orgID,
			"reportDetails": map[string]any{
				"title":         "Balance Report as of EOD " + asOf,
				"saveReport":    false,
				"balanceReport": map[string]any{"balanceOnDate": asOf, "currency": currency, "includeIgnored": false, "emailReport": "", "excludeNft": true, "exportTokenAddresses": false, "reCheckDeFi": false, "returnEmptyBalances": false, "skipPricing": true, "skipPricingSpam": true},
			},
		},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, reportsURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			RunReport struct {
				Data json.RawMessage `json:"data"`
			} `json:"runReport"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode balance comparison report: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	if len(response.Data.RunReport.Data) == 0 || string(response.Data.RunReport.Data) == "null" {
		return nil, fmt.Errorf("balance comparison report returned no data")
	}
	return response.Data.RunReport.Data, nil
}
