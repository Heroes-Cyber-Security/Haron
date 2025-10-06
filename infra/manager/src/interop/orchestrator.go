package interop

import (
	"net/http"

	"blockchain.hanz.dev/manager/types"
	"github.com/ddo/rq"
)

var BASE = "http://orchestrator:8080"

func Deploy(pea types.Pea) {
	r := rq.Post(BASE + "/deploy/" + pea.Id)
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

func Stop(pea types.Pea) {
	r := rq.Post(BASE + "/stop/" + pea.Id)
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
