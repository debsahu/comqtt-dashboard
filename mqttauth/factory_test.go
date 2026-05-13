package mqttauth

import (
	"strings"
	"testing"
)

func TestNewRoutesByKind(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		want    string
		wantErr string
	}{
		{
			name: "file",
			cfg:  Config{Kind: "file", File: &FileConfig{Path: "/tmp/x.yml"}},
			want: "file",
		},
		{
			name: "redis",
			cfg:  Config{Kind: "redis", Redis: &RedisConfig{Addr: "127.0.0.1:6379"}},
			want: "redis",
		},
		{
			name: "mysql",
			cfg:  Config{Kind: "mysql", SQL: &SQLConfig{Driver: "mysql", DSN: "u@/db"}},
			want: "mysql",
		},
		{
			name: "postgres",
			cfg:  Config{Kind: "postgres", SQL: &SQLConfig{Driver: "postgres", DSN: "postgres://u@/db"}},
			want: "postgres",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := New(tc.cfg)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if got := b.Kind(); got != tc.want {
				t.Errorf("Kind()=%q want %q", got, tc.want)
			}
			if err := b.Close(); err != nil {
				t.Errorf("Close(): %v", err)
			}
		})
	}
}

func TestNewRejectsMissingSubConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantSub string
	}{
		{"file missing", Config{Kind: "file"}, "Config.File"},
		{"redis missing", Config{Kind: "redis"}, "Config.Redis"},
		{"mysql missing", Config{Kind: "mysql"}, "Config.SQL"},
		{"postgres wrong driver", Config{Kind: "postgres", SQL: &SQLConfig{Driver: "mysql"}}, "Driver=postgres"},
		{"unknown kind", Config{Kind: "ldap"}, "unknown Kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.cfg)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("err=%q want substring %q", err, tc.wantSub)
			}
		})
	}
}
