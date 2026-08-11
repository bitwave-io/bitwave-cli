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

// TransactionSummaryAddresses reads the same Interacting Addresses view used
// by Bitwave's Transaction Summary dashboard. The dashboard is the preferred
// discovery source because it aggregates recurring counterparties before any
// raw transaction sampling is sent to an LLM.
func (c *Client) TransactionSummaryAddresses(ctx context.Context, orgID string, page, pageSize int) ([]TransactionSummaryAddressRecord, error) {
	if page < 1 || pageSize < 1 || pageSize > 500 {
		return nil, fmt.Errorf("invalid transaction summary pagination")
	}
	query := url.Values{}
	query.Set("datasource", "bigquery")
	query.Set("pagination[pageNumber]", strconv.Itoa(page))
	query.Set("pagination[pageSize]", strconv.Itoa(pageSize))
	var response struct {
		Items []TransactionSummaryAddressRecord `json:"items"`
	}
	path := "/dashboard/" + url.PathEscape(orgID) + "/txns_summary/interacting_address/records?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
