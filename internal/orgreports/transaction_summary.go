package orgreports

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type TransactionSummaryAddressRecord struct {
	WalletID                    string  `json:"walletId"`
	InteractingAddress          string  `json:"interactingAddress"`
	DepositsTransactionCount    int     `json:"depositsTxnsCount"`
	DepositsUncategorized       int     `json:"depositsUncategorized"`
	DepositsFMV                 float64 `json:"depositsFmv"`
	WithdrawalsTransactionCount int     `json:"withdrawalsTxnsCount"`
	WithdrawalsUncategorized    int     `json:"withdrawalsUncategorized"`
	WithdrawalsFMV              float64 `json:"withdrawalsFmv"`
}

type TransactionSummaryAsset struct {
	AssetID   string `json:"assetId"`
	AssetName string `json:"assetName"`
}

type TransactionSummaryWalletRecord struct {
	Wallet                      string  `json:"wallet"`
	WalletID                    string  `json:"walletId"`
	InteractingAddressesCount   int     `json:"interactingAddressesCount"`
	DepositsTransactionCount    int     `json:"depositsTxnsCount"`
	DepositsUncategorized       int     `json:"depositsUncategorized"`
	DepositsFMV                 float64 `json:"depositsFmv"`
	WithdrawalsTransactionCount int     `json:"withdrawalsTxnsCount"`
	WithdrawalsUncategorized    int     `json:"withdrawalsUncategorized"`
	WithdrawalsFMV              float64 `json:"withdrawalsFmv"`
	TotalTransactionCount       int     `json:"totalTxnsCount"`
	TotalUncategorized          int     `json:"totalUncategorized"`
	TotalUnreconciled           int     `json:"totalUnreconciled"`
	NetFMV                      float64 `json:"netFmv"`
	TotalFMV                    float64 `json:"totalFmv"`
}

// TransactionSummaryWallets reads the wallet-level main table used by the
// Transaction Summary dashboard. Unlike the interacting-address aggregate,
// this view is the inexpensive first pass for deciding which wallets need
// attention and supports the dashboard's date filter.
func (c *Client) TransactionSummaryWallets(ctx context.Context, orgID, from, to string, page, pageSize int) ([]TransactionSummaryWalletRecord, error) {
	if page < 1 || pageSize < 1 || pageSize > 500 {
		return nil, fmt.Errorf("invalid transaction summary pagination")
	}
	query := url.Values{}
	query.Set("datasource", "bigquery")
	query.Set("pagination[pageNumber]", strconv.Itoa(page))
	query.Set("pagination[pageSize]", strconv.Itoa(pageSize))
	filterIndex := 0
	for _, filter := range []struct {
		value, operator string
	}{{from, ">="}, {to, "<="}} {
		if filter.value == "" {
			continue
		}
		prefix := "base_filters[" + strconv.Itoa(filterIndex) + "]"
		query.Set(prefix+"[filterName]", "dateTime")
		query.Set(prefix+"[operator]", filter.operator)
		query.Set(prefix+"[value]", filter.value)
		filterIndex++
	}
	var response struct {
		Items []TransactionSummaryWalletRecord `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/main/records?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

// TransactionSummaryAddresses reads the same Interacting Addresses view used
// by Bitwave's Transaction Summary dashboard. The dashboard is the preferred
// discovery source because it aggregates recurring counterparties before any
// raw transaction sampling is sent to an LLM.
func (c *Client) TransactionSummaryAddresses(ctx context.Context, orgID string, page, pageSize int) ([]TransactionSummaryAddressRecord, error) {
	return c.TransactionSummaryAddressesFiltered(ctx, orgID, "", "", "", page, pageSize)
}

func (c *Client) TransactionSummaryAddressesFiltered(ctx context.Context, orgID, walletID, from, to string, page, pageSize int) ([]TransactionSummaryAddressRecord, error) {
	return c.TransactionSummaryAddressesFilteredSorted(ctx, orgID, walletID, from, to, "", page, pageSize)
}

func (c *Client) TransactionSummaryAddressesFilteredSorted(ctx context.Context, orgID, walletID, from, to, sortField string, page, pageSize int) ([]TransactionSummaryAddressRecord, error) {
	if page < 1 || pageSize < 1 || pageSize > 500 {
		return nil, fmt.Errorf("invalid transaction summary pagination")
	}
	query := url.Values{}
	query.Set("datasource", "bigquery")
	query.Set("pagination[pageNumber]", strconv.Itoa(page))
	query.Set("pagination[pageSize]", strconv.Itoa(pageSize))
	if sortField != "" {
		query.Set("sort[field]", sortField)
		query.Set("sort[order]", "desc")
	}
	filterIndex := 0
	if walletID != "" {
		prefix := "base_filters[" + strconv.Itoa(filterIndex) + "]"
		query.Set(prefix+"[filterName]", "walletId")
		query.Set(prefix+"[operator]", "in")
		query.Set(prefix+"[value][0]", walletID)
		filterIndex++
	}
	for _, filter := range []struct {
		value, operator string
	}{{from, ">="}, {to, "<="}} {
		if filter.value == "" {
			continue
		}
		prefix := "base_filters[" + strconv.Itoa(filterIndex) + "]"
		query.Set(prefix+"[filterName]", "dateTime")
		query.Set(prefix+"[operator]", filter.operator)
		query.Set(prefix+"[value]", filter.value)
		filterIndex++
	}
	var response struct {
		Items []TransactionSummaryAddressRecord `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/interacting_address/records?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	if walletID != "" {
		for index := range response.Items {
			if response.Items[index].WalletID == "" {
				response.Items[index].WalletID = walletID
			}
		}
	}
	return response.Items, nil
}

// TransactionSummaryAssets returns the asset ID-to-symbol choices used by the
// Transaction Summary dashboard without loading transaction rows.
func (c *Client) TransactionSummaryAssets(ctx context.Context, orgID string) ([]TransactionSummaryAsset, error) {
	query := url.Values{}
	query.Set("datasource", "bigquery")
	var response struct {
		Items []TransactionSummaryAsset `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/assets?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
