package api

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"unicode"

	"github.com/HashNuke/morsecode-audio/internal/morse"
)

type ttsGeneration struct {
	plan                         *morse.StreamEncoder
	source                       *fragmentSource
	pending                      string
	inputBytes                   int
	started                      bool
	mu                           sync.Mutex
	text                         string
	normalized                   []morse.Character
	finished                     bool
	originalSent, normalizedSent int
}

func newTTSGeneration(ctx context.Context, rate int) *ttsGeneration {
	cfg := morse.DefaultConfig()
	cfg.SampleRate = rate
	plan, _ := morse.NewStreamEncoder(cfg) // Rate comes from the validated format enum.
	return &ttsGeneration{plan: plan, source: newFragmentSource(ctx)}
}

func (g *ttsGeneration) buffer(text string) error {
	if len(text) > 4096-g.inputBytes {
		return errors.New("text_too_large")
	}
	g.pending += text
	g.inputBytes += len(text)
	return nil
}

func (g *ttsGeneration) release() error {
	if g.pending == "" {
		return nil
	}
	fragment, err := g.plan.Append(g.pending)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.text += g.pending
	g.normalized = append(g.normalized, fragment.Characters...)
	g.mu.Unlock()
	g.pending = ""
	if fragment.Samples == 0 {
		return nil
	}
	return g.source.offer(fragment)
}

func (g *ttsGeneration) finish() error {
	if err := g.release(); err != nil {
		return err
	}
	fragment, err := g.plan.Finish()
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.finished = true
	g.mu.Unlock()
	if fragment.Samples > 0 {
		if err := g.source.offer(fragment); err != nil {
			return err
		}
	}
	g.source.finish()
	return nil
}

type wsAlignment struct {
	Chars     []string `json:"chars"`
	Starts    []int    `json:"charStartTimesMs"`
	Durations []int    `json:"charDurationsMs"`
}

func relativeWSAlignment(chars []morse.Character, offset float64) *wsAlignment {
	if len(chars) == 0 {
		return nil
	}
	a := &wsAlignment{Chars: make([]string, 0, len(chars)), Starts: make([]int, 0, len(chars)), Durations: make([]int, 0, len(chars))}
	for _, char := range chars {
		a.Chars = append(a.Chars, char.Text)
		a.Starts = append(a.Starts, int(math.Round((char.Start-offset)*1000)))
		a.Durations = append(a.Durations, int(math.Round(char.End*1000))-int(math.Round(char.Start*1000)))
	}
	return a
}

// The output worker alone advances cursors. Metadata is published before input
// readers become visible to the codec. Unresolved trailing whitespace waits for
// the next word or finalization, so its timing never needs a later correction.
func (g *ttsGeneration) takeAlignment(through, offset float64, final bool) (*wsAlignment, *wsAlignment, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	end := g.normalizedSent
	for end < len(g.normalized) && g.normalized[end].End <= through+1e-9 {
		end++
	}
	if end == g.normalizedSent && !final {
		return nil, nil, false
	}
	text := g.text
	if !g.finished {
		text = strings.TrimRightFunc(text, unicode.IsSpace)
	}
	original := originalCharacters(text, g.normalized)
	originalEnd := g.originalSent
	for originalEnd < len(original) && original[originalEnd].End <= through+1e-9 {
		originalEnd++
	}
	a := relativeWSAlignment(original[g.originalSent:originalEnd], offset)
	n := relativeWSAlignment(g.normalized[g.normalizedSent:end], offset)
	g.originalSent = originalEnd
	g.normalizedSent = end
	return a, n, true
}
