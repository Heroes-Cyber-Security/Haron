package interop

import (
	"bytes"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

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
	filePath := "/home/ctf/challenges/" + challengeHash + "/" + challengeHash + ".zip"

	if _, err := os.Stat(filePath); err != nil {
		log.Printf("interop: challenge package missing at %s: %v", filePath, err)
		return
	}

	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("interop: unable to open challenge package: %v", err)
		return
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		log.Printf("interop: unable to create multipart form: %v", err)
		return
	}

	if _, err := io.Copy(part, file); err != nil {
		log.Printf("interop: unable to copy challenge package into multipart: %v", err)
		return
	}

	if err := writer.Close(); err != nil {
		log.Printf("interop: unable to close multipart writer: %v", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, "http://worker:8080/package/"+challengeHash, body)
	if err != nil {
		log.Printf("interop: unable to create upload request: %v", err)
		return
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("interop: upload request failed: %v", err)
		return
	}
	defer res.Body.Close()

	if res.StatusCode >= 400 {
		body, _ := io.ReadAll(res.Body)
		log.Printf("interop: upload failed with status %d: %s", res.StatusCode, string(body))
	}
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
