package config

import (
	"os"
	"path/filepath"
	"strings"
)

// Internal helper to read and tokenize the .dockerignore file lines
func ParseDockerignore(root string) ([]string, error) {
	var excludes []string
	ignorePath := filepath.Join(root, ".dockerignore")

	data, err := os.ReadFile(ignorePath)
	if os.IsNotExist(err) {
		return excludes, nil
	} else if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		excludes = append(excludes, filepath.Clean(line))
	}
	return excludes, nil
}
