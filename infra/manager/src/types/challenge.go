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
	// BlockTime is the interval mining period in seconds. When omitted it
	// defaults to DefaultBlockTime. An explicit 0 means the node mines on
	// demand instead of on an interval.
	BlockTime *uint64 `yaml:"block_time,omitempty"`
	// TxOrder selects how the node sorts its mempool. When omitted or
	// unrecognised it falls back to DefaultTxOrder.
	TxOrder string `yaml:"tx_order,omitempty"`
}

const (
	// DefaultBlockTime is the interval mining period used when a chain does not
	// specify block_time.
	DefaultBlockTime uint64 = 1
	// DefaultTxOrder is the mempool ordering used when a chain does not specify
	// tx_order or specifies an unknown value.
	DefaultTxOrder = "fees"
)

// supportedTxOrders mirrors the transaction ordering modes anvil understands.
var supportedTxOrders = map[string]bool{
	"fees": true,
	"fifo": true,
}

// EffectiveBlockTime returns the block time to request for this chain.
func (c ChainConfig) EffectiveBlockTime() uint64 {
	if c.BlockTime == nil {
		return DefaultBlockTime
	}
	return *c.BlockTime
}

// EffectiveTxOrder returns the mempool ordering to request for this chain,
// normalising unknown values to DefaultTxOrder.
func (c ChainConfig) EffectiveTxOrder() string {
	if !supportedTxOrders[c.TxOrder] {
		return DefaultTxOrder
	}
	return c.TxOrder
}

// NormalizedChains returns the configured chains with per-chain defaults
// applied. When no chains are configured it returns a single default chain.
func (c *ChallengeConfig) NormalizedChains() []ChainConfig {
	if len(c.Chains) == 0 {
		return []ChainConfig{{ChainId: 1}}
	}

	chains := make([]ChainConfig, len(c.Chains))
	copy(chains, c.Chains)
	for i := range chains {
		blockTime := chains[i].EffectiveBlockTime()
		chains[i].BlockTime = &blockTime
		chains[i].TxOrder = chains[i].EffectiveTxOrder()
	}
	return chains
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

	config.Chains = config.NormalizedChains()

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
