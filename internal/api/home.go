package api

import (
	"bytes"
	_ "embed"
	"html/template"
	"net/http"
	"strings"
)

//go:embed api.txt
var apiReference string

//go:embed home.html
var homeTemplate string

// Render once from the same reference served to agents, with no
// JavaScript, external assets or per-request template work.
var homepage = func() []byte {
	type section struct{ ID, Title, Body string }
	parts := strings.Split(strings.TrimSpace(apiReference), "\n## ")
	data := struct {
		Intro    string
		Sections []section
	}{Intro: strings.TrimSpace(strings.TrimPrefix(parts[0], "# MorseCode Audio API"))}
	for _, part := range parts[1:] {
		title, body, _ := strings.Cut(part, "\n")
		data.Sections = append(data.Sections, section{
			ID:    strings.ReplaceAll(strings.ToLower(title), " ", "-"),
			Title: title, Body: strings.TrimSpace(body),
		})
	}
	var rendered bytes.Buffer
	if err := template.Must(template.New("home").Parse(homeTemplate)).Execute(&rendered, data); err != nil {
		panic(err)
	}
	return rendered.Bytes()
}()

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(homepage)
}

func reference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(apiReference))
}
