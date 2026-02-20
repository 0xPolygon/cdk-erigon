package utils

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
)

// LoadConfig reads a YAML config file and returns it as a map.
func LoadConfig(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := make(map[string]interface{})
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

// DetectNetworkType inspects config keys to determine the network type.
func DetectNetworkType(cfg map[string]interface{}) string {
	if _, ok := cfg["override.pmtenabledblock"]; ok {
		return "Type-1"
	}
	if _, ok := cfg["zkevm.pessimistic-fork-number"]; ok {
		return "PP"
	}
	if _, ok := cfg["override.sovereignmodeblock"]; ok {
		return "Sovereign"
	}
	return "FEP"
}
