package client

import (
	"os"
	"testing"
	"time"
)

// Test configuration
var (
	testRPCURL      = getTestRPCURL()
	testAddress     = "0x742d35Cc6634C0532925a3b8D4C9db96C4b4d8b6" // Test address
	testBlockNumber = "latest"                                     // Test block number
)

// getTestRPCURL gets the test RPC URL
func getTestRPCURL() string {
	// Priority: environment variable
	if url := os.Getenv("TEST_RPC_URL"); url != "" {
		return url
	}

	// Default: local test node
	//return "http://127.0.0.1:8123"
	return ""
}

func TestConcurrentTimeWait(t *testing.T) {
	// Check if RPC URL is available
	if testRPCURL == "" {
		t.Skip("TEST_RPC_URL not set, skipping test")
	}

	t.Logf("Concurrent test RPC server: %s", testRPCURL)

	// Concurrent test parameters
	concurrency := 40
	requestsPerGoroutine := 1000

	t.Logf("Starting %d concurrent goroutines, each sending %d requests", concurrency, requestsPerGoroutine)

	// Use channel for synchronization
	done := make(chan bool, concurrency)

	startTime := time.Now()

	// Start concurrent requests
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			for j := 0; j < requestsPerGoroutine; j++ {
				_, err := JSONRPCCall(testRPCURL, "eth_getTransactionCount", testAddress, testBlockNumber)
				if err != nil {
					t.Errorf("Goroutine %d, request %d failed: %v", id, j, err)
				}
				time.Sleep(100 * time.Millisecond)
			}
			done <- true
		}(i)
	}

	// Wait for all requests to complete
	for i := 0; i < concurrency; i++ {
		<-done
	}

	duration := time.Since(startTime)
	t.Logf("Completed %d concurrent requests in %v", concurrency*requestsPerGoroutine, duration)

	// Wait for connection state to stabilize
	time.Sleep(3 * time.Second)
}
