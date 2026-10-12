package qoder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExchangePATPostsSignedCenterRequest(t *testing.T) {
	var capturedHeader http.Header
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeader = r.Header.Clone()
		decoded, err := DecodeString(readAllString(t, r))
		if err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if err := json.Unmarshal([]byte(decoded), &capturedBody); err != nil {
			t.Fatalf("parse body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                 "user-1",
			"name":               "User",
			"userType":           "personal_standard",
			"securityOauthToken": "token-1",
			"refreshToken":       "refresh-1",
		})
	}))
	defer server.Close()

	identity, err := ExchangePATContext(context.Background(), "pat-1", &MachineIdentity{
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		MachineType:  "5",
	}, server.URL, nil)
	if err != nil {
		t.Fatalf("ExchangePAT: %v", err)
	}
	if identity.SecurityOauthToken != "token-1" {
		t.Fatalf("token = %q, want token-1", identity.SecurityOauthToken)
	}
	if capturedHeader.Get("signature") == "" {
		t.Fatal("signature header is empty")
	}
	payload, _ := capturedBody["payload"].(string)
	var inner map[string]any
	if err := json.Unmarshal([]byte(payload), &inner); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if inner["personalToken"] != "pat-1" {
		t.Fatalf("personalToken = %v, want pat-1", inner["personalToken"])
	}
	if inner["needRefresh"] != false {
		t.Fatalf("needRefresh = %v, want false", inner["needRefresh"])
	}
}

func TestExchangePATContextUsesProvidedDoerAndContext(t *testing.T) {
	type contextKey string
	ctx := context.WithValue(context.Background(), contextKey("marker"), "ctx-value")
	called := false
	doer := func(req *http.Request) (*http.Response, error) {
		called = true
		if req.Context().Value(contextKey("marker")) != "ctx-value" {
			t.Fatalf("request context marker = %v, want ctx-value", req.Context().Value(contextKey("marker")))
		}
		if req.URL.String() != "https://center.example/algo/api/v3/user/jobToken?Encode=1" {
			t.Fatalf("request URL = %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"id":"user-1",
				"name":"User",
				"userType":"personal_standard",
				"securityOauthToken":"token-1",
				"refreshToken":"refresh-1"
			}`)),
			Request: req,
		}, nil
	}

	identity, err := ExchangePATContext(ctx, "pat-1", &MachineIdentity{
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		MachineType:  "5",
	}, "https://center.example", doer)
	if err != nil {
		t.Fatalf("ExchangePATContext: %v", err)
	}
	if !called {
		t.Fatal("provided doer was not called")
	}
	if identity.SecurityOauthToken != "token-1" {
		t.Fatalf("token = %q, want token-1", identity.SecurityOauthToken)
	}
}

func TestRefreshSessionPostsRefreshPayload(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoded, err := DecodeString(readAllString(t, r))
		if err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if err := json.Unmarshal([]byte(decoded), &capturedBody); err != nil {
			t.Fatalf("parse body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":                 "user-2",
			"name":               "User 2",
			"userType":           "personal_pro",
			"securityOauthToken": "new-token",
			"refreshToken":       "new-refresh",
		})
	}))
	defer server.Close()

	identity, err := RefreshSessionContext(context.Background(), "old-refresh", "old-token", &MachineIdentity{
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		MachineType:  "5",
	}, server.URL, nil)
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if identity.SecurityOauthToken != "new-token" {
		t.Fatalf("token = %q, want new-token", identity.SecurityOauthToken)
	}
	if identity.RefreshToken != "new-refresh" {
		t.Fatalf("refresh token = %q, want new-refresh", identity.RefreshToken)
	}
	payload, _ := capturedBody["payload"].(string)
	var inner map[string]any
	if err := json.Unmarshal([]byte(payload), &inner); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if inner["refreshToken"] != "old-refresh" {
		t.Fatalf("refreshToken = %v, want old-refresh", inner["refreshToken"])
	}
	if inner["securityOauthToken"] != "old-token" {
		t.Fatalf("securityOauthToken = %v, want old-token", inner["securityOauthToken"])
	}
	if inner["needRefresh"] != true {
		t.Fatalf("needRefresh = %v, want true", inner["needRefresh"])
	}
}

func TestRefreshSessionContextRejectsEmptySecurityOauthToken(t *testing.T) {
	_, err := RefreshSessionContext(context.Background(), "old-refresh", "old-token", &MachineIdentity{
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		MachineType:  "5",
	}, "https://center.example", func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"id":"user-2",
				"name":"User 2",
				"userType":"personal_pro",
				"securityOauthToken":"",
				"refreshToken":"new-refresh"
			}`)),
			Request: req,
		}, nil
	})

	if err == nil {
		t.Fatal("expected empty securityOauthToken error")
	}
	if !strings.Contains(err.Error(), "missing securityOauthToken") {
		t.Fatalf("error = %q, want missing securityOauthToken", err.Error())
	}
}

func TestRefreshSessionContextRejectsEmptyInputSecurityOauthToken(t *testing.T) {
	called := false
	_, err := RefreshSessionContext(context.Background(), "old-refresh", "  ", &MachineIdentity{
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		MachineType:  "5",
	}, "https://center.example", func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})

	if err == nil {
		t.Fatal("expected empty input securityOauthToken error")
	}
	if !strings.Contains(err.Error(), "requires securityOauthToken") {
		t.Fatalf("error = %q, want requires securityOauthToken", err.Error())
	}
	if called {
		t.Fatal("doer should not be called for empty input securityOauthToken")
	}
}

func TestRefreshSessionContextPropagatesCanceledContextToDoer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	doer := func(req *http.Request) (*http.Response, error) {
		called = true
		if req.Context().Err() != context.Canceled {
			t.Fatalf("request context err = %v, want context.Canceled", req.Context().Err())
		}
		return nil, req.Context().Err()
	}

	_, err := RefreshSessionContext(ctx, "old-refresh", "old-token", &MachineIdentity{
		MachineID:    "machine-1",
		MachineToken: "machine-token",
		MachineType:  "5",
	}, "https://center.example", doer)

	if !called {
		t.Fatal("provided doer was not called")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RefreshSessionContext error = %v, want context.Canceled", err)
	}
}
