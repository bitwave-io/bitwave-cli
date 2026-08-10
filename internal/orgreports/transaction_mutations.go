package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

const (
	TransactionStateIgnore   = "ignore"
	TransactionStateUnignore = "un-ignore"
)

type BulkStateRequest struct {
	BulkActionID   string   `json:"bulkActionId,omitempty"`
	TransactionIDs []string `json:"transactionIds"`
	Update         string   `json:"update"`
}

type TransactionFailure struct {
	TransactionID string `json:"transactionId"`
	Error         string `json:"error"`
}

type BulkStateResponse struct {
	WorkflowID   string               `json:"workflowId,omitempty"`
	BulkActionID string               `json:"bulkActionId,omitempty"`
	Status       string               `json:"status,omitempty"`
	Success      bool                 `json:"success"`
	Processed    int                  `json:"processed"`
	SuccessCount int                  `json:"successCount"`
	Failed       []TransactionFailure `json:"failed"`
	Transactions []struct {
		TransactionID string `json:"transactionId"`
		Status        string `json:"status"`
		Error         string `json:"error,omitempty"`
	} `json:"transactions,omitempty"`
}

type BulkCategorizeResult struct {
	Success bool   `json:"success"`
	TxnID   string `json:"txnId"`
	Reason  string `json:"reason,omitempty"`
}

type Category struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Code                   string `json:"code,omitempty"`
	Enabled                bool   `json:"enabled"`
	Source                 string `json:"source,omitempty"`
	Type                   string `json:"type,omitempty"`
	AccountingConnectionID string `json:"accountingConnectionId,omitempty"`
}

type Contact struct {
	ID                     string `json:"id"`
	Name                   string `json:"name"`
	Enabled                bool   `json:"enabled"`
	Source                 string `json:"source,omitempty"`
	Type                   string `json:"type,omitempty"`
	AccountingConnectionID string `json:"accountingConnectionId,omitempty"`
}

type AccountingConnection struct {
	ID       string `json:"id"`
	Name     string `json:"name,omitempty"`
	Type     string `json:"type"`
	Disabled bool   `json:"disabled"`
}

func (c *Client) BulkUpdateTransactionState(ctx context.Context, orgID string, input BulkStateRequest) (*BulkStateResponse, error) {
	if len(input.TransactionIDs) == 0 {
		return nil, fmt.Errorf("at least one transaction id is required")
	}
	var response BulkStateResponse
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/transactions/bulk/state"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

func (c *Client) Transaction(ctx context.Context, orgID, transactionID string) (json.RawMessage, error) {
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/transactions/" + url.PathEscape(transactionID)
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("transaction response was not valid JSON")
	}
	return json.RawMessage(data), nil
}

func (c *Client) BulkTransactionStateStatus(ctx context.Context, orgID, workflowID string) (*BulkStateResponse, error) {
	var response BulkStateResponse
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/transactions/bulk/state/" + url.PathEscape(workflowID)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return &response, nil
}

// CategorizeTransaction sends the complete Bitwave categorization DTO. The
// shape is deliberately json.RawMessage because it is a tagged union whose
// required fields depend on the transaction and categorization type.
func (c *Client) CategorizeTransaction(ctx context.Context, orgID, transactionID string, body json.RawMessage) error {
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/transactions/" + url.PathEscape(transactionID)
	_, err := c.do(ctx, http.MethodPatch, path, body)
	return err
}

func (c *Client) BulkCategorizeTransactions(ctx context.Context, orgID string, body json.RawMessage) ([]BulkCategorizeResult, error) {
	path := "/v3/orgs/" + url.PathEscape(orgID) + "/transactions"
	data, err := c.do(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	var response []BulkCategorizeResult
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode bulk categorization response: %w", err)
	}
	return response, nil
}

func (c *Client) Categories(ctx context.Context, orgID string) ([]Category, error) {
	var result []Category
	var token string
	for {
		query := url.Values{"pageLimit": {"500"}}
		if token != "" {
			query.Set("paginationToken", token)
		}
		var response struct {
			Items    []Category `json:"items"`
			NextPage string     `json:"nextPage"`
		}
		path := "/org/" + url.PathEscape(orgID) + "/categories?" + query.Encode()
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		result = append(result, response.Items...)
		if response.NextPage == "" || response.NextPage == token {
			return result, nil
		}
		token = response.NextPage
	}
}

func (c *Client) Contacts(ctx context.Context, orgID string) ([]Contact, error) {
	var result []Contact
	var token string
	for {
		query := url.Values{"pageLimit": {"500"}}
		if token != "" {
			query.Set("paginationToken", token)
		}
		var response struct {
			Items    []Contact `json:"items"`
			NextPage string    `json:"nextPage"`
		}
		path := "/contacts/" + url.PathEscape(orgID) + "?" + query.Encode()
		if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
			return nil, err
		}
		result = append(result, response.Items...)
		if response.NextPage == "" || response.NextPage == token {
			return result, nil
		}
		token = response.NextPage
	}
}

func (c *Client) AccountingConnections(ctx context.Context, orgID string) ([]AccountingConnection, error) {
	var response struct {
		Connections []AccountingConnection `json:"connections"`
	}
	path := "/orgs/" + url.PathEscape(orgID) + "/accounting-connections"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Connections, nil
}
