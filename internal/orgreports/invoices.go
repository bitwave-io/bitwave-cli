package orgreports

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

type Invoice struct {
	ID                     string          `json:"id"`
	Title                  string          `json:"title,omitempty"`
	Type                   string          `json:"type,omitempty"`
	Status                 string          `json:"status,omitempty"`
	Date                   string          `json:"date,omitempty"`
	DueDate                string          `json:"dueDate,omitempty"`
	DueAmount              json.Number     `json:"dueAmount,omitempty"`
	TotalAmount            json.Number     `json:"totalAmount,omitempty"`
	Currency               string          `json:"currency,omitempty"`
	Source                 string          `json:"source,omitempty"`
	Enabled                bool            `json:"enabled"`
	ContactID              string          `json:"contactId,omitempty"`
	OrgID                  string          `json:"orgId,omitempty"`
	AccountingConnectionID string          `json:"accountingConnectionId,omitempty"`
	HasMatchedTransactions bool            `json:"hasMatchedTransactions"`
	ExchangeRate           json.Number     `json:"exchangeRate,omitempty"`
	LastUpdatedSEC         int64           `json:"lastUpdatedSEC,omitempty"`
	Lines                  json.RawMessage `json:"lines,omitempty"`
}

type InvoicePage struct {
	Records           []Invoice `json:"records"`
	PreviousPageToken string    `json:"previousPageToken,omitempty"`
	NextPageToken     string    `json:"nextPageToken,omitempty"`
}

type InvoiceListInput struct {
	ContactID       string
	PageToken       string
	PageSize        int
	IncludeDisabled bool
}

func (c *Client) Invoices(ctx context.Context, orgID string, input InvoiceListInput) (*InvoicePage, error) {
	query := url.Values{
		"contactId": {input.ContactID},
		"pageSize":  {strconv.Itoa(input.PageSize)},
	}
	if input.PageToken != "" {
		query.Set("lastRef", input.PageToken)
	}
	if input.IncludeDisabled {
		query.Set("includeDisabled", "true")
	}

	var page InvoicePage
	path := "/invoices/" + url.PathEscape(orgID) + "?" + query.Encode()
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &page); err != nil {
		return nil, err
	}
	for i := range page.Records {
		if page.Records[i].AccountingConnectionID == "" {
			page.Records[i].AccountingConnectionID = resourceConnectionID(page.Records[i].ID)
		}
	}
	return &page, nil
}

func (c *Client) Invoice(ctx context.Context, orgID, invoiceID string) (*Invoice, error) {
	var invoice Invoice
	path := "/invoices/" + url.PathEscape(orgID) + "/invoice/" + url.PathEscape(invoiceID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &invoice); err != nil {
		return nil, err
	}
	if invoice.AccountingConnectionID == "" {
		invoice.AccountingConnectionID = resourceConnectionID(invoice.ID)
	}
	return &invoice, nil
}

func resourceConnectionID(id string) string {
	for i := range id {
		if id[i] == '.' {
			return id[:i]
		}
	}
	return ""
}
