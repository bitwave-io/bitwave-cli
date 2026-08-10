package orgreports

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const rulesQuery = `query rules($orgId: ID!) {
  rules(orgId: $orgId) {
    id name disabled type priority methodId
    action {
      ... on DetailedCategorizationAction {
        lines { valueExtractor assetExtractor lineQualifierExtractor contactId categoryId metadataIds __typename }
        type ignoreFailPricing isIntercompanyTransfer __typename
      }
      ... on IgnoreAction { type __typename }
      ... on SimpleCategorizationAction { type contactId categoryId feeContactId feeCategoryId ignoreFailPricing __typename }
      ... on InternalTransferCategorizationAction {
        type internalFeeContactId: feeContactId internalFeeCategoryId: feeCategoryId ignoreFailPricing __typename
      }
      ... on SimpleSplitCategorizationAction {
        type
        splits { ... on PercentageSplit { percentage contactId categoryId __typename } __typename }
        feeSplits { ... on PercentageSplit { percentage contactId categoryId __typename } __typename }
        __typename
      }
      ... on TradeCategorizationAction { type tradeFeeContactId: feeContactId ignoreFailPricing __typename }
      ... on DeFiCategorizationAction { type deFiFeeContactId: feeContactId deFiWalletId __typename }
      ... on IntercompanyTransferCategorizationAction {
        type intercompanyFeeContactId: feeContactId intercompanyFeeCategoryId: feeCategoryId
        disposedCategoryId disposedContactId acquiredCategoryId acquiredContactId ignoreFailPricing __typename
      }
      __typename
    }
    coin description memo fromAddress toAddress
    valueRules { comparison value }
    afterDateSEC beforeDateSEC walletId direction autoReconcile collapseValues
    autoCategorizeFee multiToken accountingConnectionId includesCurrency allowMismatch
    metadataRule { operator metadata { key value __typename } txnRecordRule __typename }
    __typename
  }
}`

const createRuleMutation = `mutation CreateRule($orgId: ID!, $rule: Rule!) {
  createRule(orgId: $orgId, rule: $rule) { success errors }
}`

type RuleCreateResult struct {
	Success bool            `json:"success"`
	Errors  json.RawMessage `json:"errors,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (c *Client) Rules(ctx context.Context, orgID string) ([]json.RawMessage, error) {
	request := map[string]any{
		"operationName": "rules",
		"query":         rulesQuery,
		"variables":     map[string]any{"orgId": orgID},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesQueryURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			Rules []json.RawMessage `json:"rules"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode rules response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	return response.Data.Rules, nil
}

func (c *Client) CreateRule(ctx context.Context, orgID string, rule json.RawMessage) (*RuleCreateResult, error) {
	request := map[string]any{
		"operationName": "CreateRule",
		"query":         createRuleMutation,
		"variables":     map[string]any{"orgId": orgID, "rule": rule},
	}
	data, err := c.doEndpoint(ctx, http.MethodPost, c.RulesMutationURL, request, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data struct {
			CreateRule RuleCreateResult `json:"createRule"`
		} `json:"data"`
		Errors []graphQLError `json:"errors"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode create rule response: %w", err)
	}
	if err := graphqlErrors(response.Errors); err != nil {
		return nil, err
	}
	if !response.Data.CreateRule.Success {
		return &response.Data.CreateRule, fmt.Errorf("create rule was rejected: %s", strings.TrimSpace(string(response.Data.CreateRule.Errors)))
	}
	return &response.Data.CreateRule, nil
}

func (c *Client) ValidateRule(ctx context.Context, orgID, transactionID, ruleID string) (json.RawMessage, error) {
	path := "/orgs/" + url.PathEscape(orgID) + "/transactions/" + url.PathEscape(transactionID) + "/rules/" + url.PathEscape(ruleID)
	data, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("rule validation response was not valid JSON")
	}
	return json.RawMessage(data), nil
}

func graphqlErrors(items []graphQLError) error {
	if len(items) == 0 {
		return nil
	}
	messages := make([]string, 0, len(items))
	for _, item := range items {
		messages = append(messages, item.Message)
	}
	return fmt.Errorf("GraphQL: %s", strings.Join(messages, "; "))
}
