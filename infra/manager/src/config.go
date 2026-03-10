package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

type ChallengeConfig struct {
	FlagTemplate string `yaml:"flag_template"`
}

var configCache = make(map[string]*ChallengeConfig)
var configMutex sync.RWMutex

var challengeHashRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func sanitizeChallengeHash(hash string) (string, error) {
	hash = filepath.Base(hash)
	if strings.Contains(hash, "..") {
		return "", fmt.Errorf("invalid challenge hash: contains directory traversal")
	}
	if !challengeHashRegex.MatchString(hash) {
		return "", fmt.Errorf("invalid challenge hash format")
	}
	return hash, nil
}

func LoadChallengeConfig(challengeHash string) (*ChallengeConfig, error) {
	safeHash, err := sanitizeChallengeHash(challengeHash)
	if err != nil {
		return nil, err
	}
	challengeHash = safeHash
	configMutex.RLock()
	if cfg, exists := configCache[challengeHash]; exists {
		configMutex.RUnlock()
		return cfg, nil
	}
	configMutex.RUnlock()

	configMutex.Lock()
	defer configMutex.Unlock()

	configPath := filepath.Join("challenges", challengeHash, "config.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg ChallengeConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	configCache[challengeHash] = &cfg
	return &cfg, nil
}
