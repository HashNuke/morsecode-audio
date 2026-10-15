package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/HashNuke/morsecode-audio/internal/config"
)

var version = "dev"

const usage = `morsecode-audio — Morse audio API server

Usage:
  morsecode-audio serve [--host IP] [--port PORT]
  morsecode-audio version
  morsecode-audio --help

Serve defaults to 127.0.0.1:8787. HOST and PORT also configure the listener;
command flags take precedence. Running without arguments also starts the server.
`

func parseCommand(args []string, getenv func(string) string, output io.Writer) (config.Config, bool, error) {
	command := "serve"
	if len(args) > 0 {
		command, args = args[0], args[1:]
	}
	switch command {
	case "--help", "-h", "help":
		_, _ = io.WriteString(output, usage)
		return config.Config{}, false, nil
	case "version", "--version":
		if len(args) != 0 {
			return config.Config{}, false, errors.New("version takes no arguments")
		}
		_, _ = fmt.Fprintln(output, "morsecode-audio", version)
		return config.Config{}, false, nil
	case "serve":
		flags := flag.NewFlagSet("serve", flag.ContinueOnError)
		flags.SetOutput(output)
		flags.Usage = func() { _, _ = io.WriteString(output, usage); flags.PrintDefaults() }
		host := flags.String("host", getenv("HOST"), "Listener IP (default 127.0.0.1)")
		port := flags.String("port", getenv("PORT"), "Listener port (default 8787)")
		if err := flags.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return config.Config{}, false, nil
			}
			return config.Config{}, false, err
		}
		if flags.NArg() != 0 {
			return config.Config{}, false, errors.New("unexpected serve arguments")
		}
		cfg, err := config.Load(func(key string) string {
			if key == "HOST" {
				return *host
			}
			if key == "PORT" {
				return *port
			}
			return ""
		})
		return cfg, err == nil, err
	default:
		return config.Config{}, false, fmt.Errorf("unknown command %q; use --help", command)
	}
}
