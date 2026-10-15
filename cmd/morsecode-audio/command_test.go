package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestServeCommand(t *testing.T) {
	for _, tc := range []struct {
		args             []string
		host, port, want string
	}{
		{[]string{"serve"}, "", "", "127.0.0.1:8787"},
		{nil, "0.0.0.0", "9000", "0.0.0.0:9000"},
		{[]string{"serve", "--host", "::1", "--port", "9001"}, "0.0.0.0", "9000", "[::1]:9001"},
	} {
		cfg, serve, err := parseCommand(tc.args, func(key string) string {
			if key == "HOST" {
				return tc.host
			}
			if key == "PORT" {
				return tc.port
			}
			return ""
		}, new(bytes.Buffer))
		if err != nil || !serve || cfg.Address() != tc.want {
			t.Fatalf("%v: %+v %t %v", tc.args, cfg, serve, err)
		}
	}
}

func TestCommandHelpAndVersionDoNotStartServer(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"serve", "--help"}, {"version"}, {"--version"}} {
		var output bytes.Buffer
		_, serve, err := parseCommand(args, func(string) string { return "bad environment" }, &output)
		if err != nil || serve || !strings.Contains(output.String(), "morsecode-audio") {
			t.Fatalf("%v: %t %v %q", args, serve, err, output.String())
		}
	}
}

func TestCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"unknown"}, {"serve", "extra"}, {"version", "extra"}, {"serve", "--port", "0"}, {"serve", "--host", "bad host"}} {
		if _, _, err := parseCommand(args, func(string) string { return "" }, new(bytes.Buffer)); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
