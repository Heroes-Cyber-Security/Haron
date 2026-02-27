package types

import "fmt"

type Pea struct {
	Id               string
	ChallengeHash    string
	AccessToken      string
	WorkerJobUid     string
	SetupAddress     string
	PlayerPrivateKey string
	ChainIds         []uint64
}

func (pea Pea) GetAnvilEndpoint() string {
	if len(pea.ChainIds) == 0 {
		return "http://orchestrator:8080/anvil/" + pea.Id
	}
	return "http://orchestrator:8080/anvil/" + pea.Id + "/" + formatChainId(pea.ChainIds[0])
}

func (pea Pea) GetAnvilEndpoints() []string {
	endpoints := make([]string, len(pea.ChainIds))
	for i, chainId := range pea.ChainIds {
		endpoints[i] = "http://orchestrator:8080/anvil/" + pea.Id + "/" + formatChainId(chainId)
	}
	return endpoints
}

func formatChainId(chainId uint64) string {
	return fmt.Sprintf("%d", chainId)
}
