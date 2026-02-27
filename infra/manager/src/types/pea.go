package types

type Pea struct {
	Id               string
	ChallengeHash    string
	AccessToken      string
	WorkerJobUid     string
	SetupAddress     string
	PlayerPrivateKey string
}

func (pea Pea) GetAnvilEndpoint() string {
	return "http://orchestrator:8080/anvil/" + pea.Id
}
