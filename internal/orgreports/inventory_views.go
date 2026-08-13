package orgreports

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// InventoryViewCreateRequest mirrors the deployed Bitwave inventory-view
// create contract. The CLI keeps this intentionally small: advanced settings
// can be supplied later when the user and their accounting adviser have made
// the underlying accounting decisions.
type InventoryViewCreateRequest struct {
	Name                        string                `json:"name"`
	Config                      InventoryViewConfig   `json:"config"`
	Strategy                    InventoryViewStrategy `json:"strategy"`
	Impair                      bool                  `json:"impair"`
	IgnoreNFTs                  bool                  `json:"ignoreNFTs"`
	IgnoreOrgWrappingTreatments bool                  `json:"ignoreOrgWrappingTreatments"`
	IsReallocatedView           bool                  `json:"isReallocatedView,omitempty"`
	SubsidiaryIDs               []string              `json:"subsidiaryIds,omitempty"`
}

type InventoryViewStrategy struct {
	TaxStrategy string `json:"taxStrategy"`
}

type InventoryMappingRule struct {
	Type string `json:"type"`
}

type InventoryViewConfig struct {
	CapitalizeTradingFees                  bool                  `json:"capitalizeTradingFees"`
	ImpairmentMethodology                  string                `json:"impairmentMethodology,omitempty"`
	InventoryMappingRule                   *InventoryMappingRule `json:"inventoryMappingRule,omitempty"`
	DefaultValuationStrategy               string                `json:"defaultValuationStrategy,omitempty"`
	EngineVersionOverride                  float64               `json:"engineVersionOverride,omitempty"`
	CostBasisCarryForwardAcquiredSide      bool                  `json:"costBasisCarryForwardAcquiredSide"`
	ProcessAcquisitionsBeforeDisposals     bool                  `json:"processAcquisitionsBeforeDisposals"`
	UseOriginalAcquisitionDateForTransfers bool                  `json:"useOriginalAcquisitionDateForWliTransfers"`
}

type CreateResult struct {
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

func (c *Client) CreateInventoryView(ctx context.Context, orgID string, input InventoryViewCreateRequest) (*CreateResult, error) {
	var result CreateResult
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	if !result.Success || result.ID == "" {
		return nil, fmt.Errorf("inventory-view response did not confirm creation")
	}
	return &result, nil
}

func (c *Client) TriggerInventoryViewUpdate(ctx context.Context, orgID, inventoryViewID string) (*CreateResult, error) {
	var result CreateResult
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(inventoryViewID) + "/updates"
	if err := c.doJSON(ctx, http.MethodPost, path, nil, &result); err != nil {
		return nil, err
	}
	if !result.Success || result.ID == "" {
		return nil, fmt.Errorf("inventory-view update response did not include a run id")
	}
	return &result, nil
}

// TriggerInventoryViewUpdateEnhanced mirrors the web application's Update Now
// request. An explicit endingDate avoids generating a pricing window that
// extends into the future near timezone/day boundaries.
func (c *Client) TriggerInventoryViewUpdateEnhanced(ctx context.Context, orgID, inventoryViewID string, input InventoryViewUpdateRequest) (*CreateResult, error) {
	var result CreateResult
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(inventoryViewID) + "/update-requests"
	if err := c.doJSON(ctx, http.MethodPost, path, input, &result); err != nil {
		return nil, err
	}
	if !result.Success || result.ID == "" {
		if result.Error == "" {
			result.Error = "inventory-view update response did not include a run id"
		}
		return nil, fmt.Errorf("%s", result.Error)
	}
	return &result, nil
}

func (c *Client) DeleteInventoryView(ctx context.Context, orgID, inventoryViewID string) error {
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(inventoryViewID)
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *Client) InventoryViewUpdates(ctx context.Context, orgID, inventoryViewID string) ([]InventoryViewUpdate, error) {
	var response struct {
		Items []InventoryViewUpdate `json:"items"`
	}
	path := "/orgs/" + url.PathEscape(orgID) + "/inventory-views/" + url.PathEscape(inventoryViewID) + "/updates"
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &response); err != nil {
		return nil, err
	}
	return response.Items, nil
}
