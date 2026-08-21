package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

type TransactionAssetValue struct {
	CurrencyID string `json:"currencyId"`
	Value      string `json:"value"`
}

type TransactionExchangeRate struct {
	ID      string `json:"id,omitempty"`
	From    string `json:"from"`
	To      string `json:"to"`
	Type    string `json:"type"`
	PriceID string `json:"priceId,omitempty"`
	Rate    string `json:"rate,omitempty"`
	Reason  string `json:"reason,omitempty"`
}

type TransactionCategorizationLine struct {
	ID           string                   `json:"id,omitempty"`
	TxnLineID    int                      `json:"txnLineId"`
	Operation    string                   `json:"operation"`
	Amount       TransactionAssetValue    `json:"amount"`
	Value        TransactionAssetValue    `json:"value"`
	WalletID     string                   `json:"walletId"`
	From         string                   `json:"from,omitempty"`
	To           string                   `json:"to,omitempty"`
	ExchangeRate *TransactionExchangeRate `json:"exchangeRate,omitempty"`
}

type TransactionCategorizationContext struct {
	State struct {
		State          string          `json:"state"`
		Categorization json.RawMessage `json:"categorization"`
		Price          struct {
			ExchangeRates           map[string]TransactionExchangeRate `json:"exchangeRates"`
			TransactionPriceVersion *int                               `json:"transactionPriceVersion"`
		} `json:"price"`
		Transaction struct {
			TransactionID   string                          `json:"transactionId"`
			TransactionType string                          `json:"txnType"`
			TimestampSEC    int64                           `json:"txnTimestampSEC"`
			Lines           []TransactionCategorizationLine `json:"txnLines"`
		} `json:"txn"`
	} `json:"state"`
	Assets []struct {
		CurrencyID string `json:"currencyId"`
		Ticker     string `json:"ticker"`
		Unit       string `json:"unit,omitempty"`
	} `json:"assets"`
}

func (c *Client) TransactionCategorizationContext(ctx context.Context, orgID, transactionID string) (*TransactionCategorizationContext, error) {
	var result TransactionCategorizationContext
	path := "/orgs/" + url.PathEscape(orgID) + "/transactions/" + url.PathEscape(transactionID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

type ContactSearchResponse struct {
	Items []Contact `json:"items"`
}

func (c *Client) SearchContacts(ctx context.Context, orgID, query string, limit int) ([]Contact, error) {
	values := url.Values{"nameQuery": {query}, "limit": {stringInt(limit)}}
	var response ContactSearchResponse
	path := "/contacts/" + url.PathEscape(orgID) + "/search?" + values.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

func stringInt(value int) string {
	if value <= 0 {
		value = 25
	}
	return strconv.Itoa(value)
}
