package config_test

import (
	"github.com/HashNuke/morsecode-audio/internal/config"
	"testing"
)

func TestListenerConfiguration(t *testing.T) {
	for _, tc := range []struct {
		host, port, want string
		invalid          bool
	}{
		{"", "", "127.0.0.1:8787", false},
		{"0.0.0.0", "19437", "0.0.0.0:19437", false},
		{"::1", "8788", "[::1]:8788", false},
		{"", "zero", "", true}, {"", "0", "", true}, {"", "65536", "", true},
		{"localhost:8787", "8787", "", true},
	} {
		c, err := config.Load(func(key string) string {
			if key == "HOST" {
				return tc.host
			}
			if key == "PORT" {
				return tc.port
			}
			return ""
		})
		if tc.invalid {
			if err == nil {
				t.Errorf("accepted invalid listener: %+v", tc)
			}
			continue
		}
		if err != nil || c.Address() != tc.want {
			t.Errorf("%+v: %v %v", tc, c, err)
		}
	}
}
