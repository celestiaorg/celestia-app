package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckEndpoint_Success(t *testing.T) {
	// Create a mock server that returns a successful response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := blockResultsResponse{
			Jsonrpc: "2.0",
			ID:      1,
		}
		response.Result.Height = "1034505"

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	result := checkEndpoint(context.Background(), server.URL, 1034505, 5*time.Second)

	if !result.success {
		t.Errorf("Expected success=true, got success=%v with error: %s", result.success, result.error)
	}
	if result.endpoint != server.URL {
		t.Errorf("Expected endpoint=%s, got endpoint=%s", server.URL, result.endpoint)
	}
	if result.height != 1034505 {
		t.Errorf("Expected height=1034505, got height=%d", result.height)
	}
	if result.latency == 0 {
		t.Error("Expected non-zero latency")
	}
}

func TestCheckEndpoint_HTTPError(t *testing.T) {
	// Create a mock server that returns an HTTP error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	result := checkEndpoint(context.Background(), server.URL, 1034505, 5*time.Second)

	if result.success {
		t.Error("Expected success=false for HTTP 404")
	}
	if result.error == "" {
		t.Error("Expected non-empty error message")
	}
}

func TestCheckEndpoint_RPCError(t *testing.T) {
	// Create a mock server that returns an RPC error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := blockResultsResponse{
			Jsonrpc: "2.0",
			ID:      1,
		}
		response.Error = &struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    string `json:"data"`
		}{
			Code:    -32603,
			Message: "Internal error",
			Data:    "block not found",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	result := checkEndpoint(context.Background(), server.URL, 1034505, 5*time.Second)

	if result.success {
		t.Error("Expected success=false for RPC error")
	}
	if result.error == "" {
		t.Error("Expected non-empty error message")
	}
	if result.latency == 0 {
		t.Error("Expected non-zero latency even for errors")
	}
}

func TestCheckEndpoint_Timeout(t *testing.T) {
	// Create a mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Use a very short timeout
	result := checkEndpoint(context.Background(), server.URL, 1034505, 100*time.Millisecond)

	if result.success {
		t.Error("Expected success=false for timeout")
	}
	if result.error == "" {
		t.Error("Expected non-empty error message for timeout")
	}
}

func TestCheckEndpoint_InvalidJSON(t *testing.T) {
	// Create a mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	result := checkEndpoint(context.Background(), server.URL, 1034505, 5*time.Second)

	if result.success {
		t.Error("Expected success=false for invalid JSON")
	}
	if result.error == "" {
		t.Error("Expected non-empty error message")
	}
}
