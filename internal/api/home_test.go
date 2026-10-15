package api_test

import (
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestDocumentationDisallowsProxyRewriting(t *testing.T) {
	w := request(api.New(), "GET", "/", "")
	if w.Header().Get("Cache-Control") != "no-transform" {
		t.Fatal("proxies must not rewrite the file=@sos.pcm code example")
	}
}
