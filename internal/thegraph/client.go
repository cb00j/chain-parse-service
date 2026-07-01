package thegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
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
//
// Timeout is intentionally generous (60s, not a more typical 10-15s):
// paginated queries against a large historical backlog (e.g. Uniswap V2's
// ~500k pairs) can be genuinely slow to compute on the indexer's side,
// especially the cursor-based "or"/"and" where-filter used by
// fetchV2PairsSince/FetchV3PoolsSince, which costs more to plan than a
// plain range filter — this isn't a hung request, just a query that
// legitimately takes longer than a typical API call.
func NewClient(endpoint, apiKey string) *Client {
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
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

// maxRetries / retryBaseDelay bound the automatic retry behavior in Query.
// The Graph's decentralized network Gateway load-balances each request
// across multiple indexers; a batch of currently-unhealthy indexers
// (falling behind, timing out, returning malformed responses — this
// surfaces as a "bad indexers" GraphQL error) is common and usually
// resolves itself on the very next attempt against a different indexer.
// Waiting out a full sync_interval for the caller's own retry loop to
// fire again is wasteful when a few seconds and a fresh attempt would do.
const (
	maxRetries     = 3
	retryBaseDelay = 2 * time.Second
)

// Query executes a GraphQL query against this client's endpoint and
// unmarshals the "data" field into dst (a pointer to whatever struct shape
// the caller expects). Returns an error if the HTTP call fails, the
// response isn't valid JSON, or the response carries a GraphQL-level error
// (which — unlike a REST API — does not necessarily correspond to a non-2xx
// HTTP status).
//
// Transient failures (network errors, non-200 status, gateway/indexer
// errors like "bad indexers") are retried up to maxRetries times with
// exponential backoff. Authentication errors are not retried — a bad API
// key doesn't become valid by trying again, so failing fast there avoids
// burning maxRetries attempts (and their backoff delays) on something
// retrying can't fix.
func (c *Client) Query(ctx context.Context, query string, variables map[string]any, dst any) error {
	reqBody, err := json.Marshal(graphQLRequest{
		Query:     query,
		Variables: variables,
	})
	if err != nil {
		return fmt.Errorf("thegraph: marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			delay := retryBaseDelay * time.Duration(1<<(attempt-1)) // 2s, 4s, 8s
			select {
			case <-ctx.Done():
				return fmt.Errorf("thegraph: %w (last attempt error: %v)", ctx.Err(), lastErr)
			case <-time.After(delay):
			}
		}

		err := c.doQuery(ctx, reqBody, dst)
		if err == nil {
			return nil
		}
		if isPermanentQueryError(err) {
			return err
		}
		lastErr = err
		if attempt < maxRetries {
			log.Warnf("[thegraph] request failed (attempt %d/%d), retrying: %v", attempt+1, maxRetries+1, err)
		}
	}

	return fmt.Errorf("thegraph: giving up after %d attempts: %w", maxRetries+1, lastErr)
}

// isPermanentQueryError reports whether retrying Query is pointless for
// this error — currently just authentication failures. Matched by
// substring on the GraphQL error message rather than a typed error because
// the Gateway doesn't distinguish these at the HTTP/transport level (both
// auth errors and transient indexer errors come back as HTTP 200 with a
// GraphQL "errors" array).
func isPermanentQueryError(err error) bool {
	return strings.Contains(err.Error(), "auth error")
}

// doQuery performs a single request/response round trip — the part of
// Query that actually talks to the network, factored out so Query's retry
// loop can call it repeatedly without re-marshaling the request body each
// time.
func (c *Client) doQuery(ctx context.Context, reqBody []byte, dst any) error {
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
