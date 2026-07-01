package thegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a minimal GraphQL client for a single subgraph endpoint. One
// Client per subgraph (V2 and V3 are separate subgraphs with separate
// endpoints — see Config).
type Client struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a client for a single subgraph endpoint.
// apiKey may be empty for endpoints that don't require one (e.g. a
// self-hosted or free-tier subgraph); when set, it's sent as a Bearer token,
// matching The Graph Gateway's documented auth scheme.
func NewClient(endpoint, apiKey string) *Client {
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// graphQLRequest is the standard GraphQL-over-HTTP request body.
type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// graphQLError mirrors the "errors" array in a GraphQL response — a
// response can be HTTP 200 and still carry one or more of these, so callers
// must check for them explicitly rather than relying on the HTTP status.
type graphQLError struct {
	Message string `json:"message"`
}

// graphQLResponse wraps the "data"/"errors" envelope every GraphQL response
// uses. Data is left as json.RawMessage so each query's caller can unmarshal
// it into whatever shape that specific query returns, rather than this
// generic client needing to know every subgraph's schema.
type graphQLResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []graphQLError  `json:"errors"`
}

// Query executes a GraphQL query against this client's endpoint and
// unmarshals the "data" field into dst (a pointer to whatever struct shape
// the caller expects). Returns an error if the HTTP call fails, the
// response isn't valid JSON, or the response carries a GraphQL-level error
// (which — unlike a REST API — does not necessarily correspond to a non-2xx
// HTTP status).
func (c *Client) Query(ctx context.Context, query string, variables map[string]any, dst any) error {
	reqBody, err := json.Marshal(graphQLRequest{
		Query:     query,
		Variables: variables,
	})

	if err != nil {
		return fmt.Errorf("thegraph: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(reqBody))

	if err != nil {
		return fmt.Errorf("thegraph: create request failed: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("thegraph: request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("thegraph: read response body failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("thegraph: unexpected status %d: %s", resp.StatusCode, truncate(body, 500))
	}

	var gqlResp graphQLResponse
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return fmt.Errorf("thegraph: unmarshal response envelope: %w (body: %s)", err, truncate(body, 500))
	}

	if len(gqlResp.Errors) > 0 {
		return fmt.Errorf("thegraph: graphql error: %s", gqlResp.Errors[0].Message)
	}

	if dst != nil {
		if err := json.Unmarshal(gqlResp.Data, dst); err != nil {
			return fmt.Errorf("thegraph: unmarshal data field: %w", err)
		}
	}

	return nil
}

// truncate keeps error messages from ballooning when a subgraph returns an
// unexpectedly large error/HTML body (e.g. a gateway error page).
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}
