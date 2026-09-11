package types

import "testing"

func TestNormalizedChainsDefaults(t *testing.T) {
	cfg := &ChallengeConfig{}

	chains := cfg.NormalizedChains()
	if len(chains) != 1 {
		t.Fatalf("expected 1 default chain, got %d", len(chains))
	}
	if got, want := chains[0].ChainId, uint64(1); got != want {
		t.Errorf("default chainId = %d, want %d", got, want)
	}
	if got, want := chains[0].EffectiveBlockTime(), DefaultBlockTime; got != want {
		t.Errorf("default block time = %d, want %d", got, want)
	}
	if got, want := chains[0].EffectiveTxOrder(), DefaultTxOrder; got != want {
		t.Errorf("default tx order = %q, want %q", got, want)
	}
}

func TestNormalizedChainsKeepsExplicitValues(t *testing.T) {
	five := uint64(5)
	cfg := &ChallengeConfig{
		Chains: []ChainConfig{
			{ChainId: 1, Name: "racestart", BlockTime: &five, TxOrder: "fees"},
		},
	}

	chains := cfg.NormalizedChains()
	if len(chains) != 1 {
		t.Fatalf("expected 1 chain, got %d", len(chains))
	}
	if got, want := chains[0].EffectiveBlockTime(), uint64(5); got != want {
		t.Errorf("block time = %d, want %d", got, want)
	}
	if got, want := chains[0].EffectiveTxOrder(), "fees"; got != want {
		t.Errorf("tx order = %q, want %q", got, want)
	}
	if got, want := chains[0].Name, "racestart"; got != want {
		t.Errorf("name = %q, want %q", got, want)
	}
}

func TestNormalizedChainsUnknownTxOrderFallsBack(t *testing.T) {
	cfg := &ChallengeConfig{
		Chains: []ChainConfig{{ChainId: 1, TxOrder: "bogus"}},
	}

	chains := cfg.NormalizedChains()
	if got, want := chains[0].EffectiveTxOrder(), DefaultTxOrder; got != want {
		t.Errorf("unknown tx order = %q, want fallback %q", got, want)
	}
}

func TestNormalizedChainsPreservesExplicitZeroBlockTime(t *testing.T) {
	zero := uint64(0)
	cfg := &ChallengeConfig{
		Chains: []ChainConfig{{ChainId: 1, BlockTime: &zero}},
	}

	chains := cfg.NormalizedChains()
	if got := chains[0].EffectiveBlockTime(); got != 0 {
		t.Errorf("explicit zero block time = %d, want 0 (on-demand mining)", got)
	}
}

func TestNormalizedChainsDoesNotMutateSource(t *testing.T) {
	cfg := &ChallengeConfig{
		Chains: []ChainConfig{{ChainId: 1}},
	}

	_ = cfg.NormalizedChains()

	if cfg.Chains[0].BlockTime != nil {
		t.Error("NormalizedChains mutated the source config's BlockTime")
	}
	if cfg.Chains[0].TxOrder != "" {
		t.Errorf("NormalizedChains mutated the source config's TxOrder to %q", cfg.Chains[0].TxOrder)
	}
}
