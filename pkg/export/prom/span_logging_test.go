package prom

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/beyla/v2/pkg/internal/request"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *SpanLoggingConfig
		expectError bool
		errorMsg    string
	}{
		{
			name:        "nil config should be valid",
			config:      nil,
			expectError: false,
		},
		{
			name: "empty config should be valid",
			config: &SpanLoggingConfig{
				Enabled:    false,
				Categories: map[string]*CategoryConfig{},
			},
			expectError: false,
		},
		{
			name: "valid categories should pass",
			config: &SpanLoggingConfig{
				Enabled: true,
				Categories: map[string]*CategoryConfig{
					"http_server": {
						Enabled:      true,
						LabelFilters: map[string]string{"server_addr": "localhost"},
					},
					"service_graph": {
						Enabled:      true,
						LabelFilters: map[string]string{"client": "test-service"},
					},
				},
			},
			expectError: false,
		},
		{
			name: "invalid category should fail",
			config: &SpanLoggingConfig{
				Enabled: true,
				Categories: map[string]*CategoryConfig{
					"invalid_category": {
						Enabled: true,
					},
				},
			},
			expectError: true,
			errorMsg:    "invalid category 'invalid_category'",
		},
		{
			name: "multiple invalid categories should fail",
			config: &SpanLoggingConfig{
				Enabled: true,
				Categories: map[string]*CategoryConfig{
					"invalid1": {Enabled: true},
					"invalid2": {Enabled: true},
				},
			},
			expectError: true,
		},
		{
			name: "mixed valid and invalid categories should fail",
			config: &SpanLoggingConfig{
				Enabled: true,
				Categories: map[string]*CategoryConfig{
					"http_server":      {Enabled: true},
					"invalid_category": {Enabled: true},
				},
			},
			expectError: true,
			errorMsg:    "invalid category 'invalid_category'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConfig(tt.config)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNewSpanLogger(t *testing.T) {
	tests := []struct {
		name   string
		config *SpanLoggingConfig
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name: "valid config",
			config: &SpanLoggingConfig{
				Enabled: true,
				Categories: map[string]*CategoryConfig{
					"http_server": {
						Enabled:      true,
						LabelFilters: map[string]string{"server_addr": "localhost"},
					},
				},
			},
		},
		{
			name: "config with nil categories",
			config: &SpanLoggingConfig{
				Enabled:    true,
				Categories: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewSpanLogger(tt.config)
			require.NotNil(t, logger)
			require.NotNil(t, logger.config)
			require.NotNil(t, logger.config.Categories)
			require.NotNil(t, logger.logger)
		})
	}
}

func TestSpanLoggingConfigJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonStr     string
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid JSON config",
			jsonStr: `{
				"enabled": true,
				"categories": {
					"http_server": {
						"enabled": true,
						"label_filters": {
							"server_addr": "localhost"
						}
					}
				}
			}`,
			expectError: false,
		},
		{
			name: "invalid category in JSON",
			jsonStr: `{
				"enabled": true,
				"categories": {
					"invalid_category": {
						"enabled": true
					}
				}
			}`,
			expectError: true,
			errorMsg:    "invalid category 'invalid_category'",
		},
		{
			name: "malformed JSON",
			jsonStr: `{
				"enabled": true,
				"categories": {
					"http_server": {
						"enabled": true,
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config SpanLoggingConfig
			err := json.Unmarshal([]byte(tt.jsonStr), &config)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotNil(t, config.Categories)
			}
		})
	}
}

func TestSpanLoggingConfigMarshalJSON(t *testing.T) {
	config := &SpanLoggingConfig{
		Enabled: true,
		Categories: map[string]*CategoryConfig{
			"http_server": {
				Enabled: true,
				LabelFilters: map[string]string{
					"server_addr": "localhost",
					"method":      "GET",
				},
			},
			"service_graph": {
				Enabled:      true,
				LabelFilters: map[string]string{},
			},
		},
	}

	data, err := json.Marshal(config)
	require.NoError(t, err)

	var unmarshaled SpanLoggingConfig
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, config.Enabled, unmarshaled.Enabled)
	assert.Equal(t, len(config.Categories), len(unmarshaled.Categories))
	assert.Equal(t, config.Categories["http_server"].Enabled, unmarshaled.Categories["http_server"].Enabled)
	assert.Equal(t, config.Categories["http_server"].LabelFilters, unmarshaled.Categories["http_server"].LabelFilters)
}

func TestInvalidCategoryError(t *testing.T) {
	err := &InvalidCategoryError{
		Category:        "invalid_test",
		ValidCategories: []string{"http_server", "http_client", "service_graph"},
	}

	expected := "invalid category 'invalid_test', valid categories are: http_server, http_client, service_graph"
	assert.Equal(t, expected, err.Error())
}

func TestGetSpanCategory(t *testing.T) {
	tests := []struct {
		name        string
		spanType    request.EventType
		expectedCat string
	}{
		{"HTTP server", request.EventTypeHTTP, "http_server"},
		{"HTTP client", request.EventTypeHTTPClient, "http_client"},
		{"GRPC server", request.EventTypeGRPC, "grpc_server"},
		{"GRPC client", request.EventTypeGRPCClient, "grpc_client"},
		{"SQL client", request.EventTypeSQLClient, "db_client"},
		{"Redis client", request.EventTypeRedisClient, "db_client"},
		{"Redis server", request.EventTypeRedisServer, "db_client"},
		{"Mongo client", request.EventTypeMongoClient, "db_client"},
		{"Kafka client", request.EventTypeKafkaClient, "messaging"},
		{"Kafka server", request.EventTypeKafkaServer, "messaging"},
		{"GPU kernel launch", request.EventTypeGPUKernelLaunch, "gpu"},
		{"GPU malloc", request.EventTypeGPUMalloc, "gpu"},
		{"Unknown type", request.EventType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &request.Span{Type: tt.spanType}
			category := getSpanCategory(span)
			assert.Equal(t, tt.expectedCat, category)
		})
	}
}

func TestUpdateAndGetConfig(t *testing.T) {
	logger := NewSpanLogger(nil)

	// Initial config should be disabled with empty categories
	initialConfig := logger.GetConfig()
	assert.False(t, initialConfig.Enabled)
	assert.Empty(t, initialConfig.Categories)

	// Update with new config
	newConfig := &SpanLoggingConfig{
		Enabled: true,
		Categories: map[string]*CategoryConfig{
			"http_server": {
				Enabled:      true,
				LabelFilters: map[string]string{"server_addr": "localhost"},
			},
		},
	}

	logger.UpdateConfig(newConfig)

	// Verify config was updated
	updatedConfig := logger.GetConfig()
	assert.True(t, updatedConfig.Enabled)
	assert.Len(t, updatedConfig.Categories, 1)
	assert.True(t, updatedConfig.Categories["http_server"].Enabled)
	assert.Equal(t, "localhost", updatedConfig.Categories["http_server"].LabelFilters["server_addr"])

	// Verify we get a copy, not the original
	updatedConfig.Enabled = false
	finalConfig := logger.GetConfig()
	assert.True(t, finalConfig.Enabled) // Should still be true
}
