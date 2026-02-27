package main

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type ChallengeConfig struct {
	FlagTemplate string `yaml:"flag_template"`
}

var configCache = make(map[string]*ChallengeConfig)
var configMutex sync.RWMutex

func LoadChallengeConfig(challengeHash string) (*ChallengeConfig, error) {
	configMutex.RLock()
	if cfg, exists := configCache[challengeHash]; exists {
		configMutex.RUnlock()
		return cfg, nil
	}
	configMutex.RUnlock()

	configMutex.Lock()
	defer configMutex.Unlock()

	configPath := "challenges/" + challengeHash + "/config.yaml"
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
