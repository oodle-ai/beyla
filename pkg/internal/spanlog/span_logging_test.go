package spanlog

import (
	"encoding/json"
	"testing"

	"github.com/grafana/beyla/v2/pkg/internal/request"
)

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      *SpanLoggingConfig
		shouldError bool
	}{
		{
			name:        "nil config should be valid",
			config:      nil,
			shouldError: false,
		},
		{
			name:        "empty config should be valid",
			config:      &SpanLoggingConfig{},
			shouldError: false,
		},
		{
			name: "valid categories should pass",
			config: &SpanLoggingConfig{
				Categories: map[string]*CategoryConfig{
					"http_server": {Enabled: true},
					"grpc_client": {Enabled: false},
				},
			},
			shouldError: false,
		},
		{
			name: "invalid category should fail",
			config: &SpanLoggingConfig{
				Categories: map[string]*CategoryConfig{
					"invalid_category": {Enabled: true},
				},
			},
			shouldError: true,
		},
		{
			name: "multiple invalid categories should fail",
			config: &SpanLoggingConfig{
				Categories: map[string]*CategoryConfig{
					"invalid1": {Enabled: true},
					"invalid2": {Enabled: true},
				},
			},
			shouldError: true,
		},
		{
			name: "mixed valid and invalid categories should fail",
			config: &SpanLoggingConfig{
				Categories: map[string]*CategoryConfig{
					"http_server":      {Enabled: true},
					"invalid_category": {Enabled: true},
				},
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidConfig(tt.config)
			if tt.shouldError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
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
						Enabled: true,
						LabelFilters: map[string]string{
							"server_addr": "localhost:8080",
						},
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
			spanLogger := NewSpanLogger(tt.config)
			if spanLogger == nil {
				t.Error("Expected non-nil SpanLogger")
			}

			// Verify config is properly initialized
			config := spanLogger.GetConfig()
			if config == nil {
				t.Error("Expected non-nil config from GetConfig()")
			}
			if config.Categories == nil {
				t.Error("Expected non-nil Categories map")
			}
		})
	}
}

func TestSpanLoggingConfigJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonStr     string
		shouldError bool
	}{
		{
			name: "valid JSON config",
			jsonStr: `{
				"enabled": true,
				"categories": {
					"http_server": {
						"enabled": true,
						"label_filters": {
							"server_addr": "localhost:8080"
						}
					}
				}
			}`,
			shouldError: false,
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
			shouldError: true,
		},
		{
			name:        "malformed JSON",
			jsonStr:     `{"enabled": true, "categories":`,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config SpanLoggingConfig
			err := json.Unmarshal([]byte(tt.jsonStr), &config)

			if tt.shouldError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
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
					"server_addr": "localhost:8080",
				},
			},
		},
	}

	data, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	var unmarshaled SpanLoggingConfig
	err = json.Unmarshal(data, &unmarshaled)
	if err != nil {
		t.Fatalf("Failed to unmarshal config: %v", err)
	}

	if unmarshaled.Enabled != config.Enabled {
		t.Errorf("Expected Enabled=%v, got %v", config.Enabled, unmarshaled.Enabled)
	}
}

func TestInvalidCategoryError(t *testing.T) {
	err := &InvalidCategoryError{Category: "invalid_test"}
	expected := "invalid span category: invalid_test. Valid categories are: http_server, http_client, grpc_server, grpc_client, db_client, messaging, service_graph, gpu"
	if err.Error() != expected {
		t.Errorf("Expected error message %q, got %q", expected, err.Error())
	}
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
		{"Unknown type", request.EventType(0), ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			span := &request.Span{Type: tt.spanType}
			category := GetSpanCategory(span)
			if category != tt.expectedCat {
				t.Errorf("Expected category %q, got %q", tt.expectedCat, category)
			}
		})
	}
}

func TestUpdateAndGetConfig(t *testing.T) {
	spanLogger := NewSpanLogger(nil)

	// Test initial config
	initialConfig := spanLogger.GetConfig()
	if initialConfig.Enabled {
		t.Error("Expected initial config to be disabled")
	}

	// Test update
	newConfig := &SpanLoggingConfig{
		Enabled: true,
		Categories: map[string]*CategoryConfig{
			"http_server": {
				Enabled: true,
				LabelFilters: map[string]string{
					"server_addr": "localhost:8080",
				},
			},
		},
	}

	spanLogger.UpdateConfig(newConfig)

	// Test get updated config
	updatedConfig := spanLogger.GetConfig()
	if !updatedConfig.Enabled {
		t.Error("Expected updated config to be enabled")
	}

	if len(updatedConfig.Categories) != 1 {
		t.Errorf("Expected 1 category, got %d", len(updatedConfig.Categories))
	}

	httpServerConfig, exists := updatedConfig.Categories["http_server"]
	if !exists {
		t.Error("Expected http_server category to exist")
	}

	if !httpServerConfig.Enabled {
		t.Error("Expected http_server category to be enabled")
	}

	if httpServerConfig.LabelFilters["server_addr"] != "localhost:8080" {
		t.Errorf("Expected server_addr filter to be localhost:8080, got %s",
			httpServerConfig.LabelFilters["server_addr"])
	}
}
