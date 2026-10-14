package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type ttsMultiMessage struct {
	ttsSocketMessage
	ContextID    *string `json:"context_id"`
	CloseContext *bool   `json:"close_context"`
	CloseSocket  bool    `json:"close_socket"`
}

func parseTTSMultiMessage(data []byte) (ttsMultiMessage, error) {
	var m ttsMultiMessage
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF {
		return m, errors.New("invalid_request")
	}
	if m.ContextID != nil && (strings.TrimSpace(*m.ContextID) == "" || len(*m.ContextID) > 128) {
		return m, errors.New("invalid_context_id")
	}
	if m.Text == nil && !m.Flush && m.CloseContext == nil && !m.CloseSocket {
		return m, errors.New("invalid_request")
	}
	var err error
	m.ttsSocketMessage, err = validateTTSSocketMessage(m.ttsSocketMessage)
	return m, err
}
func (m ttsMultiMessage) id() string {
	if m.ContextID == nil {
		return "default"
	}
	return *m.ContextID
}
