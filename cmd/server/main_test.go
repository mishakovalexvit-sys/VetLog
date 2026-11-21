package main

import "testing"

func TestHealthURL(t *testing.T) {
	tests := []struct {
		name, env, want string
	}{
		{"default", "", "http://127.0.0.1:8080/healthz"},
		{"all interfaces", "0.0.0.0:9000", "http://127.0.0.1:9000/healthz"},
		{"ipv6 any", "[::]:8081", "http://127.0.0.1:8081/healthz"},
		{"explicit host", "10.0.0.5:8082", "http://10.0.0.5:8082/healthz"},
		{"port only", ":7000", "http://127.0.0.1:7000/healthz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HEALTH_URL", "")
			t.Setenv("HTTP_ADDR", tt.env)
			if got := healthURL(); got != tt.want {
				t.Fatalf("healthURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHealthURLOverride(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":9000")
	t.Setenv("HEALTH_URL", "http://app:8080/healthz")
	if got := healthURL(); got != "http://app:8080/healthz" {
		t.Fatalf("HEALTH_URL must win, got %q", got)
	}
}
