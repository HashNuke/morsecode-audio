package main

import "github.com/railwayapp/railway-go-sdk"

// This repository owns only its speech service, even in a shared Railway project.
const Partial = "morsecode-audio"

func Railway() railway.Project {
	api := railway.ServiceNamed("morsecode-audio", railway.ServiceConfig{
		"source":             railway.Github("HashNuke/morsecode-audio", map[string]any{"branch": "main"}),
		"build":              map[string]any{"builder": "DOCKERFILE", "dockerfilePath": "Dockerfile"},
		"healthcheck":        "/readyz",
		"healthcheckTimeout": 60,
		"replicas":           1,
		"env":                map[string]any{"HOST": "0.0.0.0", "GOMEMLIMIT": "256MiB"},
		"deploy": map[string]any{
			"restartPolicyType":       "ON_FAILURE",
			"restartPolicyMaxRetries": 3,
			"drainingSeconds":         "10",
			"overlapSeconds":          "0",
		},
	})
	return railway.ProjectNamed("morsecode-audio", []any{api})
}
