package config

import "testing"

func TestSMTPPort(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"587", 587, false},
		{"465", 0, true}, // неявный TLS не поддерживается
		{"25", 25, false},
		{"0", 0, true},
		{"70000", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := smtpPort(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("port = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error without DATABASE_URL")
	}
}

func TestLoadRejects465(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("SMTP_PORT", "465")
	if _, err := Load(); err == nil {
		t.Fatal("SMTP_PORT=465 must be rejected at startup")
	}
}
