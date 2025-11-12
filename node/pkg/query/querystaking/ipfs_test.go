package querystaking

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

// TestBytes32ToCIDString tests CID conversion from bytes32 to string
func TestBytes32ToCIDString(t *testing.T) {
	tests := []struct {
		name        string
		hashHex     string
		wantPrefix  string // CIDv1 base32 always starts with "bafk"
		wantError   bool
	}{
		{
			name:       "valid sha256 hash",
			hashHex:    "2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
			wantPrefix: "bafk",
			wantError:  false,
		},
		{
			name:       "zero hash",
			hashHex:    "0000000000000000000000000000000000000000000000000000000000000000",
			wantPrefix: "bafk",
			wantError:  false,
		},
		{
			name:       "max hash",
			hashHex:    "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			wantPrefix: "bafk",
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hashBytes, err := hex.DecodeString(tt.hashHex)
			if err != nil {
				t.Fatalf("failed to decode hex: %v", err)
			}

			var hash32 [32]byte
			copy(hash32[:], hashBytes)

			got, err := bytes32ToCIDString(hash32)

			if tt.wantError {
				if err == nil {
					t.Errorf("bytes32ToCIDString() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("bytes32ToCIDString() unexpected error = %v", err)
				return
			}

			// Check that CID starts with expected prefix
			if len(got) < len(tt.wantPrefix) || got[:len(tt.wantPrefix)] != tt.wantPrefix {
				t.Errorf("bytes32ToCIDString() = %v, want prefix %v", got, tt.wantPrefix)
			}

			// Check that CID has reasonable length (base32 encoded CIDv1 should be ~59 chars)
			if len(got) < 50 || len(got) > 70 {
				t.Errorf("bytes32ToCIDString() length = %d, expected ~59 chars", len(got))
			}

			t.Logf("CID: %s", got)
		})
	}
}

// TestIPFSClientFetch tests the IPFS client fetch functionality with a mock server
func TestIPFSClientFetch(t *testing.T) {
	tests := []struct {
		name           string
		serverResponse string
		serverStatus   int
		wantError      bool
		errorType      string
	}{
		{
			name:           "valid JSON response",
			serverResponse: `{"EVM":{"5000":"1 QPM","50000":"1 QPS"},"Solana":{"12500":"1 QPM","125000":"1 QPS"}}`,
			serverStatus:   http.StatusOK,
			wantError:      false,
		},
		{
			name:           "invalid JSON",
			serverResponse: `{invalid json`,
			serverStatus:   http.StatusOK,
			wantError:      true,
			errorType:      "json_parse",
		},
		{
			name:           "404 not found",
			serverResponse: `not found`,
			serverStatus:   http.StatusNotFound,
			wantError:      true,
			errorType:      "http_status",
		},
		{
			name:           "500 server error",
			serverResponse: `internal error`,
			serverStatus:   http.StatusInternalServerError,
			wantError:      true,
			errorType:      "http_status",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock HTTP server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverStatus)
				w.Write([]byte(tt.serverResponse))
			}))
			defer server.Close()

			// Create IPFS client pointing to mock server
			logger := zap.NewNop()
			client := NewIPFSClient(server.URL+"/", 5*time.Second, logger)

			// Create a test hash
			var testHash [32]byte
			copy(testHash[:], []byte("test hash for fetching"))

			// Attempt to fetch
			ctx := context.Background()
			result, err := client.FetchConversionTable(ctx, testHash)

			if tt.wantError {
				if err == nil {
					t.Errorf("FetchConversionTable() error = nil, wantError = true")
				}
				return
			}

			if err != nil {
				t.Errorf("FetchConversionTable() unexpected error = %v", err)
				return
			}

			if result == nil {
				t.Errorf("FetchConversionTable() result = nil, want non-nil")
			}
		})
	}
}

// TestIPFSClientCache tests that the IPFS client properly caches results
func TestIPFSClientCache(t *testing.T) {
	requestCount := 0

	// Create mock HTTP server that counts requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"EVM":{"5000":"1 QPM"},"Solana":{"12500":"1 QPM"}}`))
	}))
	defer server.Close()

	logger := zap.NewNop()
	client := NewIPFSClient(server.URL+"/", 5*time.Second, logger)

	var testHash [32]byte
	copy(testHash[:], []byte("test hash for caching"))

	ctx := context.Background()

	// First fetch should hit the server
	result1, err := client.FetchConversionTable(ctx, testHash)
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	if result1 == nil {
		t.Fatal("first fetch returned nil result")
	}
	if requestCount != 1 {
		t.Errorf("first fetch: requestCount = %d, want 1", requestCount)
	}

	// Second fetch should use cache
	result2, err := client.FetchConversionTable(ctx, testHash)
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}
	if result2 == nil {
		t.Fatal("second fetch returned nil result")
	}
	if requestCount != 1 {
		t.Errorf("second fetch: requestCount = %d, want 1 (should use cache)", requestCount)
	}

	// Results should be the same object from cache
	if result1 != result2 {
		t.Error("cached result is not the same object")
	}
}

// TestIPFSClientTimeout tests that the IPFS client respects timeouts
func TestIPFSClientTimeout(t *testing.T) {
	// Create mock HTTP server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"EVM":{"5000":"1 QPM"}}`))
	}))
	defer server.Close()

	logger := zap.NewNop()
	// Set very short timeout
	client := NewIPFSClient(server.URL+"/", 100*time.Millisecond, logger)

	var testHash [32]byte
	copy(testHash[:], []byte("test hash for timeout"))

	ctx := context.Background()
	_, err := client.FetchConversionTable(ctx, testHash)

	if err == nil {
		t.Error("FetchConversionTable() expected timeout error, got nil")
	}
}
