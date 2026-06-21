package common

import (
	"fmt"
	"os"
	"regexp"
)

var envPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

func ExpandEnv(s string) (string, error) {
	if s == "" {
		return s, nil
	}

	matches := envPattern.FindAllStringSubmatch(s, -1)
	for _, m := range matches {
		if os.Getenv(m[1]) == "" {
			return "", fmt.Errorf("environment variable not found: %s", m[1])
		}
	}

	result := envPattern.ReplaceAllStringFunc(s, func(match string) string {
		varName := envPattern.FindStringSubmatch(match)[1]
		return os.Getenv(varName)
	})

	return result, nil
}
