package interop

import (
	"testing"

	"blockchain.hanz.dev/manager/types"
)

func uint64Ptr(v uint64) *uint64 { return &v }

func TestDeployQueryRendersParallelLists(t *testing.T) {
	chains := []types.ChainConfig{
		{ChainId: 1, Name: "racestart", BlockTime: uint64Ptr(5), TxOrder: "fees"},
	}

	params := DeployQuery(chains, "demo")
	if params == nil {
		t.Fatal("expected query params, got nil")
	}

	if got, want := params.Get("chains"), "1"; got != want {
		t.Errorf("chains = %q, want %q", got, want)
	}
	if got, want := params.Get("block_times"), "5"; got != want {
		t.Errorf("block_times = %q, want %q", got, want)
	}
	if got, want := params.Get("tx_orders"), "fees"; got != want {
		t.Errorf("tx_orders = %q, want %q", got, want)
	}
}

func TestDeployQueryIsParallelAcrossChains(t *testing.T) {
	chains := []types.ChainConfig{
		{ChainId: 1, BlockTime: uint64Ptr(5), TxOrder: "fees"},
		{ChainId: 42, BlockTime: uint64Ptr(2), TxOrder: "fifo"},
	}

	params := DeployQuery(chains, "demo")
	if got, want := params.Get("chains"), "1,42"; got != want {
		t.Errorf("chains = %q, want %q", got, want)
	}
	if got, want := params.Get("block_times"), "5,2"; got != want {
		t.Errorf("block_times = %q, want %q", got, want)
	}
	if got, want := params.Get("tx_orders"), "fees,fifo"; got != want {
		t.Errorf("tx_orders = %q, want %q", got, want)
	}
}

func TestDeployQueryAppliesDefaults(t *testing.T) {
	// No block_time and no tx_order set.
	params := DeployQuery([]types.ChainConfig{{ChainId: 1}}, "demo")
	if got, want := params.Get("block_times"), "1"; got != want {
		t.Errorf("default block_times = %q, want %q", got, want)
	}
	if got, want := params.Get("tx_orders"), "fees"; got != want {
		t.Errorf("default tx_orders = %q, want %q", got, want)
	}
}

func TestDeployQueryFallsBackOnUnknownTxOrder(t *testing.T) {
	params := DeployQuery([]types.ChainConfig{{ChainId: 1, TxOrder: "random"}}, "demo")
	if got, want := params.Get("tx_orders"), "fees"; got != want {
		t.Errorf("unknown tx_order should fall back to %q, got %q", want, got)
	}
}

func TestDeployQueryPreservesExplicitZeroBlockTime(t *testing.T) {
	// An explicit 0 means on-demand mining and must survive normalisation.
	params := DeployQuery([]types.ChainConfig{{ChainId: 1, BlockTime: uint64Ptr(0)}}, "demo")
	if got, want := params.Get("block_times"), "0"; got != want {
		t.Errorf("explicit zero block_time = %q, want %q", got, want)
	}
}

func TestDeployQueryEmptyChains(t *testing.T) {
	if params := DeployQuery(nil, "demo"); params != nil {
		t.Errorf("expected nil params for empty chains, got %v", params)
	}
}

func TestDeployQueryCarriesChallengeForHooks(t *testing.T) {
	params := DeployQuery([]types.ChainConfig{{ChainId: 1}}, "i-can-sell-you-a-bridge")
	if got, want := params.Get("challenge"), "i-can-sell-you-a-bridge"; got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}
}

func TestDeployQueryOmitsEmptyChallenge(t *testing.T) {
	params := DeployQuery([]types.ChainConfig{{ChainId: 1}}, "")
	if params.Has("challenge") {
		t.Errorf("challenge should be omitted when empty, got %q", params.Get("challenge"))
	}
}
