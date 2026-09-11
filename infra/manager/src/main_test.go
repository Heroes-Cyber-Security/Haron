package main

import (
	"testing"

	"blockchain.hanz.dev/manager/types"
)

func testPea() types.Pea {
	return types.Pea{
		Id:               "pea-123",
		ChallengeHash:    "racestart",
		AccessToken:      "token-abc",
		SetupAddress:     "0xPLAYER",
		PlayerPrivateKey: "deadbeef",
		ChainIds:         []uint64{1},
		Chains: []types.ChainInfo{
			{
				ChainId:      1,
				Name:         "racestart",
				Rpc:          "http://orchestrator:8080/anvil/pea-123/1",
				SetupAddress: "0xSETUP",
			},
		},
	}
}

func TestBuildInstanceResponseIncludesSingleChain(t *testing.T) {
	response := buildInstanceResponse(testPea())

	chains, ok := response["chains"].([]types.ChainInfo)
	if !ok {
		t.Fatalf("expected chains to be []types.ChainInfo, got %T", response["chains"])
	}
	if len(chains) != 1 {
		t.Fatalf("expected 1 chain for a single-chain challenge, got %d", len(chains))
	}
	if got, want := chains[0].Rpc, "http://orchestrator:8080/anvil/pea-123/1"; got != want {
		t.Errorf("chain rpc = %q, want %q", got, want)
	}
	if got, want := chains[0].SetupAddress, "0xSETUP"; got != want {
		t.Errorf("chain setup_address = %q, want %q", got, want)
	}
}

func TestBuildInstanceResponseAlwaysHasCoreFields(t *testing.T) {
	response := buildInstanceResponse(testPea())

	for _, key := range []string{"id", "setup_address", "player_private_key"} {
		if _, ok := response[key]; !ok {
			t.Errorf("response is missing %q", key)
		}
	}

	if got, want := response["id"], "pea-123"; got != want {
		t.Errorf("id = %v, want %v", got, want)
	}
	if got, want := response["setup_address"], "0xPLAYER"; got != want {
		t.Errorf("setup_address = %v, want %v", got, want)
	}
	if got, want := response["player_private_key"], "deadbeef"; got != want {
		t.Errorf("player_private_key = %v, want %v", got, want)
	}
}

func TestBuildInstanceResponseOmitsChainsWhenEmpty(t *testing.T) {
	pea := testPea()
	pea.Chains = nil

	response := buildInstanceResponse(pea)
	if _, ok := response["chains"]; ok {
		t.Error("expected no chains key when the pea has no chains")
	}
}

func TestBuildInstanceResponseKeepsJSONFieldNames(t *testing.T) {
	response := buildInstanceResponse(testPea())

	chains := response["chains"].([]types.ChainInfo)
	chain := chains[0]

	// The webui reads these exact keys out of the JSON payload.
	if chain.ChainId != 1 {
		t.Errorf("chainId = %d, want 1", chain.ChainId)
	}
	if chain.Name != "racestart" {
		t.Errorf("name = %q, want racestart", chain.Name)
	}
	if chain.Rpc == "" {
		t.Error("rpc must not be empty")
	}
	if chain.SetupAddress == "" {
		t.Error("setup_address must not be empty")
	}
}
