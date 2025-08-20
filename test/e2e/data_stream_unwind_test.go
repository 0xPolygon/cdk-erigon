package e2e

import (
	"os"
	"strings"
	"testing"
)

func TestAddFlag(t *testing.T) {
	// File to modify
	filePath := "../config/test.erigon.seq.config.yaml"

	// Read the file contents
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal("Error reading file:", err)
	}

	// Convert data to string for easier manipulation
	content := string(data)

	// Add the new configuration line at the end of the file
	content = strings.TrimSpace(content) + "\nzkevm.data-stream-unwind-to-block: 5"

	// Write the modified content back to the file
	err = os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		t.Fatal("Error writing file:", err)
	}

	t.Log("Successfully added zkevm.data-stream-unwind-to-block: 5 to test.erigon.seq.config.yaml")
}
