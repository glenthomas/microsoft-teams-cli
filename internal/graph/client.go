// Package graph is a minimal Microsoft Graph client for Teams data.
package graph

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// TokenSource supplies bearer tokens for Graph requests.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// Client performs authenticated requests against Microsoft Graph.
type Client struct {
	BaseURL    string
	HTTP       *http.Client
	Tokens     TokenSource
	MaxRetries int
	// Sleep is used to wait between retries; overridable for tests.
	Sleep func(ctx context.Context, d time.Duration) error
}

// NewClient returns a Client with sensible defaults.
func NewClient(baseURL string, tokens TokenSource) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTP:       &http.Client{Timeout: 60 * time.Second},
		Tokens:     tokens,
		MaxRetries: 4,
		Sleep:      sleepCtx,
	}
}

// APIError is returned for non-2xx Graph responses.
type APIError struct {
	StatusCode int    `json:"status"`
	Code       string `json:"code"`
	Message    string `json:"message"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("graph API error %d %s: %s", e.StatusCode, e.Code, e.Message)
}

// IsStatus reports whether err is an APIError with the given HTTP status.
func IsStatus(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// Get fetches path (relative to BaseURL, or an absolute URL on the same host
// such as an @odata.nextLink) and decodes the JSON response into out.
func (c *Client) Get(ctx context.Context, path string, query url.Values, out any) error {
	u, err := c.resolve(path, query)
	if err != nil {
		return err
	}
	for attempt := 0; ; attempt++ {
		retryAfter, err := c.do(ctx, u, out)
		if err == nil {
			return nil
		}
		if retryAfter < 0 || attempt >= c.MaxRetries {
			return err
		}
		if err := c.Sleep(ctx, retryAfter); err != nil {
			return err
		}
	}
}

// Post sends a JSON request to Microsoft Graph and decodes its JSON response.
// It does not retry because repeating a POST can create duplicate resources.
func (c *Client) Post(ctx context.Context, path string, body, out any) error {
	u, err := c.resolve(path, nil)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encoding Microsoft Graph request: %w", err)
	}
	_, err = c.request(ctx, http.MethodPost, u, payload, out)
	return err
}

// do performs a single request. It returns a non-negative retry delay when the
// request may be retried.
func (c *Client) do(ctx context.Context, u string, out any) (time.Duration, error) {
	return c.request(ctx, http.MethodGet, u, nil, out)
}

func (c *Client) request(ctx context.Context, method, u string, body []byte, out any) (time.Duration, error) {
	token, err := c.Tokens.Token(ctx)
	if err != nil {
		return -1, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return -1, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return -1, ctx.Err()
		}
		return time.Second, fmt.Errorf("request to Microsoft Graph failed: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return time.Second, fmt.Errorf("reading Microsoft Graph response: %w", err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return -1, nil
		}
		if err := json.Unmarshal(responseBody, out); err != nil {
			return -1, fmt.Errorf("decoding Microsoft Graph response: %w", err)
		}
		return -1, nil
	}

	apiErr := &APIError{StatusCode: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(responseBody, &payload) == nil && payload.Error.Code != "" {
		apiErr.Code = payload.Error.Code
		apiErr.Message = payload.Error.Message
	}
	switch resp.StatusCode {
	case http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusGatewayTimeout, http.StatusBadGateway:
		return retryDelay(resp.Header.Get("Retry-After")), apiErr
	}
	return -1, apiErr
}

func (c *Client) resolve(path string, query url.Values) (string, error) {
	var u *url.URL
	var err error
	if strings.HasPrefix(path, "https://") || strings.HasPrefix(path, "http://") {
		u, err = url.Parse(path)
		if err != nil {
			return "", err
		}
		base, err := url.Parse(c.BaseURL)
		if err != nil {
			return "", err
		}
		// Never send the bearer token to a host other than Graph.
		if u.Host != base.Host || u.Scheme != base.Scheme {
			return "", fmt.Errorf("refusing to follow link to unexpected host %q", u.Host)
		}
	} else {
		u, err = url.Parse(c.BaseURL + "/" + strings.TrimLeft(path, "/"))
		if err != nil {
			return "", err
		}
	}
	if len(query) > 0 {
		q := u.Query()
		for k, vs := range query {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		// Graph expects literal "$" in OData parameter names.
		u.RawQuery = strings.ReplaceAll(q.Encode(), "%24", "$")
	}
	return u.String(), nil
}

func retryDelay(h string) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && s >= 0 {
		d := time.Duration(s) * time.Second
		if d > 60*time.Second {
			d = 60 * time.Second
		}
		return d
	}
	return 2 * time.Second
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// page is the envelope of a Graph collection response.
type page[T any] struct {
	Value    []T    `json:"value"`
	NextLink string `json:"@odata.nextLink"`
}

// Pages iterates over a Graph collection, calling fn with each page of items.
// Iteration stops when fn returns false or there are no more pages.
func Pages[T any](ctx context.Context, c *Client, path string, query url.Values, fn func([]T) bool) error {
	next := path
	q := query
	for next != "" {
		var p page[T]
		if err := c.Get(ctx, next, q, &p); err != nil {
			return err
		}
		if !fn(p.Value) {
			return nil
		}
		next, q = p.NextLink, nil
	}
	return nil
}

// All collects every item from a Graph collection.
func All[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	var out []T
	err := Pages(ctx, c, path, query, func(items []T) bool {
		out = append(out, items...)
		return true
	})
	return out, err
}
