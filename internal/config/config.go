// Package config validates process settings without loading environment files.
package config

import (
	"errors"
	"net"
	"strconv"
)

type Config struct {
	Host              string
	Port              int
	MaxTTSConcurrency int
	MaxSTTConnections int
}

func Load(getenv func(string) string) (Config, error) {
	host, port := getenv("HOST"), getenv("PORT")
	if host == "" {
		host = "127.0.0.1"
	}
	if port == "" {
		port = "8787"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return Config{}, errors.New("PORT must be an integer between 1 and 65535")
	}
	if net.ParseIP(host) == nil && host != "localhost" {
		return Config{}, errors.New("HOST must be an IP address or localhost")
	}
	tts, err := capacity(getenv("MAX_TTS_CONCURRENCY"), 8)
	if err != nil {
		return Config{}, errors.New("MAX_TTS_CONCURRENCY must be a positive integer")
	}
	stt, err := capacity(getenv("MAX_STT_CONNECTIONS"), 64)
	if err != nil {
		return Config{}, errors.New("MAX_STT_CONNECTIONS must be a positive integer")
	}
	return Config{Host: host, Port: n, MaxTTSConcurrency: tts, MaxSTTConnections: stt}, nil
}

func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

func capacity(value string, fallback int) (int, error) {
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0, errors.New("invalid capacity")
	}
	return n, nil
}
