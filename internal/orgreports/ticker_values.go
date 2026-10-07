package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// TransactionTickerValues returns the same complete ticker choice list used by
// the transaction grid's Filter by Ticker dropdown. limit=-1 is the deployed
// transaction service sentinel for all values up to its hard cap.
func (c *Client) TransactionTickerValues(ctx context.Context, orgID string) ([]string, error) {
	query := url.Values{"fieldName": {"amountCurrencyName"}, "limit": {"-1"}}
	endpoint := strings.TrimRight(c.TransactionsURL, "/") + "/orgs/" + url.PathEscape(orgID) + "/lookups?" + query.Encode()
	data, err := c.doEndpoint(ctx, http.MethodGet, endpoint, nil, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Values []string `json:"values"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode transaction ticker lookup: %w", err)
	}
	return response.Values, nil
}
