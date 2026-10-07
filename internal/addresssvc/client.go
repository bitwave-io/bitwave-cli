// Package addresssvc reads Bitwave's public token metadata service.
package addresssvc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultBaseURL = "https://address-svc-utyjy373hq-uc.a.run.app"
const DefaultSpamThreshold = 0.5

type Symbols struct {
	CanonicalSymbol string `json:"canonicalSymbol,omitempty"`
	HistoricSymbol  string `json:"historicSymbol,omitempty"`
	ContractSymbol  string `json:"contractSymbol,omitempty"`
	PricingSymbol   string `json:"pricingSymbol,omitempty"`
	CoinGeckoID     string `json:"coinGeckoId,omitempty"`
}

type Coin struct {
	CoinID    int64    `json:"coinId"`
	NetworkID string   `json:"networkId,omitempty"`
	Address   string   `json:"address,omitempty"`
	TokenID   string   `json:"tokenId,omitempty"`
	Symbol    string   `json:"symbol"`
	Symbols   Symbols  `json:"symbols,omitempty"`
	Decimals  *int64   `json:"decimals,omitempty"`
	SpamScore *float64 `json:"spamScore,omitempty"`
	Source    string   `json:"source,omitempty"`
	Type      string   `json:"type,omitempty"`
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (c *Client) LookupSymbol(ctx context.Context, symbol string) (*Coin, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	endpoint := c.BaseURL + "/symbols/" + url.PathEscape(symbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lookup symbol %s: %w", symbol, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read symbol %s response: %w", symbol, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("lookup symbol %s returned HTTP %d: %s", symbol, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var coin Coin
	if err := json.Unmarshal(body, &coin); err != nil {
		return nil, fmt.Errorf("decode symbol %s response: %w", symbol, err)
	}
	if coin.Symbol == "" {
		return nil, fmt.Errorf("symbol %s response did not include a symbol", symbol)
	}
	return &coin, nil
}

func IsSpam(coin *Coin, threshold float64) bool {
	return coin != nil && coin.SpamScore != nil && *coin.SpamScore >= threshold
}
