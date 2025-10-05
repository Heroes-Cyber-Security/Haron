package main

import (
	"fmt"
	"os"
	"strings"

	"blockchain.hanz.dev/manager/types"
	"golang.org/x/crypto/sha3"
)

func GenerateFlag(pea types.Pea) string {
	template, exists := os.LookupEnv("FLAG")
	if !exists {
		return "NO FLAG"
	}

	h256 := sha3.Sum256([]byte(pea.AccessToken))
	h64 := fmt.Sprintf("%x", h256[:8])

	return strings.Replace(template, "%>HASH<%", h64, -1)
}
