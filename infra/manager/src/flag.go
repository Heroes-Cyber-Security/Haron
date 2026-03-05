package main

import (
	"fmt"
	"strings"

	"blockchain.hanz.dev/manager/types"
	"golang.org/x/crypto/sha3"
)

func GenerateFlag(pea types.Pea) string {
	cfg, err := LoadChallengeConfig(pea.ChallengeHash)
	if err != nil {
		return "NO FLAG"
	}

	h256 := sha3.Sum256([]byte(pea.AccessToken))
	h128 := fmt.Sprintf("%x", h256[:16])

	return strings.Replace(cfg.FlagTemplate, "%>HASH<%", h128, -1)
}
