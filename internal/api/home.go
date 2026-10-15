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

//go:embed assets/inter-variable.woff2
var interFont []byte

type referenceBlock struct {
	Text, Language string
	Code           bool
	Items          []string
}

// The reference uses paragraphs, ordered steps and fenced code blocks.
// Keep this deliberately limited to the documentation's authored format.
func referenceBlocks(text string) []referenceBlock {
	var blocks []referenceBlock
	for _, paragraph := range strings.Split(strings.TrimSpace(text), "\n\n") {
		block := referenceBlock{Text: paragraph}
		if strings.HasPrefix(paragraph, "```") {
			language, code, _ := strings.Cut(paragraph[3:], "\n")
			block = referenceBlock{Code: true, Language: language, Text: strings.TrimSuffix(code, "\n```")}
		} else if strings.HasPrefix(paragraph, "1. ") {
			for _, line := range strings.Split(paragraph, "\n") {
				_, item, _ := strings.Cut(line, ". ")
				block.Items = append(block.Items, item)
			}
		}
		blocks = append(blocks, block)
	}
	return blocks
}

func linkRepository(text string) template.HTML {
	const url = "https://github.com/HashNuke/morsecode-audio"
	// Only the authored repository URL becomes markup; all prose stays escaped.
	return template.HTML(strings.ReplaceAll(template.HTMLEscapeString(text), url,
		`<a href="`+url+`">GitHub HashNuke/morsecode-audio</a>`))
}

// Render once from the same reference served to agents, with no
// JavaScript, external assets or per-request template work.
var homepage = func() []byte {
	type section struct {
		ID, Title string
		Body      []referenceBlock
	}
	parts := strings.Split(strings.TrimSpace(apiReference), "\n## ")
	data := struct {
		Intro    []referenceBlock
		Sections []section
	}{Intro: referenceBlocks(strings.TrimPrefix(parts[0], "# MorseCode Audio API"))}
	for _, part := range parts[1:] {
		title, body, _ := strings.Cut(part, "\n")
		data.Sections = append(data.Sections, section{
			ID:    strings.ReplaceAll(strings.ToLower(title), " ", "-"),
			Title: title, Body: referenceBlocks(body),
		})
	}
	var rendered bytes.Buffer
	if err := template.Must(template.New("home").Funcs(template.FuncMap{"linkRepository": linkRepository}).Parse(homeTemplate)).Execute(&rendered, data); err != nil {
		panic(err)
	}
	return rendered.Bytes()
}()

func home(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Preserve literal curl upload syntax through Cloudflare email obfuscation.
	w.Header().Set("Cache-Control", "no-transform")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(homepage)
}

func font(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(interFont)
}

func reference(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write([]byte(apiReference))
}
