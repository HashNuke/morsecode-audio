package api

import (
	"unicode"

	"github.com/HashNuke/morsecode-audio/internal/morse"
)

type characterAlignment struct {
	Characters []string  `json:"characters"`
	Starts     []float64 `json:"character_start_times_seconds"`
	Ends       []float64 `json:"character_end_times_seconds"`
}

func alignment(chars []morse.Character) *characterAlignment {
	if len(chars) == 0 {
		return nil
	}
	a := &characterAlignment{Characters: make([]string, 0, len(chars)), Starts: make([]float64, 0, len(chars)), Ends: make([]float64, 0, len(chars))}
	for _, char := range chars {
		a.Characters = append(a.Characters, char.Text)
		a.Starts = append(a.Starts, char.Start)
		a.Ends = append(a.Ends, char.End)
	}
	return a
}

// originalCharacters maps already validated text to the normalized signal.
// Collapsed interior whitespace shares the word gap; trimmed whitespace has
// zero duration at the adjacent signal boundary. Original case is preserved.
func originalCharacters(text string, normalized []morse.Character) []morse.Character {
	runes := []rune(text)
	result := make([]morse.Character, 0, len(runes))
	ni := 0
	for i := 0; i < len(runes); {
		if !unicode.IsSpace(runes[i]) {
			char := normalized[ni]
			char.Text = string(runes[i])
			result = append(result, char)
			ni++
			i++
			continue
		}
		end := i + 1
		for end < len(runes) && unicode.IsSpace(runes[end]) {
			end++
		}
		startTime, endTime := 0.0, 0.0
		if ni > 0 {
			startTime = normalized[ni-1].End
			endTime = startTime
			if ni < len(normalized) && normalized[ni].Text == " " {
				startTime = normalized[ni].Start
				endTime = normalized[ni].End
				ni++
			}
		}
		for j := i; j < end; j++ {
			start := startTime + (endTime-startTime)*float64(j-i)/float64(end-i)
			stop := startTime + (endTime-startTime)*float64(j-i+1)/float64(end-i)
			result = append(result, morse.Character{Text: string(runes[j]), Start: start, End: stop})
		}
		i = end
	}
	return result
}

func takeAlignment(chars *[]morse.Character, through float64) *characterAlignment {
	n := 0
	for n < len(*chars) && (*chars)[n].End <= through+1e-9 {
		n++
	}
	result := alignment((*chars)[:n])
	*chars = (*chars)[n:]
	return result
}
