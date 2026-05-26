package core

import (
	"testing"
)

func TestProcessEndpoint(t *testing.T) {
	tests := []struct {
		name           string
		endpoint       string
		configInsecure bool
		wantEndpoint   string
		wantInsecure   bool
		wantErr        bool
	}{
		{
			name:           "Empty endpoint",
			endpoint:       "",
			configInsecure: false,
			wantEndpoint:   "",
			wantInsecure:   false,
			wantErr:        false,
		},
		{
			name:           "No scheme - host only",
			endpoint:       "otel.jmdt.io:4317",
			configInsecure: false,
			wantEndpoint:   "otel.jmdt.io:4317",
			wantInsecure:   false,
			wantErr:        false,
		},
		{
			name:           "No scheme - explicit insecure config",
			endpoint:       "localhost:4317",
			configInsecure: true,
			wantEndpoint:   "localhost:4317",
			wantInsecure:   true,
			wantErr:        false,
		},
		{
			name:           "HTTPS scheme - secure override",
			endpoint:       "https://otel.jmdt.io",
			configInsecure: true, // Config says insecure, but scheme says secure
			wantEndpoint:   "otel.jmdt.io:443",
			wantInsecure:   false, // Should be false (secure)
			wantErr:        false,
		},
		{
			name:           "HTTPS scheme - with explicit port",
			endpoint:       "https://otel.jmdt.io:8443",
			configInsecure: true,
			wantEndpoint:   "otel.jmdt.io:8443",
			wantInsecure:   false,
			wantErr:        false,
		},
		{
			name:           "HTTP scheme - insecure override",
			endpoint:       "http://localhost",
			configInsecure: false, // Config says secure, but scheme says insecure
			wantEndpoint:   "localhost:80",
			wantInsecure:   true, // Should be true (insecure)
			wantErr:        false,
		},
		{
			name:           "HTTP scheme - with explicit port",
			endpoint:       "http://localhost:4318",
			configInsecure: false,
			wantEndpoint:   "localhost:4318",
			wantInsecure:   true,
			wantErr:        false,
		},
		{
			name:           "Unsupported scheme",
			endpoint:       "ftp://otel.jmdt.io:21",
			configInsecure: false,
			wantEndpoint:   "",
			wantInsecure:   false,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEndpoint, gotInsecure, err := processEndpoint(tt.endpoint, tt.configInsecure)

			if tt.wantErr {
				if err == nil {
					t.Errorf("processEndpoint() error = nil, wantErr %v", tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Errorf("processEndpoint() unexpected error = %v", err)
				return
			}

			if gotEndpoint != tt.wantEndpoint {
				t.Errorf("processEndpoint() gotEndpoint = %v, want %v", gotEndpoint, tt.wantEndpoint)
			}
			if gotInsecure != tt.wantInsecure {
				t.Errorf("processEndpoint() gotInsecure = %v, want %v", gotInsecure, tt.wantInsecure)
			}
		})
	}
}
func TestInjectAuth(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		username   string
		password   string
		protocol   string
		wantKey    string
		wantValue  string
		wantLength int
	}{
		{
			name:       "Header-First priority over Basic Auth (capitalized)",
			headers:    map[string]string{"Authorization": "Bearer pre-existing-token"},
			username:   "user",
			password:   "pass",
			protocol:   "http",
			wantKey:    "Authorization",
			wantValue:  "Bearer pre-existing-token",
			wantLength: 1,
		},
		{
			name:       "Header-First priority over Basic Auth (lowercase)",
			headers:    map[string]string{"authorization": "Bearer lower-token"},
			username:   "user",
			password:   "pass",
			protocol:   "grpc",
			wantKey:    "authorization",
			wantValue:  "Bearer lower-token",
			wantLength: 1,
		},
		{
			name:       "Basic Auth fallback when no token is in headers",
			headers:    nil,
			username:   "user",
			password:   "pass",
			protocol:   "http",
			wantKey:    "Authorization",
			wantValue:  "Basic dXNlcjpwYXNz",
			wantLength: 1,
		},
		{
			name:       "gRPC uses lowercase authorization for Basic Auth and custom tokens",
			headers:    map[string]string{"Authorization": "Bearer test-grpc-token"},
			username:   "",
			password:   "",
			protocol:   "grpc",
			wantKey:    "authorization", // normalized to lower case by the function
			wantValue:  "Bearer test-grpc-token",
			wantLength: 1,
		},
		{
			name:       "No auth info",
			headers:    nil,
			protocol:   "http",
			wantLength: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy the original input to ensure immutability is respected
			originalInput := make(map[string]string)
			for k, v := range tt.headers {
				originalInput[k] = v
			}

			got := injectAuth(tt.headers, tt.username, tt.password, tt.protocol)

			// 1. Verify Immutability
			if tt.headers != nil {
				if len(originalInput) != len(tt.headers) {
					t.Errorf("injectAuth() mutated original input length! Expected %v, got %v", len(originalInput), len(tt.headers))
				}
				for k, v := range originalInput {
					if tt.headers[k] != v {
						t.Errorf("injectAuth() mutated original input value for key %q", k)
					}
				}
			}

			// 2. Verify Output
			if len(got) != tt.wantLength {
				t.Errorf("injectAuth() length = %v, want %v", len(got), tt.wantLength)
				return
			}
			if tt.wantLength > 0 {
				if val, ok := got[tt.wantKey]; !ok || val != tt.wantValue {
					t.Errorf("injectAuth() %s = %v, want %v. out map: %+v", tt.wantKey, val, tt.wantValue, got)
				}
			}
		})
	}
}

func TestParseSampler(t *testing.T) {
	tests := []struct {
		name    string
		sampler string
		// Since sdktrace.Sampler is an interface, we can verify its description
		wantDesc string
	}{
		{
			name:     "Empty defaults to AlwaysOn",
			sampler:  "",
			wantDesc: "AlwaysOnSampler",
		},
		{
			name:     "Explicit always",
			sampler:  "always",
			wantDesc: "AlwaysOnSampler",
		},
		{
			name:     "Explicit never",
			sampler:  "never",
			wantDesc: "AlwaysOffSampler",
		},
		{
			name:     "Valid ratio",
			sampler:  "ratio:0.15",
			wantDesc: "TraceIDRatioBased{0.15}",
		},
		{
			name:     "Invalid ratio format falls back to AlwaysOn",
			sampler:  "ratio:not-a-number",
			wantDesc: "AlwaysOnSampler",
		},
		{
			name:     "Unknown string falls back to AlwaysOn",
			sampler:  "random_string",
			wantDesc: "AlwaysOnSampler",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSampler(tt.sampler)
			if got == nil {
				t.Fatal("parseSampler() returned nil")
			}

			desc := got.Description()
			if desc != tt.wantDesc {
				t.Errorf("parseSampler() description = %v, want %v", desc, tt.wantDesc)
			}
		})
	}
}
