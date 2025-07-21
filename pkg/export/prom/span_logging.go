package prom

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/grafana/beyla/v2/pkg/internal/request"
)

// SpanLoggingConfig defines the configuration for span logging
type SpanLoggingConfig struct {
	// Enabled controls whether span logging is active
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Categories contains configuration for different metric categories
	Categories map[string]*CategoryConfig `yaml:"categories" json:"categories"`
}

// CategoryConfig defines the logging configuration for a specific metric category
type CategoryConfig struct {
	// Enabled controls whether logging is active for this category
	Enabled bool `yaml:"enabled" json:"enabled"`
	// LabelFilters contains key-value pairs that must match for a span to be logged
	// If empty, all spans in this category will be logged
	LabelFilters map[string]string `yaml:"label_filters" json:"label_filters"`
}

// SpanLogger handles thread-safe span logging with configurable filtering
type SpanLogger struct {
	mu     sync.RWMutex
	config *SpanLoggingConfig
	logger *slog.Logger
}

// NewSpanLogger creates a new SpanLogger with the given configuration
func NewSpanLogger(config *SpanLoggingConfig) *SpanLogger {
	if config == nil {
		config = &SpanLoggingConfig{
			Enabled:    false,
			Categories: make(map[string]*CategoryConfig),
		}
	}
	if config.Categories == nil {
		config.Categories = make(map[string]*CategoryConfig)
	}

	return &SpanLogger{
		config: config,
		logger: slog.With("component", "prom.SpanLogger"),
	}
}

// UpdateConfig atomically updates the logging configuration
func (sl *SpanLogger) UpdateConfig(config *SpanLoggingConfig) {
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if config == nil {
		config = &SpanLoggingConfig{
			Enabled:    false,
			Categories: make(map[string]*CategoryConfig),
		}
	}
	if config.Categories == nil {
		config.Categories = make(map[string]*CategoryConfig)
	}

	sl.config = config
}

// GetConfig returns a copy of the current configuration
func (sl *SpanLogger) GetConfig() *SpanLoggingConfig {
	sl.mu.RLock()
	defer sl.mu.RUnlock()

	// Deep copy the configuration
	configCopy := &SpanLoggingConfig{
		Enabled:    sl.config.Enabled,
		Categories: make(map[string]*CategoryConfig),
	}

	for k, v := range sl.config.Categories {
		if v != nil {
			labelFiltersCopy := make(map[string]string)
			for lk, lv := range v.LabelFilters {
				labelFiltersCopy[lk] = lv
			}
			configCopy.Categories[k] = &CategoryConfig{
				Enabled:      v.Enabled,
				LabelFilters: labelFiltersCopy,
			}
		}
	}

	return configCopy
}

// LogSpan logs a span if it matches the configured criteria
func (sl *SpanLogger) LogSpan(category string, span *request.Span, labelValuesFunc func() map[string]string) {
	sl.mu.RLock()

	if !sl.config.Enabled {
		sl.mu.RUnlock()
		return
	}

	categoryConfig, exists := sl.config.Categories[category]
	if !exists || categoryConfig == nil || !categoryConfig.Enabled {
		sl.mu.RUnlock()
		return
	}

	// Only compute label values if we might need them
	var labelValues map[string]string
	if len(categoryConfig.LabelFilters) > 0 {
		labelValues = labelValuesFunc()

		// Check if all label filters match
		for filterKey, filterValue := range categoryConfig.LabelFilters {
			if labelValue, exists := labelValues[filterKey]; !exists || labelValue != filterValue {
				sl.mu.RUnlock()
				return
			}
		}
	}

	sl.mu.RUnlock()

	// Build log fields from span directly
	fields := []interface{}{
		"category", category,
		"span", span, // Log the entire span
	}

	// Add label values if computed
	if labelValues != nil {
		for k, v := range labelValues {
			fields = append(fields, "label_"+k, v)
		}
	}

	sl.logger.Info("span_logged", fields...)
}

// getSpanCategory determines the category for a given span type
func getSpanCategory(span *request.Span) string {
	switch span.Type {
	case request.EventTypeHTTP:
		return "http_server"
	case request.EventTypeHTTPClient:
		return "http_client"
	case request.EventTypeGRPC:
		return "grpc_server"
	case request.EventTypeGRPCClient:
		return "grpc_client"
	case request.EventTypeSQLClient, request.EventTypeRedisClient, request.EventTypeRedisServer, request.EventTypeMongoClient:
		return "db_client"
	case request.EventTypeKafkaClient, request.EventTypeKafkaServer:
		return "messaging"
	case request.EventTypeGPUKernelLaunch, request.EventTypeGPUMalloc:
		return "gpu"
	default:
		return "unknown"
	}
}

// ValidateConfig validates a SpanLoggingConfig for correctness
func ValidateConfig(config *SpanLoggingConfig) error {
	if config == nil {
		return nil
	}

	validCategories := []string{
		"http_server", "http_client", "grpc_server", "grpc_client",
		"db_client", "messaging", "gpu", "service_graph",
	}

	for category := range config.Categories {
		if !slices.Contains(validCategories, category) {
			return &InvalidCategoryError{Category: category, ValidCategories: validCategories}
		}
	}

	return nil
}

// InvalidCategoryError represents an error for invalid category names
type InvalidCategoryError struct {
	Category        string
	ValidCategories []string
}

func (e *InvalidCategoryError) Error() string {
	return "invalid category '" + e.Category + "', valid categories are: " + strings.Join(e.ValidCategories, ", ")
}

// MarshalJSON implements json.Marshaler for SpanLoggingConfig
func (c *SpanLoggingConfig) MarshalJSON() ([]byte, error) {
	type Alias SpanLoggingConfig
	return json.Marshal((*Alias)(c))
}

// UnmarshalJSON implements json.Unmarshaler for SpanLoggingConfig
func (c *SpanLoggingConfig) UnmarshalJSON(data []byte) error {
	type Alias SpanLoggingConfig
	aux := (*Alias)(c)
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	return ValidateConfig(c)
}

// CreateSpanLoggingHandler creates a single HTTP handler for managing span logging configuration
// It supports both GET (retrieve config) and PUT (update config) methods
func CreateSpanLoggingHandler(spanLogger *SpanLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			config := spanLogger.GetConfig()
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(config); err != nil {
				http.Error(w, "Failed to encode configuration", http.StatusInternalServerError)
				return
			}

		case http.MethodPut:
			var config SpanLoggingConfig
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}

			if err := ValidateConfig(&config); err != nil {
				http.Error(w, "Invalid configuration: "+err.Error(), http.StatusBadRequest)
				return
			}

			spanLogger.UpdateConfig(&config)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "updated"})

		default:
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
