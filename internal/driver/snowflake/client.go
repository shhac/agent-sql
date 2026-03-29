package snowflake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"context"

	"github.com/shhac/agent-sql/internal/errors"
)

// pollIntervals defines the backoff intervals for async polling.
var pollIntervals = []time.Duration{
	500 * time.Millisecond,
	500 * time.Millisecond,
	1 * time.Second,
	1500 * time.Millisecond,
	2 * time.Second,
	4 * time.Second,
	5 * time.Second,
}

const maxRetries = 3

func (c *snowflakeConn) executeStatement(ctx context.Context, sqlStr string, binds map[string]binding) (*apiResponse, error) {
	req := statementRequest{
		Statement: sqlStr,
		Timeout:   45,
		Database:  c.database,
		Schema:    c.schema,
		Warehouse: c.warehouse,
		Role:      c.role,
		Parameters: map[string]string{
			"MULTI_STATEMENT_COUNT": "1",
		},
	}
	if len(binds) > 0 {
		req.Bindings = binds
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := c.baseURL + "/api/v2/statements"
	resp, err := c.doWithRetry(ctx, http.MethodPost, url, body)
	if err != nil {
		return nil, err
	}

	if resp.isAsync() {
		return c.pollForResult(ctx, resp.StatementHandle)
	}

	if resp.ResultSetMetaData == nil && resp.Message != "" && resp.Code != "090001" {
		return nil, &snowflakeAPIError{Code: resp.Code, Msg: resp.Message, SQLState: resp.SQLState}
	}

	return resp, nil
}

func (c *snowflakeConn) pollForResult(ctx context.Context, handle string) (*apiResponse, error) {
	url := c.baseURL + "/api/v2/statements/" + handle

	for attempt := range 100 {
		idx := attempt
		if idx >= len(pollIntervals) {
			idx = len(pollIntervals) - 1
		}
		delay := pollIntervals[idx]

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		resp, err := c.doWithRetry(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}

		if resp.isQuery() {
			return resp, nil
		}

		if !resp.isAsync() {
			return nil, &snowflakeAPIError{Code: resp.Code, Msg: resp.Message, SQLState: resp.SQLState}
		}
	}

	return nil, errors.New("Snowflake query timed out after polling", errors.FixableByRetry)
}

func (c *snowflakeConn) doWithRetry(ctx context.Context, method, url string, body []byte) (*apiResponse, error) {
	var lastErr error
	for attempt := range maxRetries + 1 {
		if attempt > 0 {
			// Exponential backoff with jitter-like delay
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		httpReq, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		httpReq.Header.Set("Authorization", "Bearer "+c.token)
		httpReq.Header.Set("X-Snowflake-Authorization-Token-Type", "PROGRAMMATIC_ACCESS_TOKEN")
		httpReq.Header.Set("Accept", "application/json")
		if body != nil {
			httpReq.Header.Set("Content-Type", "application/json")
		}

		httpResp, err := c.client.Do(httpReq)
		if err != nil {
			lastErr = err
			continue // transport errors are always retryable
		}

		respBody, err := io.ReadAll(httpResp.Body)
		httpResp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if isRetryable(httpResp.StatusCode) {
			lastErr = fmt.Errorf("HTTP %d", httpResp.StatusCode)
			continue
		}

		var apiResp apiResponse
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			return nil, fmt.Errorf("unmarshal response: %w (body: %s)", err, truncateBody(respBody))
		}

		return &apiResp, nil
	}

	return nil, fmt.Errorf("request failed after %d retries: %w", maxRetries+1, lastErr)
}

func isRetryable(status int) bool {
	return status == 429 || status == 408 || status >= 500
}

func truncateBody(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "..."
	}
	return string(b)
}
