package interop

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"blockchain.hanz.dev/manager/types"
	"github.com/ddo/rq"
)

var ORCHESTRATOR_BASE = "http://orchestrator:8080"

func Deploy(pea types.Pea) {
	chainIdsStr := ""
	if len(pea.ChainIds) > 0 {
		chainIds := make([]string, len(pea.ChainIds))
		for i, id := range pea.ChainIds {
			chainIds[i] = strconv.FormatUint(id, 10)
		}
		chainIdsStr = "?chains=" + url.QueryEscape(strings.Join(chainIds, ","))
	}

	r := rq.Post(ORCHESTRATOR_BASE + "/deploy/" + pea.Id + chainIdsStr)
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
