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

func TestConcurrencyLimits(t *testing.T) {
	for _, tc := range []struct {
		tts, stt         string
		wantTTS, wantSTT int
		invalid          bool
	}{
		{"", "", 8, 64, false}, {"1000", "1000", 1000, 1000, false}, {"2", "3", 2, 3, false},
		{"0", "64", 0, 0, true}, {"8", "1001", 0, 0, true}, {"wrong", "64", 0, 0, true},
	} {
		cfg, err := config.Load(func(key string) string {
			switch key {
			case "MAX_TTS_CONCURRENCY":
				return tc.tts
			case "MAX_STT_CONNECTIONS":
				return tc.stt
			}
			return ""
		})
		if tc.invalid {
			if err == nil {
				t.Fatal("accepted invalid capacity")
			}
			continue
		}
		if err != nil || cfg.MaxTTSConcurrency != tc.wantTTS || cfg.MaxSTTConnections != tc.wantSTT {
			t.Fatalf("incorrect capacity: %+v %v", cfg, err)
		}
	}
}
