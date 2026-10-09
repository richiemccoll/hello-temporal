// Package catapi is a small, dependency-free client for TheCatAPI (https://thecatapi.com).
//
// In this project the client never talks to the internet directly: its base URL
// points at Envoy's egress listener, which owns TLS, DNS, timeouts and retries
// for the upstream API.
package catapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DefaultBaseURL is the public TheCatAPI endpoint.
const DefaultBaseURL = "https://api.thecatapi.com"

// ErrNoCats is returned when the API answers successfully but with an empty list.
var ErrNoCats = errors.New("catapi: response contained no cats")

// Cat is a single image returned by TheCatAPI.
type Cat struct {
	ID     string `json:"id"`
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// StatusError is returned when the API responds with a non-2xx status code.
type StatusError struct {
	StatusCode int
	Body       string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("catapi: unexpected status %d: %s", e.StatusCode, e.Body)
}

// Retryable reports whether retrying the same request could succeed.
// Rate limiting and server errors are transient; other 4xx responses are not.
func (e *StatusError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// Client calls TheCatAPI. It is safe for concurrent use.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the optional x-api-key header. TheCatAPI works without one at a lower rate limit.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithHTTPClient overrides the HTTP client used for requests.
func WithHTTPClient(hc *http.Client) Option { return func(c *Client) { c.httpClient = hc } }

// New returns a Client for baseURL, e.g. "http://127.0.0.1:10000" (Envoy egress).
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// RandomCat returns one random cat image.
func (c *Client) RandomCat(ctx context.Context) (Cat, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/images/search?limit=1", nil)
	if err != nil {
		return Cat{}, fmt.Errorf("catapi: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "hello-temporal/1.0")
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Cat{}, fmt.Errorf("catapi: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Cat{}, &StatusError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}

	var cats []Cat
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&cats); err != nil {
		return Cat{}, fmt.Errorf("catapi: decode response: %w", err)
	}
	if len(cats) == 0 {
		return Cat{}, ErrNoCats
	}
	return cats[0], nil
}
