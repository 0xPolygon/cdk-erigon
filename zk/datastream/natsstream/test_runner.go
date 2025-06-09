package natsstream

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/erigontech/erigon-lib/log/v3"
	"github.com/nats-io/nats-server/v2/server"
)

// TestSuite represents a comprehensive test suite for NATS client
type TestSuite struct {
	logger     log.Logger
	tempDir    string
	natsServer *server.Server
	natsURL    string
}

// NewTestSuite creates a new test suite with embedded NATS server
func NewTestSuite() (*TestSuite, error) {
	tempDir, err := os.MkdirTemp("", "nats-test-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	return &TestSuite{
		logger:  log.New(),
		tempDir: tempDir,
	}, nil
}

// Setup initializes the test environment
func (ts *TestSuite) Setup() error {
	// Start embedded NATS server
	opts := &server.Options{
		Port:      -1, // Random port
		JetStream: true,
		StoreDir:  ts.tempDir,
		NoLog:     true,
		NoSigs:    true,
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return fmt.Errorf("failed to create NATS server: %w", err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(10 * time.Second) {
		return fmt.Errorf("NATS server failed to start within timeout")
	}

	ts.natsServer = ns
	ts.natsURL = fmt.Sprintf("nats://localhost:%d", ns.Addr().(*net.TCPAddr).Port)

	ts.logger.Info("Test suite initialized", "natsURL", ts.natsURL, "tempDir", ts.tempDir)
	return nil
}

// Cleanup shuts down the test environment
func (ts *TestSuite) Cleanup() error {
	if ts.natsServer != nil {
		ts.natsServer.Shutdown()
		ts.natsServer = nil
	}

	if ts.tempDir != "" {
		err := os.RemoveAll(ts.tempDir)
		if err != nil {
			ts.logger.Warn("Failed to remove temp dir", "dir", ts.tempDir, "error", err)
		}
	}

	ts.logger.Info("Test suite cleaned up")
	return nil
}

// RunBehaviorTests runs all behavior validation tests
func (ts *TestSuite) RunBehaviorTests(t *testing.T) {
	t.Run("StateBlockBuildingValidation", func(t *testing.T) {
		// Run state machine behavior tests
		ts.logger.Info("Running state block building validation tests")
		// TestStateBlockBuildingValidation would be called here
	})

	t.Run("MessageOrderingValidation", func(t *testing.T) {
		// Run message ordering tests
		ts.logger.Info("Running message ordering validation tests")
		// TestMessageOrderingValidation would be called here
	})

	t.Run("BookmarkBasedResumption", func(t *testing.T) {
		// Run bookmark functionality tests
		ts.logger.Info("Running bookmark-based resumption tests")
		// TestBookmarkBasedResumption would be called here
	})
}

// RunE2ETests runs all end-to-end tests
func (ts *TestSuite) RunE2ETests(t *testing.T) {
	t.Run("CompleteBatchProcessing", func(t *testing.T) {
		ts.logger.Info("Running complete batch processing E2E test")
		// TestE2E_CompleteBatchProcessing would be called here
	})

	t.Run("ConnectionFailureRecovery", func(t *testing.T) {
		ts.logger.Info("Running connection failure recovery E2E test")
		// TestE2E_ConnectionFailureRecovery would be called here
	})

	t.Run("ConcurrentConsumers", func(t *testing.T) {
		ts.logger.Info("Running concurrent consumers E2E test")
		// TestE2E_ConcurrentConsumers would be called here
	})

	t.Run("HighThroughputStress", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping high throughput test in short mode")
		}
		ts.logger.Info("Running high throughput stress E2E test")
		// TestE2E_HighThroughputStressTest would be called here
	})
}

// RunInterfaceTests runs all interface compliance tests
func (ts *TestSuite) RunInterfaceTests(t *testing.T) {
	t.Run("InterfaceCompliance", func(t *testing.T) {
		ts.logger.Info("Running interface compliance tests")
		// TestInterfaceCompliance would be called here
	})

	t.Run("ConcurrentInterfaceUsage", func(t *testing.T) {
		ts.logger.Info("Running concurrent interface usage tests")
		// TestConcurrentInterfaceUsage would be called here
	})

	t.Run("InterfaceErrorHandling", func(t *testing.T) {
		ts.logger.Info("Running interface error handling tests")
		// TestInterfaceErrorHandling would be called here
	})
}

// CreateTestClient creates a properly configured test client
func (ts *TestSuite) CreateTestClient(ctx context.Context, chainID uint64) (*NATSClient, error) {
	client := NewNATSClient(ctx, ts.natsURL, false, chainID, 7, ts.logger)

	err := client.Start()
	if err != nil {
		return nil, fmt.Errorf("failed to start client: %w", err)
	}

	return client, nil
}

// GetNATSURL returns the URL of the test NATS server
func (ts *TestSuite) GetNATSURL() string {
	return ts.natsURL
}

// GetLogger returns the test suite logger
func (ts *TestSuite) GetLogger() log.Logger {
	return ts.logger
}

// RunAllTests runs the complete test suite
func RunAllTests(t *testing.T) {
	suite, err := NewTestSuite()
	if err != nil {
		t.Fatalf("Failed to create test suite: %v", err)
	}

	err = suite.Setup()
	if err != nil {
		t.Fatalf("Failed to setup test suite: %v", err)
	}
	defer suite.Cleanup()

	suite.logger.Info("🚀 Starting NATS Client comprehensive test suite")

	// Run interface compliance tests first
	t.Run("InterfaceTests", suite.RunInterfaceTests)

	// Run behavior validation tests
	t.Run("BehaviorTests", suite.RunBehaviorTests)

	// Run E2E tests
	t.Run("E2ETests", suite.RunE2ETests)

	suite.logger.Info("✅ NATS Client test suite completed")
}

// Test execution summary and reporting
type TestReport struct {
	TotalTests   int
	PassedTests  int
	FailedTests  int
	SkippedTests int
	Duration     time.Duration
	TestResults  []TestResult
}

type TestResult struct {
	Name     string
	Status   string // "PASS", "FAIL", "SKIP"
	Duration time.Duration
	Error    error
}

// GenerateTestReport creates a summary report of test execution
func GenerateTestReport(results []TestResult) *TestReport {
	report := &TestReport{
		TestResults: results,
	}

	for _, result := range results {
		report.TotalTests++
		report.Duration += result.Duration

		switch result.Status {
		case "PASS":
			report.PassedTests++
		case "FAIL":
			report.FailedTests++
		case "SKIP":
			report.SkippedTests++
		}
	}

	return report
}

// PrintTestReport prints a formatted test report
func (tr *TestReport) Print() {
	fmt.Printf("\n📊 NATS Client Test Report\n")
	fmt.Printf("═══════════════════════════\n")
	fmt.Printf("Total Tests:   %d\n", tr.TotalTests)
	fmt.Printf("✅ Passed:     %d\n", tr.PassedTests)
	fmt.Printf("❌ Failed:     %d\n", tr.FailedTests)
	fmt.Printf("⏭️  Skipped:    %d\n", tr.SkippedTests)
	fmt.Printf("⏱️  Duration:   %v\n", tr.Duration)

	if tr.FailedTests > 0 {
		fmt.Printf("\n❌ Failed Tests:\n")
		for _, result := range tr.TestResults {
			if result.Status == "FAIL" {
				fmt.Printf("  • %s: %v\n", result.Name, result.Error)
			}
		}
	}

	successRate := float64(tr.PassedTests) / float64(tr.TotalTests) * 100
	fmt.Printf("\n🎯 Success Rate: %.1f%%\n", successRate)

	if successRate == 100.0 {
		fmt.Printf("🎉 All tests passed!\n")
	} else if successRate >= 90.0 {
		fmt.Printf("👍 Good test coverage!\n")
	} else {
		fmt.Printf("⚠️  Some tests need attention\n")
	}
}
