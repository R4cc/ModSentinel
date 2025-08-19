package main

import (
	"errors"
	urlpkg "net/url"
	"strings"
)

func parseModrinthSlug(raw string) (string, error) {
	u, err := urlpkg.Parse(raw)
	if err != nil {
		return "", err
	}
	parts := strings.Split(u.Path, "/")
	for i, p := range parts {
		if p == "mod" || p == "plugin" || p == "datapack" || p == "resourcepack" {
			if i+1 < len(parts) {
				return parts[i+1], nil
			}
		}
	}
	return "", errors.New("slug not found")
}
