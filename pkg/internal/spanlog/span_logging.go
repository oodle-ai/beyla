package spanlog

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/grafana/beyla/v2/pkg/internal/request"
)

// SpanLoggingConfig configures span logging for debugging purposes
type SpanLoggingConfig struct {
	// Enabled controls whether span logging is active
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Categories defines per-category logging configuration
	Categories map[string]*CategoryConfig `yaml:"categories" json:"categories"`
}

// CategoryConfig defines logging configuration for a specific span category
type CategoryConfig struct {
	// Enabled controls whether this category should be logged
	Enabled bool `yaml:"enabled" json:"enabled"`
	// LabelFilters specifies label-based filtering for this category
	// Only spans where ALL specified labels match will be logged
	LabelFilters map[string]string `yaml:"label_filters" json:"label_filters"`
}

// SpanLogger handles span logging with configurable filtering
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
		logger: slog.With("component", "spanlog.SpanLogger"),
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
	if !exists || !categoryConfig.Enabled {
		sl.mu.RUnlock()
		return
	}

	// Check label filters if any are configured
	if len(categoryConfig.LabelFilters) > 0 {
		// Compute label values only if we have filters to check
		labelValues := labelValuesFunc()

		for filterKey, filterValue := range categoryConfig.LabelFilters {
			if actualValue, exists := labelValues[filterKey]; !exists || actualValue != filterValue {
				sl.mu.RUnlock()
				return
			}
		}
	}

	sl.mu.RUnlock()

	// Log the span as-is
	sl.logger.Info("span logged",
		"category", category,
		"span", span)
}

// ValidConfig validates span logging configuration
func ValidConfig(config *SpanLoggingConfig) error {
	if config == nil {
		return nil
	}

	if config.Categories == nil {
		return nil
	}

	for categoryName := range config.Categories {
		if !isValidCategory(categoryName) {
			return &InvalidCategoryError{Category: categoryName}
		}
	}

	return nil
}

// InvalidCategoryError represents an error for invalid span category
type InvalidCategoryError struct {
	Category string
}

func (e *InvalidCategoryError) Error() string {
	return fmt.Sprintf("invalid span category: %s. Valid categories are: %s",
		e.Category, strings.Join(validCategories, ", "))
}

// Valid span categories based on request event types
var validCategories = []string{
	"http_server",
	"http_client",
	"grpc_server",
	"grpc_client",
	"db_client",
	"messaging",
	"service_graph",
	"gpu",
}

func isValidCategory(category string) bool {
	for _, valid := range validCategories {
		if category == valid {
			return true
		}
	}
	return false
}

// GetSpanCategory returns the appropriate category for a span based on its type
func GetSpanCategory(span *request.Span) string {
	switch span.Type {
	case request.EventTypeHTTP:
		return "http_server"
	case request.EventTypeHTTPClient:
		return "http_client"
	case request.EventTypeGRPC:
		return "grpc_server"
	case request.EventTypeGRPCClient:
		return "grpc_client"
	case request.EventTypeRedisClient, request.EventTypeSQLClient,
		request.EventTypeRedisServer, request.EventTypeMongoClient:
		return "db_client"
	case request.EventTypeKafkaClient, request.EventTypeKafkaServer:
		return "messaging"
	case request.EventTypeGPUKernelLaunch, request.EventTypeGPUMalloc:
		return "gpu"
	default:
		return ""
	}
}

// MarshalJSON implements json.Marshaler for SpanLoggingConfig
func (c *SpanLoggingConfig) MarshalJSON() ([]byte, error) {
	type Alias SpanLoggingConfig
	return json.Marshal((*Alias)(c))
}

// UnmarshalJSON implements json.Unmarshaler for SpanLoggingConfig
func (c *SpanLoggingConfig) UnmarshalJSON(data []byte) error {
	type Alias SpanLoggingConfig
	alias := (*Alias)(c)
	if err := json.Unmarshal(data, alias); err != nil {
		return err
	}

	return ValidConfig(c)
}

// CreateSpanLoggingHandler creates an HTTP handler for span logging configuration
func CreateSpanLoggingHandler(spanLogger *SpanLogger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// Return current configuration
			config := spanLogger.GetConfig()
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(config); err != nil {
				http.Error(w, "Failed to encode configuration", http.StatusInternalServerError)
				return
			}

		case http.MethodPut:
			// Update configuration
			var newConfig SpanLoggingConfig
			if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
				http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
				return
			}

			if err := ValidConfig(&newConfig); err != nil {
				http.Error(w, "Invalid configuration: "+err.Error(), http.StatusBadRequest)
				return
			}

			spanLogger.UpdateConfig(&newConfig)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Configuration updated successfully"))

		default:
			w.Header().Set("Allow", "GET, PUT")
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
}
