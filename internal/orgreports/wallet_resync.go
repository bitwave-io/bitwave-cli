package orgreports

import (
	"context"
	"net/http"
	"net/url"
)

type WalletResyncStateTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type FullWalletResyncResponse struct {
	Success      bool                      `json:"success"`
	DryRun       bool                      `json:"dryRun"`
	OrgID        string                    `json:"orgId"`
	WalletID     string                    `json:"walletId"`
	Syncer       string                    `json:"syncer,omitempty"`
	WorkflowHref string                    `json:"workflowHref,omitempty"`
	ClearedState []WalletResyncStateTarget `json:"clearedState,omitempty"`
	ExecutionID  string                    `json:"executionId,omitempty"`
	Message      string                    `json:"message,omitempty"`
}

func (c *Client) FullWalletResync(ctx context.Context, orgID, walletID string, execute bool) (*FullWalletResyncResponse, error) {
	path := "/hawaii/orgs/" + url.PathEscape(orgID) + "/wallets/" + url.PathEscape(walletID) + "/fullResync"
	var response FullWalletResyncResponse
	if err := c.doJSON(ctx, http.MethodPost, path, map[string]bool{"execute": execute}, &response); err != nil {
		return nil, err
	}
	return &response, nil
}
