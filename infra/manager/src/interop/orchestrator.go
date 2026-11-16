package interop

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"blockchain.hanz.dev/manager/types"
	"github.com/ddo/rq"
)

var ORCHESTRATOR_BASE = "http://orchestrator:8080"

// DeployQuery renders the orchestrator deploy parameters for a set of chains.
// The three comma separated lists are parallel: entry i of each list describes
// the chain at entry i of `chains`. It returns nil when there is nothing to
// describe, which lets the caller post without a query string.
//
// `challenge` tells the orchestrator which challenge is being deployed so it can
// fetch that challenge's deploy-time hooks (genesis documents and precompiles)
// before the nodes spawn.
func DeployQuery(chains []types.ChainConfig, challenge string) url.Values {
	if len(chains) == 0 {
		return nil
	}

	ids := make([]string, len(chains))
	blockTimes := make([]string, len(chains))
	txOrders := make([]string, len(chains))
	for i, chain := range chains {
		ids[i] = strconv.FormatUint(chain.ChainId, 10)
		blockTimes[i] = strconv.FormatUint(chain.EffectiveBlockTime(), 10)
		txOrders[i] = chain.EffectiveTxOrder()
	}

	params := url.Values{}
	params.Set("chains", strings.Join(ids, ","))
	params.Set("block_times", strings.Join(blockTimes, ","))
	params.Set("tx_orders", strings.Join(txOrders, ","))
	if challenge != "" {
		params.Set("challenge", challenge)
	}
	return params
}

func Deploy(pea types.Pea, chains []types.ChainConfig) error {
	query := ""
	if params := DeployQuery(chains, pea.ChallengeHash); params != nil {
		query = "?" + params.Encode()
	}

	r := rq.Post(ORCHESTRATOR_BASE + "/deploy/" + pea.Id + query)
	req, err := r.ParseRequest()
	if err != nil {
		return fmt.Errorf("failed to parse orchestrator request: %w", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("orchestrator request failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("orchestrator returned status %d", res.StatusCode)
	}

	return nil
}

func Stop(pea types.Pea) {
	r := rq.Post(ORCHESTRATOR_BASE + "/stop/" + pea.Id)
	req, err := r.ParseRequest()
	if err != nil {
		return
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
}
