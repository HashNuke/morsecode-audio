package api

import "net/http"

func (s *Server) catalogRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/text-to-speech/{voice_id}", s.synthesize)
	mux.HandleFunc("POST /v1/text-to-speech/{voice_id}/stream", s.synthesize)
	mux.HandleFunc("POST /v1/speech-to-text", s.transcribe)
	mux.HandleFunc("GET /v1/speech-to-text/realtime", s.realtimeSTT)
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, models()) })
	mux.HandleFunc("GET /v1/voices", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"voices": []any{voice()}})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, 404, "not_found", "Unknown API route") })
	return mux
}

func knownVoice(w http.ResponseWriter, r *http.Request) bool {
	if r.PathValue("voice_id") == "morse" {
		return true
	}
	writeError(w, 404, "voice_not_found", "Unknown Morse voice")
	return false
}

func voice() map[string]any {
	return map[string]any{
		"voice_id": "morse", "name": "Morse 700 Hz", "category": "generated",
		"description": "International Morse tones at 700 Hz with a 60 ms dot. Not human speech.",
		"labels":      map[string]string{"type": "morse"}, "available_for_tiers": []string{},
		"high_quality_base_model_ids": []string{"morse_v1"},
	}
}

func models() []any {
	return []any{map[string]any{
		"model_id": "morse_v1", "name": "MorseCodeApiTTS", "can_be_finetuned": false,
		"can_do_text_to_speech": true, "can_do_voice_conversion": false,
		"can_use_style": false, "can_use_speaker_boost": false, "serves_pro_voices": false,
		"token_cost_factor": 0, "description": "Deterministic Morse tone synthesis; no upstream billing.",
		"requires_alpha_access": false, "max_characters_request_free_user": 4096,
		"max_characters_request_subscribed_user": 4096, "maximum_text_length_per_request": 4096,
		"languages": []any{}, "model_rates": map[string]any{"character_cost_multiplier": 0, "cost_discount_multiplier": 1},
		"concurrency_group": "morse",
	}}
}
