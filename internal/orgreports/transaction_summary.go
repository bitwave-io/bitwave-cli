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

func transactionSummaryPagination(page, pageSize int) (url.Values, error) {
	if page < 1 || pageSize < 1 || pageSize > 500 {
		return nil, fmt.Errorf("transaction summary page must be at least 1 and page size must be between 1 and 500")
	}
	query := url.Values{}
	query.Set("datasource", "bigquery")
	query.Set("pagination[pageNumber]", strconv.Itoa(page))
	query.Set("pagination[pageSize]", strconv.Itoa(pageSize))
	return query, nil
}

func addSummaryDateFilters(query url.Values, from, to string, startIndex int) {
	for _, filter := range []struct {
		value    string
		operator string
	}{{from, ">="}, {to, "<="}} {
		if filter.value == "" {
			continue
		}
		prefix := "base_filters[" + strconv.Itoa(startIndex) + "]"
		query.Set(prefix+"[filterName]", "dateTime")
		query.Set(prefix+"[operator]", filter.operator)
		query.Set(prefix+"[value]", filter.value)
		startIndex++
	}
}

func (c *Client) TransactionSummaryWallets(ctx context.Context, orgID, from, to string, page, pageSize int) ([]TransactionSummaryWalletRecord, error) {
	query, err := transactionSummaryPagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	addSummaryDateFilters(query, from, to, 0)
	var response struct {
		Items []TransactionSummaryWalletRecord `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/main/records?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

func (c *Client) TransactionSummaryAddresses(ctx context.Context, orgID, walletID, from, to, sortField string, page, pageSize int) ([]TransactionSummaryAddressRecord, error) {
	query, err := transactionSummaryPagination(page, pageSize)
	if err != nil {
		return nil, err
	}
	filterIndex := 0
	if walletID != "" {
		prefix := "base_filters[0]"
		query.Set(prefix+"[filterName]", "walletId")
		query.Set(prefix+"[operator]", "in")
		query.Set(prefix+"[value][0]", walletID)
		filterIndex++
	}
	addSummaryDateFilters(query, from, to, filterIndex)
	if sortField != "" {
		query.Set("sort[field]", sortField)
		query.Set("sort[order]", "desc")
	}
	var response struct {
		Items []TransactionSummaryAddressRecord `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/interacting_address/records?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

func (c *Client) TransactionSummaryAssets(ctx context.Context, orgID string) ([]TransactionSummaryAsset, error) {
	query := url.Values{"datasource": []string{"bigquery"}}
	var response struct {
		Items []TransactionSummaryAsset `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/assets?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
