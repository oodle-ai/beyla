package request

import (
	"regexp"
	"strings"
)

// HostnamePattern represents a regex pattern and its associated service name
type HostnamePattern struct {
	Regex       string `yaml:"regex" json:"regex"`
	ServiceName string `yaml:"service_name" json:"service_name"`
	compiled    *regexp.Regexp
}

// HostnameDomain represents a domain with optional sub-patterns
type HostnameDomain struct {
	ServiceName string             `yaml:"service_name" json:"service_name"`
	Patterns    []*HostnamePattern `yaml:"patterns,omitempty" json:"patterns,omitempty"`
}

// HostnameMapping represents the complete hostname mapping configuration
type HostnameMapping struct {
	Enabled  bool                       `yaml:"enabled" json:"enabled"`
	Mappings map[string]*HostnameDomain `yaml:"mappings" json:"mappings"`
}

// Default hostname mapping - can be overridden by configuration
var hostnameMapping = HostnameMapping{
	Enabled:  false,
}

// compilePatterns compiles all regex patterns in the mapping
func (hm *HostnameMapping) compilePatterns() error {
	if hm.Mappings == nil {
		return nil
	}
	for _, domain := range hm.Mappings {
		for _, pattern := range domain.Patterns {
			compiled, err := regexp.Compile(pattern.Regex)
			if err != nil {
				return err
			}
			pattern.compiled = compiled
		}
	}
	return nil
}

// mapHostnameWithConfig maps a hostname using the provided configuration
func mapHostnameWithConfig(hostname string, mapping *HostnameMapping) string {
	if hostname == "" || mapping == nil || !mapping.Enabled || mapping.Mappings == nil {
		return hostname
	}

	// Convert to lowercase for case-insensitive matching
	lowerHostname := strings.ToLower(hostname)

	// Check exact domain matches and suffix matches
	for domain, config := range mapping.Mappings {
		// Standard suffix matching for most domains
		if strings.HasSuffix(lowerHostname, "."+domain) || lowerHostname == domain {
			// Check if there are specific patterns to match
			if len(config.Patterns) > 0 {
				for _, pattern := range config.Patterns {
					if pattern.compiled != nil && pattern.compiled.MatchString(lowerHostname) {
						return pattern.ServiceName
					}
				}
			}
			// Return the default service name for this domain
			return config.ServiceName
		}
	}

	return hostname
}

// SetHostnameMapping allows overriding the default mapping
func SetHostnameMapping(mapping *HostnameMapping) error {
	if mapping == nil {
		return nil
	}
	if err := mapping.compilePatterns(); err != nil {
		return err
	}
	hostnameMapping = *mapping
	return nil
}

// GetHostnameMapping returns the current default mapping
func GetHostnameMapping() *HostnameMapping {
	return &hostnameMapping
}
