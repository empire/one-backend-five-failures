// Package thirdparty calls two APIs that this service does not own:
// a safety scan (when a link is created) and an analytics service (when a link is opened).
//
// Every call carries the ID of the event that caused it (header X-Event-ID), so the provider can spot a repeat.
package thirdparty

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to the scan and analytics APIs.
type Client struct {
	http         *http.Client
	scanURL      string
	analyticsURL string
}

// New returns a client with a 5-second timeout per call.
func New(scanURL, analyticsURL string) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        256,
				MaxIdleConnsPerHost: 256,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		scanURL:      scanURL,
		analyticsURL: analyticsURL,
	}
}

// Scan asks the safety-scan API whether a URL is safe.
func (c *Client) Scan(ctx context.Context, longURL, eventID string) (bool, error) {
	var out struct {
		Safe bool `json:"safe"`
	}
	if err := c.post(ctx, c.scanURL, eventID, map[string]string{"url": longURL}, &out); err != nil {
		return false, err
	}
	return out.Safe, nil
}

// Click tells the analytics API that a short link was opened.
func (c *Client) Click(ctx context.Context, code, eventID string) error {
	return c.post(ctx, c.analyticsURL, eventID, map[string]string{"code": code}, nil)
}

func (c *Client) post(ctx context.Context, url, eventID string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Event-ID", eventID)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("%s: status %d", url, resp.StatusCode)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
