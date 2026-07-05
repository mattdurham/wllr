package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OpenAI Codex (ChatGPT Plus/Pro subscription) OAuth via the device-code flow.
//
// Unlike Anthropic (browser + local callback), Codex offers a headless
// device-code flow: request a user code, show it + a verification URL, then poll
// until the user approves in a browser (on any machine). This is the
// remote/SSH-friendly path — nothing listens locally, so no port forwarding.
//
// Constants match pi's Codex client so wllr authenticates the same way. In the
// device flow the SERVER generates the PKCE code_verifier and returns it with
// the authorization code; the client then exchanges that pair for tokens.
//
// Endpoints are package vars (not consts) so tests can point them at httptest.
const codexOAuthClientID = "app_EMoamEEZ73f0CkXaXp7hrann"

var (
	codexTokenURL          = "https://auth.openai.com/oauth/token" //nolint:gosec // OAuth token endpoint URL, not a credential.
	codexDeviceUserCodeURL = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	codexDeviceTokenURL    = "https://auth.openai.com/api/accounts/deviceauth/token" //nolint:gosec // OAuth token endpoint URL, not a credential.
	// codexDeviceVerificationURI is shown to the user (where they enter the code).
	codexDeviceVerificationURI = "https://auth.openai.com/codex/device"
	// codexDeviceRedirectURI is echoed on the final code→token exchange.
	codexDeviceRedirectURI = "https://auth.openai.com/deviceauth/callback"
)

// codexDeviceTimeout bounds the whole device-code flow.
const codexDeviceTimeout = 15 * time.Minute

// codexDeviceAuth is the response to a user-code request.
type codexDeviceAuth struct {
	DeviceAuthID string
	UserCode     string
	// VerificationURI is where the user enters the code (constant, surfaced for the UI).
	VerificationURI string
	IntervalSeconds int
}

// codexToken is a Codex OAuth token set, plus the ChatGPT account id extracted
// from the access token's JWT claims (required to route Codex API calls).
type codexToken struct {
	Access    string
	Refresh   string
	AccountID string
	ExpiresAt int64
}

// startCodexDeviceAuth requests a device/user code from the Codex auth server.
func startCodexDeviceAuth(ctx context.Context, client *http.Client) (codexDeviceAuth, error) {
	body, _ := json.Marshal(map[string]string{oauthParamClientID: codexOAuthClientID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexDeviceUserCodeURL, bytes.NewReader(body))
	if err != nil {
		return codexDeviceAuth{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := doOAuthHTTP(client, req)
	if err != nil {
		return codexDeviceAuth{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return codexDeviceAuth{}, fmt.Errorf("codex device-code login is not enabled for this account")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexDeviceAuth{}, fmt.Errorf(
			"codex device-code request failed: status=%d body=%s",
			resp.StatusCode,
			string(data),
		)
	}
	var parsed struct {
		DeviceAuthID string          `json:"device_auth_id"`
		UserCode     string          `json:"user_code"`
		Interval     json.RawMessage `json:"interval"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return codexDeviceAuth{}, fmt.Errorf("codex device-code response invalid JSON: %w", err)
	}
	if parsed.DeviceAuthID == "" || parsed.UserCode == "" {
		return codexDeviceAuth{}, fmt.Errorf("codex device-code response missing fields: %s", string(data))
	}
	return codexDeviceAuth{
		DeviceAuthID:    parsed.DeviceAuthID,
		UserCode:        parsed.UserCode,
		IntervalSeconds: parseFlexibleInt(parsed.Interval),
		VerificationURI: codexDeviceVerificationURI,
	}, nil
}

// pollCodexDeviceAuth polls the device-token endpoint until the user approves,
// returning the authorization code and server-supplied PKCE verifier.
func pollCodexDeviceAuth(
	ctx context.Context,
	client *http.Client,
	device codexDeviceAuth,
) (code, verifier string, err error) {
	type pair struct{ code, verifier string }
	res, err := pollDeviceCode(ctx, devicePollOptions[pair]{
		IntervalSeconds:  device.IntervalSeconds,
		ExpiresInSeconds: int(codexDeviceTimeout / time.Second),
		Poll: func(ctx context.Context) devicePollResult[pair] {
			body, _ := json.Marshal(map[string]string{
				"device_auth_id": device.DeviceAuthID,
				"user_code":      device.UserCode,
			})
			req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, codexDeviceTokenURL, bytes.NewReader(body))
			if reqErr != nil {
				return devicePollResult[pair]{Status: deviceFailed, Err: reqErr}
			}
			req.Header.Set("Content-Type", "application/json")
			resp, doErr := doOAuthHTTP(client, req)
			if doErr != nil {
				return devicePollResult[pair]{Status: deviceFailed, Err: doErr}
			}
			defer func() { _ = resp.Body.Close() }()
			data, _ := io.ReadAll(resp.Body)

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				var ok struct {
					AuthorizationCode string `json:"authorization_code"`
					CodeVerifier      string `json:"code_verifier"`
				}
				if json.Unmarshal(data, &ok) != nil || ok.AuthorizationCode == "" || ok.CodeVerifier == "" {
					return devicePollResult[pair]{
						Status: deviceFailed,
						Err:    fmt.Errorf("invalid device-token response: %s", string(data)),
					}
				}
				return devicePollResult[pair]{
					Status: deviceComplete,
					Value:  pair{ok.AuthorizationCode, ok.CodeVerifier},
				}
			}
			// 403/404 mean not-yet-approved.
			if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
				return devicePollResult[pair]{Status: devicePending}
			}
			switch codexErrorCode(data) {
			case "deviceauth_authorization_pending":
				return devicePollResult[pair]{Status: devicePending}
			case "slow_down":
				return devicePollResult[pair]{Status: deviceSlowDown}
			}
			return devicePollResult[pair]{
				Status: deviceFailed,
				Err:    fmt.Errorf("codex device-token failed: status=%d body=%s", resp.StatusCode, string(data)),
			}
		},
	})
	if err != nil {
		return "", "", err
	}
	return res.code, res.verifier, nil
}

// exchangeCodexCode swaps the authorization code + verifier for tokens and
// extracts the ChatGPT account id from the access token.
func exchangeCodexCode(ctx context.Context, client *http.Client, code, verifier string) (codexToken, error) {
	form := url.Values{
		oauthParamGrantType:    {oauthGrantAuthorizationCode},
		oauthParamClientID:     {codexOAuthClientID},
		oauthParamCode:         {code},
		oauthParamCodeVerifier: {verifier},
		oauthParamRedirectURI:  {codexDeviceRedirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return codexToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := doOAuthHTTP(client, req)
	if err != nil {
		return codexToken{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexToken{}, fmt.Errorf("codex token exchange failed: status=%d body=%s", resp.StatusCode, string(data))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`  //nolint:gosec // OAuth token response field name.
		RefreshToken string `json:"refresh_token"` //nolint:gosec // OAuth token response field name.
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return codexToken{}, fmt.Errorf("codex token response invalid JSON: %w", err)
	}
	if parsed.AccessToken == "" {
		return codexToken{}, fmt.Errorf("codex token response missing access_token: %s", string(data))
	}
	accountID := chatGPTAccountID(parsed.AccessToken)
	if accountID == "" {
		return codexToken{}, fmt.Errorf("codex token missing chatgpt_account_id claim")
	}
	return codexToken{
		Access:    parsed.AccessToken,
		Refresh:   parsed.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli(),
		AccountID: accountID,
	}, nil
}

// refreshCodexToken exchanges a refresh token for a fresh Codex access token.
func refreshCodexToken(ctx context.Context, client *http.Client, refreshToken string) (codexToken, error) {
	form := url.Values{
		oauthParamGrantType:    {oauthGrantRefreshToken},
		oauthParamRefreshToken: {refreshToken},
		oauthParamClientID:     {codexOAuthClientID},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, codexTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return codexToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := doOAuthHTTP(client, req)
	if err != nil {
		return codexToken{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return codexToken{}, fmt.Errorf("codex token refresh failed: status=%d body=%s", resp.StatusCode, string(data))
	}
	var parsed struct {
		AccessToken  string `json:"access_token"`  //nolint:gosec // OAuth token response field name.
		RefreshToken string `json:"refresh_token"` //nolint:gosec // OAuth token response field name.
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return codexToken{}, fmt.Errorf("codex refresh response invalid JSON: %w", err)
	}
	if parsed.AccessToken == "" {
		return codexToken{}, fmt.Errorf("codex refresh response missing access_token: %s", string(data))
	}
	return codexToken{
		Access:    parsed.AccessToken,
		Refresh:   parsed.RefreshToken,
		ExpiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn)*time.Second - 5*time.Minute).UnixMilli(),
		AccountID: chatGPTAccountID(parsed.AccessToken),
	}, nil
}

// doOAuthHTTP performs an OAuth HTTP request with a default client/timeout.
func doOAuthHTTP(client *http.Client, req *http.Request) (*http.Response, error) {
	req.Header.Set("Accept", "application/json")
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return client.Do(req) //nolint:gosec // OAuth requests use provider endpoints or test-injected URLs.
}

// codexErrorCode extracts the `error` / `error.code` field from a JSON body.
func codexErrorCode(data []byte) string {
	var probe struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(data, &probe) != nil || len(probe.Error) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(probe.Error, &s) == nil {
		return s
	}
	var obj struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(probe.Error, &obj) == nil {
		return obj.Code
	}
	return ""
}

// chatGPTAccountID decodes the JWT access token and returns the ChatGPT account
// id from the "https://api.openai.com/auth" claim, or "" if absent.
func chatGPTAccountID(accessToken string) string {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			ChatGPTAccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	return claims.Auth.ChatGPTAccountID
}

// parseFlexibleInt reads an int from a JSON value that may be a number or a
// string (the Codex `interval` field is inconsistent). Returns 0 on failure.
func parseFlexibleInt(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return v
		}
	}
	return 0
}
