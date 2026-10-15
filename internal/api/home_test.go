package api_test

import (
	"strings"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

func TestPublicDocumentation(t *testing.T) {
	s := api.New()
	for _, path := range []string{"/", "/llms.txt"} {
		w := request(s, "GET", path, "")
		if w.Code != 200 {
			t.Fatalf("public documentation %s: %d", path, w.Code)
		}
		for _, contract := range []string{"/v1/text-to-speech/morse/stream", "/v1/speech-to-text/realtime", "input_audio_chunk", "committed_transcript"} {
			if !strings.Contains(w.Body.String(), contract) {
				t.Errorf("%s missing %s", path, contract)
			}
		}
	}
	plain := request(s, "GET", "/llms.txt", "")
	if plain.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatal("agent documentation must be plain text")
	}
	for i, line := range strings.Split(plain.Body.String(), "\n") {
		if len([]rune(line)) > 80 {
			t.Errorf("documentation line %d exceeds 80 characters", i+1)
		}
	}
	if w := request(s, "GET", "/missing", ""); w.Code != 404 {
		t.Fatal("homepage must not mask unknown routes")
	}
	if w := request(s, "GET", "/v1/models", ""); w.Code != 200 {
		t.Fatal("API discovery must be public too")
	}
}

func TestDocumentationTypography(t *testing.T) {
	s := api.New()
	w := request(s, "GET", "/", "")
	for _, fragment := range []string{"<p>This API produces", "<code class=\"language-bash\">curl", "<ol>", "font-src 'self'"} {
		if !strings.Contains(w.Body.String()+w.Header().Get("Content-Security-Policy"), fragment) {
			t.Errorf("homepage missing %q", fragment)
		}
	}
	font := request(s, "GET", "/assets/inter-variable.woff2", "")
	if font.Code != 200 || font.Header().Get("Content-Type") != "font/woff2" || !strings.HasPrefix(font.Body.String(), "wOF2") {
		t.Fatal("homepage font must be served locally as WOFF2")
	}
}

func TestDocumentationDisallowsProxyRewriting(t *testing.T) {
	w := request(api.New(), "GET", "/", "")
	if w.Header().Get("Cache-Control") != "no-transform" {
		t.Fatal("proxies must not rewrite the file=@sos.pcm code example")
	}
	if !strings.Contains(w.Body.String(), "file=@sos.pcm") {
		t.Fatal("multipart example must retain curl's file-upload syntax")
	}
}
