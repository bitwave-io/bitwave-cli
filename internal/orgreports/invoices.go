package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
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

// FindInvoiceInput mirrors the contact-first invoice picker in the Bitwave UI.
// Title is matched exactly (case-insensitively) after fetching only the
// selected contact's invoices. This avoids enumerating an organization's full
// invoice population, which can contain hundreds of thousands of records.
type FindInvoiceInput struct {
	ContactID       string
	Title           string
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

func (c *Client) FindInvoiceForContact(ctx context.Context, orgID string, input FindInvoiceInput) (*Invoice, error) {
	contactID := strings.TrimSpace(input.ContactID)
	title := strings.TrimSpace(input.Title)
	if contactID == "" {
		return nil, fmt.Errorf("contact id is required")
	}
	if title == "" {
		return nil, fmt.Errorf("invoice number is required")
	}
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}

	var match *Invoice
	var token string
	seen := map[string]bool{}
	for {
		page, err := c.Invoices(ctx, orgID, InvoiceListInput{
			ContactID: contactID, PageToken: token, PageSize: pageSize, IncludeDisabled: input.IncludeDisabled,
		})
		if err != nil {
			return nil, err
		}
		for i := range page.Records {
			invoice := page.Records[i]
			if strings.EqualFold(strings.TrimSpace(invoice.Title), title) {
				if match != nil && match.ID != invoice.ID {
					return nil, fmt.Errorf("multiple invoices named %q exist for contact %q", title, contactID)
				}
				copy := invoice
				match = &copy
			}
		}
		if len(page.Records) < pageSize || page.NextPageToken == "" || page.NextPageToken == token || seen[page.NextPageToken] {
			break
		}
		seen[page.NextPageToken] = true
		token = page.NextPageToken
	}
	if match == nil {
		return nil, fmt.Errorf("invoice %q was not found for contact %q", title, contactID)
	}
	return match, nil
}

func resourceConnectionID(id string) string {
	for i := range id {
		if id[i] == '.' {
			return id[:i]
		}
	}
	return ""
}
