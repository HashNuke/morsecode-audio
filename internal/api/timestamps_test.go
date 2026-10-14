package api_test

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/HashNuke/morsecode-audio/internal/api"
)

type alignmentResult struct {
	Characters []string  `json:"characters"`
	Starts     []float64 `json:"character_start_times_seconds"`
	Ends       []float64 `json:"character_end_times_seconds"`
}
type timedResult struct {
	Audio      string           `json:"audio_base64"`
	Alignment  *alignmentResult `json:"alignment"`
	Normalized *alignmentResult `json:"normalized_alignment"`
}

func TestHTTPTimestampVariants(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		for _, format := range []string{"pcm_16000", "mp3_44100_128", "mp3_22050_32", "opus_48000_64", "ulaw_8000", "alaw_8000"} {
			t.Run(format+map[bool]string{true: "/stream"}[streaming], func(t *testing.T) {
				path := "/v1/text-to-speech/morse"
				if streaming {
					path += "/stream"
				}
				r := httptest.NewRequest("POST", path+"/with-timestamps?output_format="+format, strings.NewReader(`{"text":" e  t "}`))
				r.Header.Set("xi-api-key", "synthetic")
				w := httptest.NewRecorder()
				api.New().ServeHTTP(w, r)
				if w.Code != 200 {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
				if w.Header().Get("Content-Type") != "application/json" {
					t.Fatal(w.Header())
				}
				var audio bytes.Buffer
				original, normalized := alignmentResult{}, alignmentResult{}
				appendAlignment := func(out *alignmentResult, in *alignmentResult) {
					if in == nil {
						return
					}
					if len(in.Characters) != len(in.Starts) || len(in.Characters) != len(in.Ends) {
						t.Fatal("parallel arrays differ")
					}
					out.Characters = append(out.Characters, in.Characters...)
					out.Starts = append(out.Starts, in.Starts...)
					out.Ends = append(out.Ends, in.Ends...)
				}
				scanner := bufio.NewScanner(w.Body)
				scanner.Buffer(make([]byte, 4096), 1<<20)
				chunks := 0
				for scanner.Scan() {
					var chunk timedResult
					if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
						t.Fatal(err)
					}
					data, err := base64.StdEncoding.DecodeString(chunk.Audio)
					if err != nil {
						t.Fatal(err)
					}
					audio.Write(data)
					appendAlignment(&original, chunk.Alignment)
					appendAlignment(&normalized, chunk.Normalized)
					chunks++
				}
				if err := scanner.Err(); err != nil {
					t.Fatal(err)
				}
				if streaming && (chunks < 2 || !w.Flushed) {
					t.Fatal("no incremental JSON frames")
				}
				if !streaming && chunks != 1 {
					t.Fatal("nonstreaming response is not one JSON object")
				}
				if strings.Join(original.Characters, "") != " e  t " || strings.Join(normalized.Characters, "") != "E T" {
					t.Fatalf("alignment %+v / %+v", original, normalized)
				}
				for i, span := range [][2]float64{{0, .06}, {.06, .48}, {.48, .66}} {
					if math.Abs(normalized.Starts[i]-span[0]) > .00001 || math.Abs(normalized.Ends[i]-span[1]) > .00001 {
						t.Fatalf("timing %+v", normalized)
					}
				}
				if audio.Len() == 0 {
					t.Fatal("missing audio")
				}
				if format == "pcm_16000" && audio.Len() != 48000 {
					t.Fatalf("PCM bytes %d", audio.Len())
				}
			})
		}
	}
}

func TestTimestampedStreamMatchesBinaryAudio(t *testing.T) {
	for _, format := range []string{"pcm_16000", "mp3_44100_128", "opus_48000_64"} {
		var results [][]byte
		for _, suffix := range []string{"", "/with-timestamps"} {
			r := httptest.NewRequest("POST", "/v1/text-to-speech/morse/stream"+suffix+"?output_format="+format, strings.NewReader(`{"text":"ET"}`))
			r.Header.Set("xi-api-key", "test")
			w := httptest.NewRecorder()
			api.New().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if suffix == "" {
				results = append(results, w.Body.Bytes())
				continue
			}
			var data []byte
			d := json.NewDecoder(w.Body)
			for {
				var chunk timedResult
				if err := d.Decode(&chunk); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				b, err := base64.StdEncoding.DecodeString(chunk.Audio)
				if err != nil {
					t.Fatal(err)
				}
				data = append(data, b...)
			}
			results = append(results, data)
		}
		// Ogg serials are randomized per independent encoding; encoded length is stable.
		if format == "opus_48000_64" {
			if len(results[0]) != len(results[1]) {
				t.Fatal("lost Ogg bytes")
			}
		} else if !bytes.Equal(results[0], results[1]) {
			t.Fatalf("lost %s stream bytes", format)
		}
	}
}
