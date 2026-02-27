package types

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Challenge struct {
	Hash string
	Name string
}

type ChallengeConfig struct {
	FlagTemplate string        `yaml:"flag_template"`
	Chains       []ChainConfig `yaml:"chains,omitempty"`
}

type ChainConfig struct {
	ChainId uint64 `yaml:"chainId"`
	Name    string `yaml:"name,omitempty"`
}

func LoadChallengeConfig(challengeHash string) (*ChallengeConfig, error) {
	configPath := filepath.Join("challenges", challengeHash, "config.yaml")

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

	return &config, nil
}

func (c *ChallengeConfig) GetChainIds() []uint64 {
	chainIds := make([]uint64, len(c.Chains))
	for i, chain := range c.Chains {
		chainIds[i] = chain.ChainId
	}
	return chainIds
}
