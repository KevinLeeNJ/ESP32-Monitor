// Package github implements GitHub desktop authentication using the official
// secretless device flow and the user's OS default browser.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/platform"
	"esp32-monitor/client/internal/platform/proxy"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
	Type             string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	Interval         int64  `json:"interval"`
}
type deviceResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Error           string `json:"error"`
}

func (c *Client) post(ctx context.Context, endpoint string, values url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return errors.New("invalid_auth_request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "esp32-monitor-client/0.1")
	return c.request(req, out)
}
func (c *Client) request(req *http.Request, out any) error {
	response, err := c.http.Do(req)
	if err != nil {
		for _, known := range []error{proxy.ErrAutomatic, proxy.ErrDetection, proxy.ErrInvalid} {
			if errors.Is(err, known) {
				return known
			}
		}
		return errors.New("auth_network_error")
	}
	defer response.Body.Close()
	if response.StatusCode == 429 {
		return errors.New("auth_rate_limited")
	}
	if response.StatusCode != 200 {
		return errors.New("auth_upstream_error")
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<16)+1))
	if err != nil || len(body) > 1<<16 || json.Unmarshal(body, out) != nil {
		return errors.New("auth_invalid_response")
	}
	return nil
}
func (c *Client) identity(ctx context.Context, token string) (string, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.userURL, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "esp32-monitor-client/0.1")
	var identity struct {
		Login string `json:"login"`
	}
	if err := c.request(req, &identity); err != nil {
		return "", err
	}
	if !platform.ValidGitHubLogin(identity.Login) {
		return "", errors.New("auth_invalid_identity")
	}
	return identity.Login, nil
}
