package main

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"time"
)

type config struct {
	NavidromeURL      string
	NavidromeUser     string
	NavidromePassword string
	NavidromeRoot     string
	AurralURL         string
	AurralUser        string
	AurralGroups      string
	AurralRoot        string
	Interval          time.Duration
}

func loadConfig(getenv func(string) string) (config, error) {
	c := config{
		NavidromeURL:      getenv("NAVIDROME_URL"),
		NavidromeUser:     getenv("NAVIDROME_USER"),
		NavidromePassword: getenv("NAVIDROME_PASSWORD"),
		NavidromeRoot:     path.Clean(cmp.Or(getenv("NAVIDROME_LIBRARY_ROOT"), "/aurral")),
		AurralURL:         cmp.Or(getenv("AURRAL_URL"), "http://127.0.0.1:3001"),
		AurralUser:        cmp.Or(getenv("AURRAL_USER"), getenv("NAVIDROME_USER")),
		AurralGroups:      cmp.Or(getenv("AURRAL_GROUPS"), "administrators"),
		AurralRoot:        path.Clean(cmp.Or(getenv("AURRAL_ROOT"), "/data/aurral")),
	}
	var errs []error
	for _, name := range []string{"NAVIDROME_URL", "NAVIDROME_USER", "NAVIDROME_PASSWORD"} {
		if getenv(name) == "" {
			errs = append(errs, fmt.Errorf("%s is required", name))
		}
	}
	interval, err := time.ParseDuration(cmp.Or(getenv("INTERVAL"), "15m"))
	switch {
	case err != nil:
		errs = append(errs, fmt.Errorf("INTERVAL: %w", err))
	case interval <= 0:
		errs = append(errs, errors.New("INTERVAL must be positive"))
	}
	c.Interval = interval
	return c, errors.Join(errs...)
}
