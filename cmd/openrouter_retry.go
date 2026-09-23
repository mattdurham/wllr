package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"
)

// openRouterRetryClient retries one 402 caused by OpenRouter's temporary
// in-flight spending budget. Other credit failures remain actionable errors.
type openRouterRetryClient struct {
	base *http.Client
}

func (c openRouterRetryClient) Do(req *http.Request) (*http.Response, error) {
	resp, err := c.base.Do(req)
	if err != nil || resp.StatusCode != http.StatusPaymentRequired ||
		(req.Body != nil && req.GetBody == nil) {
		return resp, err
	}

	const maxErrorBody = 64 << 10
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))
	if readErr != nil || len(body) > maxErrorBody || !isOpenRouterInFlightBudget(body) {
		resp.Body = restoredResponseBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), Closer: resp.Body}
		return resp, nil
	}

	delay := 500 * time.Millisecond
	if value := resp.Header.Get("Retry-After"); value != "" {
		if seconds, parseErr := strconv.ParseFloat(value, 64); parseErr == nil {
			delay = time.Duration(seconds * float64(time.Second))
		} else if retryAt, parseErr := http.ParseTime(value); parseErr == nil {
			delay = time.Until(retryAt)
		}
	}
	if delay < 0 {
		delay = 0
	}
	if delay > 30*time.Second {
		resp.Body = restoredResponseBody{Reader: io.MultiReader(bytes.NewReader(body), resp.Body), Closer: resp.Body}
		return resp, nil
	}

	if err := resp.Body.Close(); err != nil {
		return nil, err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}

	retry := req.Clone(req.Context())
	if req.GetBody != nil {
		retry.Body, err = req.GetBody()
		if err != nil {
			return nil, err
		}
	}
	return c.base.Do(retry)
}

type restoredResponseBody struct {
	io.Reader
	io.Closer
}

func isOpenRouterInFlightBudget(body []byte) bool {
	var response struct {
		Error struct {
			Metadata struct {
				LimitSource string `json:"limit_source"`
			} `json:"metadata"`
		} `json:"error"`
	}
	return json.Unmarshal(body, &response) == nil &&
		response.Error.Metadata.LimitSource == "openrouter_in_flight_budget"
}
