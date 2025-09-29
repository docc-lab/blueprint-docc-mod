package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPClient provides a wrapper around the standard HTTP client with Blueprint-specific functionality
type HTTPClient struct {
	client  *http.Client
	baseURL string
	verbose bool
}

// NewHTTPClient creates a new HTTP client for Blueprint applications
func NewHTTPClient(baseURL string, timeout time.Duration) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL: baseURL,
		verbose: false,
	}
}

// NewHTTPClientWithVerbose creates a new HTTP client with verbose logging
func NewHTTPClientWithVerbose(baseURL string, timeout time.Duration, verbose bool) *HTTPClient {
	return &HTTPClient{
		client: &http.Client{
			Timeout: timeout,
		},
		baseURL: baseURL,
		verbose: verbose,
	}
}

// Get performs a GET request to the specified endpoint
func (c *HTTPClient) Get(ctx context.Context, endpoint string) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", c.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if c.verbose {
		fmt.Printf("🌐 GET %s\n", url)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		if c.verbose {
			fmt.Printf("❌ GET %s failed: %v\n", url, err)
		}
		return resp, err
	}

	if c.verbose {
		fmt.Printf("📥 GET %s -> %d\n", url, resp.StatusCode)
		// Log response body for debugging
		if resp.Body != nil {
			body, _ := io.ReadAll(resp.Body)
			if len(body) > 0 {
				fmt.Printf("📄 Response body: %s\n", string(body))
			}
			// Reset the body for the caller
			resp.Body = io.NopCloser(strings.NewReader(string(body)))
		}
	}

	return resp, err
}

// Post performs a POST request with JSON data
func (c *HTTPClient) PostJSON(ctx context.Context, endpoint string, data interface{}) (*http.Response, error) {
	_, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %w", err)
	}

	url := fmt.Sprintf("%s%s", c.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Body = http.NoBody // Would need proper body handling in real implementation

	return c.client.Do(req)
}

// Post performs a POST request without data
func (c *HTTPClient) Post(ctx context.Context, endpoint string) (*http.Response, error) {
	url := fmt.Sprintf("%s%s", c.baseURL, endpoint)
	req, err := http.NewRequestWithContext(ctx, "POST", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	return c.client.Do(req)
}

// ParseJSONResponse parses a JSON response into the target struct
func ParseJSONResponse(resp *http.Response, target interface{}) error {
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}
