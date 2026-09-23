package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenRouterRetryClientRetriesOnlyInFlightBudget(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstBody  string
		wantCalls  int
		wantStatus int
	}{
		{"in-flight budget", `{"error":{"metadata":{"limit_source":"openrouter_in_flight_budget"}}}`, 2, http.StatusOK},
		{"actual credits", `{"error":{"metadata":{"limit_source":"openrouter_credits"}}}`, 1, http.StatusPaymentRequired},
		{"key limit", `{"error":{"metadata":{"limit_source":"openrouter_key_limit"}}}`, 1, http.StatusPaymentRequired},
		{"unknown 402", `{"error":{"message":"Insufficient credits"}}`, 1, http.StatusPaymentRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				if err != nil || string(body) != "prompt" {
					t.Errorf("request body = %q, err = %v", body, err)
				}
				if calls == 1 {
					w.Header().Set("Retry-After", "0")
					w.WriteHeader(http.StatusPaymentRequired)
					_, _ = io.WriteString(w, tc.firstBody)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer server.Close()
			req, err := http.NewRequestWithContext(
				context.Background(),
				http.MethodPost,
				server.URL,
				strings.NewReader("prompt"),
			)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := (openRouterRetryClient{base: server.Client()}).Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if calls != tc.wantCalls || resp.StatusCode != tc.wantStatus {
				t.Errorf("calls = %d, status = %d; want %d, %d", calls, resp.StatusCode, tc.wantCalls, tc.wantStatus)
			}
			if tc.wantCalls == 1 {
				body, err := io.ReadAll(resp.Body)
				if err != nil || string(body) != tc.firstBody {
					t.Errorf("preserved error body = %q, err = %v", body, err)
				}
			}
		})
	}
}
