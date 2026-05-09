package types

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

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

type Challenge struct {
	Hash string
	Name string
}

type ChallengeConfig struct {
	FlagTemplate   string        `yaml:"flag_template"`
	Chains         []ChainConfig `yaml:"chains,omitempty"`
	TimeoutMinutes int           `yaml:"timeout_minutes,omitempty"`
}

type ChainConfig struct {
	ChainId uint64 `yaml:"chainId"`
	Name    string `yaml:"name,omitempty"`
}

func LoadChallengeConfig(challengeHash string) (*ChallengeConfig, error) {
	safeHash, err := sanitizeChallengeHash(challengeHash)
	if err != nil {
		return nil, err
	}

	configMutex.RLock()
	if cfg, exists := configCache[safeHash]; exists {
		configMutex.RUnlock()
		return cfg, nil
	}
	configMutex.RUnlock()

	configMutex.Lock()
	defer configMutex.Unlock()

	if cfg, exists := configCache[safeHash]; exists {
		return cfg, nil
	}

	configPath := filepath.Join("challenges", safeHash, "config.yaml")

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var config ChallengeConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	if len(config.Chains) == 0 {
		config.Chains = []ChainConfig{{ChainId: 1}}
	}

	configCache[safeHash] = &config
	return &config, nil
}

func (c *ChallengeConfig) GetChainIds() []uint64 {
	chainIds := make([]uint64, len(c.Chains))
	for i, chain := range c.Chains {
		chainIds[i] = chain.ChainId
	}
	return chainIds
}
