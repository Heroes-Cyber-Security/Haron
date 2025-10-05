package interop

import (
	"io"
	"net/http"
	"net/url"
	"os"

	"blockchain.hanz.dev/manager/types"
	"github.com/ddo/rq"
)

func ChallengeExists(challengeHash string) bool {
	r := rq.Get("http://worker:8080/package/" + challengeHash)
	req, err := r.ParseRequest()
	if err != nil {
		return false
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return false
	}

	data := string(content)
	if data == "false" {
		return false
	}
	return true
}

func UploadChallenge(challengeHash string) {
	filePath := "/home/ctf/challenges/" + challengeHash + ".zip"

	file, err := os.Open(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	r := rq.Post("http://worker:8080/package/" + challengeHash)
	r.SendRaw(file)
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

func DelegateJob(challengeHash string, pea types.Pea) {
	if !ChallengeExists(challengeHash) {
		UploadChallenge(challengeHash)
	}

	r := rq.Post("http://worker:8080/delegate/" + challengeHash + "?anvil_endpoint=" + url.QueryEscape(pea.GetAnvilEndpoint()))
	req, err := r.ParseRequest()
	if err != nil {
		return
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return
	}

	uid := string(content)
	pea.WorkerJobUid = uid
}

func StopJob(pea types.Pea) {
	if pea.WorkerJobUid == "" {
		return
	}

	r := rq.Post("http://worker:8080/stop/" + pea.WorkerJobUid)
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
