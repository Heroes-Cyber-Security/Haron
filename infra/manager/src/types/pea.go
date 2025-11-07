package types

type Pea struct {
	Id           string
	ChallengeId  string
	AccessToken  string
	WorkerJobUid string
}

func (pea Pea) GetAnvilEndpoint() string {
	return "http://orchestrator:8080/anvil/" + pea.Id
}
