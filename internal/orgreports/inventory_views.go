package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

type InventoryMutationResult struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Error   string `json:"error,omitempty"`
}

type InventoryViewUpdateRequest struct {
	RunIDReference           string `json:"runIdReference,omitempty"`
	StartingDate             string `json:"startingDate,omitempty"`
	EndingDate               string `json:"endingDate"`
	TransferAtHistoricalCost bool   `json:"transferAtHistoricalCost"`
}

type InventoryViewUpdate struct {
	ID                       string   `json:"id"`
	Status                   string   `json:"status"`
	InventoryViewID          string   `json:"inventoryViewId"`
	UpdateRequestedSEC       int64    `json:"updateRequestedSEC"`
	UpdateCompletedSEC       int64    `json:"updateCompletedSEC,omitempty"`
	Errors                   []string `json:"errors,omitempty"`
	RunIDReference           string   `json:"runIdReference,omitempty"`
	StartingSEC              int64    `json:"startingSEC,omitempty"`
	EndingSEC                int64    `json:"endingSEC,omitempty"`
	TransferAtHistoricalCost bool     `json:"transferAtHistoricalCost,omitempty"`
}

func (c *Client) CreateInventoryView(ctx context.Context, orgID string, input json.RawMessage) (*InventoryMutationResult, error) {
	var result InventoryMutationResult
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	if !result.Success || result.ID == "" {
		return nil, fmt.Errorf("inventory-view response did not confirm creation: %s", result.Error)
	}
	return &result, nil
}

func (c *Client) TriggerInventoryViewUpdate(ctx context.Context, orgID, viewID string, input InventoryViewUpdateRequest) (*InventoryMutationResult, error) {
	var result InventoryMutationResult
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(viewID) + "/update-requests"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	if !result.Success || result.ID == "" {
		return nil, fmt.Errorf("inventory-view update was rejected: %s", result.Error)
	}
	return &result, nil
}

func (c *Client) InventoryViewUpdates(ctx context.Context, orgID, viewID string) ([]InventoryViewUpdate, error) {
	var response struct {
		Items []InventoryViewUpdate `json:"items"`
	}
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(viewID) + "/updates"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}

func (c *Client) DeleteInventoryView(ctx context.Context, orgID, viewID string) error {
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(viewID)
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *Client) CancelInventoryViewUpdate(ctx context.Context, orgID, viewID, updateID string) (*InventoryMutationResult, error) {
	var result InventoryMutationResult
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(viewID) + "/" + url.PathEscape(updateID) + "/cancel"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &result); err != nil {
		return nil, err
	}
	if !result.Success {
		return nil, fmt.Errorf("inventory-view cancel was rejected: %s", result.Error)
	}
	return &result, nil
}
