package request

import (
	"testing"
)

// Test mapping configuration based on real-world services
var testMapping = &HostnameMapping{
	Enabled: true,
	Mappings: map[string]*HostnameDomain{
		// Amazon
		"amazonaws.com": {
			ServiceName: "AWS",
			Patterns: []*HostnamePattern{
				{Regex: ".*s3.*", ServiceName: "AWS S3"},
				{Regex: ".*dynamodb.*", ServiceName: "AWS DynamoDB"},
				{Regex: ".*lambda.*", ServiceName: "AWS Lambda"},
				{Regex: ".*rds.*", ServiceName: "AWS RDS"},
				{Regex: ".*ec2.*", ServiceName: "AWS EC2"},
				{Regex: ".*ecs.*", ServiceName: "AWS ECS"},
				{Regex: ".*eks.*", ServiceName: "AWS EKS"},
			},
		},
		// Google
		"googleusercontent.com": {
			ServiceName: "Google User Content",
		},
		"googleapis.com": {
			ServiceName: "Google APIs",
			Patterns: []*HostnamePattern{
				{Regex: ".*storage.*", ServiceName: "Google Cloud Storage"},
				{Regex: ".*bigquery.*", ServiceName: "Google BigQuery"},
			},
		},
		// Azure services
		"azureedge.net": {
			ServiceName: "Azure CDN",
		},
		"azurewebsites.net": {
			ServiceName: "Azure Web Apps",
		},
		"blob.core.windows.net": {
			ServiceName: "Azure Blob Storage",
		},
		"cloudfront.net": {
			ServiceName: "CloudFront CDN",
		},
		// Other CDNs
		"fastly.com": {
			ServiceName: "Fastly CDN",
		},
		"cloudflare.com": {
			ServiceName: "Cloudflare CDN",
		},
	},
}

func TestHostnameMappingConfiguration(t *testing.T) {
	// Store original mapping to restore later
	originalMapping := hostnameMapping
	defer func() {
		hostnameMapping = originalMapping
	}()

	// Test setting valid configuration
	err := SetHostnameMapping(testMapping)
	if err != nil {
		t.Fatalf("failed to set hostname mapping: %v", err)
	}

	// Test getting configuration back
	retrieved := GetHostnameMapping()
	if !retrieved.Enabled {
		t.Error("expected mapping to be enabled")
	}

	expectedMappings := 9 // amazonaws.com, googleusercontent.com, googleapis.com, etc.
	if len(retrieved.Mappings) != expectedMappings {
		t.Errorf("expected %d mappings, got %d", expectedMappings, len(retrieved.Mappings))
	}

	// Test setting nil configuration
	err = SetHostnameMapping(nil)
	if err != nil {
		t.Errorf("unexpected error setting nil mapping: %v", err)
	}
}

func TestHostnameMappingBehavior(t *testing.T) {
	// Store original mapping to restore later
	originalMapping := hostnameMapping
	defer func() {
		hostnameMapping = originalMapping
	}()

	// Set up test mapping
	err := SetHostnameMapping(testMapping)
	if err != nil {
		t.Fatalf("failed to set hostname mapping: %v", err)
	}

	testCases := []struct {
		name     string
		hostname string
		expected string
	}{
		// AWS Services - pattern matching
		{
			name:     "AWS S3 service",
			hostname: "s3.amazonaws.com",
			expected: "AWS S3",
		},
		{
			name:     "AWS S3 regional",
			hostname: "s3.us-west-2.amazonaws.com",
			expected: "AWS S3",
		},
		{
			name:     "AWS DynamoDB",
			hostname: "dynamodb.us-east-1.amazonaws.com",
			expected: "AWS DynamoDB",
		},
		{
			name:     "AWS Lambda",
			hostname: "lambda.us-east-1.amazonaws.com",
			expected: "AWS Lambda",
		},
		{
			name:     "AWS RDS",
			hostname: "rds.amazonaws.com",
			expected: "AWS RDS",
		},
		{
			name:     "AWS EC2",
			hostname: "ec2.us-west-1.amazonaws.com",
			expected: "AWS EC2",
		},
		{
			name:     "AWS ECS",
			hostname: "ecs.us-east-1.amazonaws.com",
			expected: "AWS ECS",
		},
		{
			name:     "AWS EKS",
			hostname: "eks.us-west-2.amazonaws.com",
			expected: "AWS EKS",
		},
		{
			name:     "AWS other service falls back to default",
			hostname: "sns.amazonaws.com",
			expected: "AWS",
		},
		{
			name:     "AWS subdomain falls back to default",
			hostname: "monitoring.amazonaws.com",
			expected: "AWS",
		},

		// Google Services
		{
			name:     "Google User Content",
			hostname: "lh3.googleusercontent.com",
			expected: "Google User Content",
		},
		{
			name:     "Google Cloud Storage",
			hostname: "storage.googleapis.com",
			expected: "Google Cloud Storage",
		},
		{
			name:     "Google BigQuery",
			hostname: "bigquery.googleapis.com",
			expected: "Google BigQuery",
		},
		{
			name:     "Google APIs default",
			hostname: "compute.googleapis.com",
			expected: "Google APIs",
		},

		// Azure Services
		{
			name:     "Azure CDN",
			hostname: "example.azureedge.net",
			expected: "Azure CDN",
		},
		{
			name:     "Azure Web Apps",
			hostname: "myapp.azurewebsites.net",
			expected: "Azure Web Apps",
		},
		{
			name:     "Azure Blob Storage",
			hostname: "mystorageaccount.blob.core.windows.net",
			expected: "Azure Blob Storage",
		},

		// CDN Services
		{
			name:     "CloudFront CDN",
			hostname: "d1234567890.cloudfront.net",
			expected: "CloudFront CDN",
		},
		{
			name:     "Fastly CDN",
			hostname: "assets.fastly.com",
			expected: "Fastly CDN",
		},
		{
			name:     "Cloudflare CDN",
			hostname: "cdnjs.cloudflare.com",
			expected: "Cloudflare CDN",
		},

		// Non-matching hostnames
		{
			name:     "Unknown service returns original hostname",
			hostname: "api.example.com",
			expected: "api.example.com",
		},
		{
			name:     "Regular domain returns original",
			hostname: "www.github.com",
			expected: "www.github.com",
		},

		// Case sensitivity tests
		{
			name:     "Case insensitive matching for AWS",
			hostname: "S3.AMAZONAWS.COM",
			expected: "AWS S3",
		},
		{
			name:     "Mixed case Google service",
			hostname: "Storage.GoogleAPIs.com",
			expected: "Google Cloud Storage",
		},

		// Edge cases
		{
			name:     "Empty hostname",
			hostname: "",
			expected: "",
		},
	}

	currentMapping := GetHostnameMapping()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := mapHostnameWithConfig(tc.hostname, currentMapping)
			if result != tc.expected {
				t.Errorf("hostname mapping for %q = %q, want %q", tc.hostname, result, tc.expected)
			}
		})
	}
}

func TestHostnameMappingDisabled(t *testing.T) {
	// Store original mapping to restore later
	originalMapping := hostnameMapping
	defer func() {
		hostnameMapping = originalMapping
	}()

	// Create disabled mapping
	disabledMapping := &HostnameMapping{
		Enabled:  false,
		Mappings: testMapping.Mappings, // Same mappings but disabled
	}

	err := SetHostnameMapping(disabledMapping)
	if err != nil {
		t.Fatalf("failed to set disabled hostname mapping: %v", err)
	}

	testCases := []struct {
		hostname string
		expected string
	}{
		{"s3.amazonaws.com", "s3.amazonaws.com"},             // Should return original
		{"storage.googleapis.com", "storage.googleapis.com"}, // Should return original
		{"example.azureedge.net", "example.azureedge.net"},   // Should return original
	}

	currentMapping := GetHostnameMapping()
	for _, tc := range testCases {
		t.Run(tc.hostname, func(t *testing.T) {
			result := mapHostnameWithConfig(tc.hostname, currentMapping)
			if result != tc.expected {
				t.Errorf("disabled mapping for %q = %q, want %q (original hostname)", tc.hostname, result, tc.expected)
			}
		})
	}
}

func TestHostnameMappingInvalidRegex(t *testing.T) {
	// Store original mapping to restore later
	originalMapping := hostnameMapping
	defer func() {
		hostnameMapping = originalMapping
	}()

	// Create mapping with invalid regex
	invalidMapping := &HostnameMapping{
		Enabled: true,
		Mappings: map[string]*HostnameDomain{
			"example.com": {
				ServiceName: "example-service",
				Patterns: []*HostnamePattern{
					{Regex: "[invalid-regex", ServiceName: "invalid-service"},
				},
			},
		},
	}

	err := SetHostnameMapping(invalidMapping)
	if err == nil {
		t.Error("expected error when setting mapping with invalid regex, but got none")
	}

	// Verify original mapping is still in place after error
	current := GetHostnameMapping()
	if current.Enabled != originalMapping.Enabled {
		t.Error("original mapping should be preserved after configuration error")
	}
}
