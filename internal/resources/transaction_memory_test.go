/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package resources

import (
	"testing"
)

func TestCalculateTransactionMemoryLimit(t *testing.T) {
	tests := []struct {
		name     string
		heapSize string
		config   map[string]string
		expected string
	}{
		{
			name:     "4G heap should give 2.8G transaction memory (70%)",
			heapSize: "4g",
			config:   map[string]string{},
			expected: "2.8g",
		},
		{
			name:     "8G heap should give 5.6G transaction memory (70%)",
			heapSize: "8g",
			config:   map[string]string{},
			expected: "5.6g",
		},
		{
			name:     "User-provided value should be preserved",
			heapSize: "4g",
			config: map[string]string{
				"dbms.memory.transaction.total.max": "3g",
			},
			expected: "3g",
		},
		{
			name:     "Invalid heap size should return safe default",
			heapSize: "invalid",
			config:   map[string]string{},
			expected: "2g",
		},
		{
			name:     "Empty heap size should return safe default",
			heapSize: "",
			config:   map[string]string{},
			expected: "2g",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateTransactionMemoryLimit(tt.heapSize, tt.config)
			if result != tt.expected {
				t.Errorf("calculateTransactionMemoryLimit(%q, %v) = %q, want %q", tt.heapSize, tt.config, result, tt.expected)
			}
		})
	}
}

func TestCalculatePerTransactionLimit(t *testing.T) {
	tests := []struct {
		name     string
		heapSize string
		config   map[string]string
		expected string
	}{
		{
			name:     "4G heap should give ~286M per-transaction (10% of 70% of heap)",
			heapSize: "4g",
			config:   map[string]string{},
			expected: "286.7m",
		},
		{
			name:     "1G heap should give 256M per-transaction (minimum enforced)",
			heapSize: "1g",
			config:   map[string]string{},
			expected: "256m",
		},
		{
			name:     "User-provided value should be preserved",
			heapSize: "4g",
			config: map[string]string{
				"db.memory.transaction.max": "500m",
			},
			expected: "500m",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculatePerTransactionLimit(tt.heapSize, tt.config)
			if result != tt.expected {
				t.Errorf("calculatePerTransactionLimit(%q, %v) = %q, want %q", tt.heapSize, tt.config, result, tt.expected)
			}
		})
	}
}

func TestParseMemoryString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int64
	}{
		{
			name:     "4g should parse to 4GB in bytes",
			input:    "4g",
			expected: 4 * 1024 * 1024 * 1024,
		},
		{
			name:     "512m should parse to 512MB in bytes",
			input:    "512m",
			expected: 512 * 1024 * 1024,
		},
		{
			name:     "2.5g should parse correctly",
			input:    "2.5g",
			expected: int64(2.5 * 1024 * 1024 * 1024),
		},
		{
			name:     "empty string should return 0",
			input:    "",
			expected: 0,
		},
		{
			name:     "invalid string should return 0",
			input:    "invalid",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseMemoryString(tt.input)
			if result != tt.expected {
				t.Errorf("parseMemoryString(%q) = %d, want %d", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatMemorySizeForNeo4j(t *testing.T) {
	tests := []struct {
		name     string
		input    int64
		expected string
	}{
		{
			name:     "4GB should format as 4g",
			input:    4 * 1024 * 1024 * 1024,
			expected: "4g",
		},
		{
			name:     "2.5GB should format as 2.5g",
			input:    int64(2.5 * 1024 * 1024 * 1024),
			expected: "2.5g",
		},
		{
			name:     "512MB should format as 512m",
			input:    512 * 1024 * 1024,
			expected: "512m",
		},
		{
			name:     "256KB should format as 256k",
			input:    256 * 1024,
			expected: "256k",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatMemorySizeForNeo4j(tt.input)
			if result != tt.expected {
				t.Errorf("formatMemorySizeForNeo4j(%d) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// Simple string contains implementation for testing
func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
