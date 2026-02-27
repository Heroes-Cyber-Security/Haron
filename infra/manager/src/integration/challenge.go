package integration

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func ResolveChallengeName(challengeHash string) string {
	readmePath := filepath.Join("challenges", challengeHash, "README.md")

	file, err := os.Open(readmePath)
	if err != nil {
		return challengeHash
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
		return line
	}

	return challengeHash
}
