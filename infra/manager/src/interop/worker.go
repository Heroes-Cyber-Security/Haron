package interop

import (
	"bytes"
	"encoding/json"
	"fmt"
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

type ChainReport struct {
	ChainId      uint64 `json:"chainId"`
	Name         string `json:"name"`
	Rpc          string `json:"rpc"`
	SetupAddress string `json:"setup_address"`
}

type AnvilConfig struct {
	ContractAddress  string        `json:"contract_address"`
	SetupAddress     string        `json:"setup_address"`
	PlayerPrivateKey string        `json:"player_private_key"`
	Chains           []ChainReport `json:"chains,omitempty"`
}

type Report struct {
	AnvilConfig AnvilConfig `json:"anvilconfig"`
}

type DelegateResponse struct {
	Uid    string `json:"uid"`
	Report Report `json:"report"`
}

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

func DelegateJob(challengeHash string, pea *types.Pea) error {
	if !ChallengeExists(challengeHash) {
		UploadChallenge(challengeHash)
	}

	endpointsJSON, _ := json.Marshal(pea.GetAnvilEndpoints())
	queryParams := url.Values{}
	queryParams.Set("anvil_endpoints", string(endpointsJSON))

	r := rq.Post("http://worker:8080/delegate/" + challengeHash + "?" + queryParams.Encode())
	req, err := r.ParseRequest()
	if err != nil {
		return fmt.Errorf("failed to parse request: %w", err)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("worker request failed: %w", err)
	}
	defer res.Body.Close()

	content, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	log.Printf("interop: raw worker response: %s", string(content))

	var resp DelegateResponse
	if err := json.Unmarshal(content, &resp); err != nil {
		return fmt.Errorf("failed to decode response: %w", err)
	}

	pea.WorkerJobUid = resp.Uid
	pea.SetupAddress = resp.Report.AnvilConfig.SetupAddress
	pea.PlayerPrivateKey = resp.Report.AnvilConfig.PlayerPrivateKey

	if len(resp.Report.AnvilConfig.Chains) > 0 {
		pea.Chains = make([]types.ChainInfo, len(resp.Report.AnvilConfig.Chains))
		pea.ChainIds = make([]uint64, len(resp.Report.AnvilConfig.Chains))
		for i, chain := range resp.Report.AnvilConfig.Chains {
			pea.Chains[i] = types.ChainInfo{
				ChainId:      chain.ChainId,
				Name:         chain.Name,
				Rpc:          chain.Rpc,
				SetupAddress: chain.SetupAddress,
			}
			pea.ChainIds[i] = chain.ChainId
		}
		if pea.SetupAddress == "" {
			pea.SetupAddress = pea.Chains[0].SetupAddress
		}
		log.Printf("interop: populated %d chains from worker response", len(pea.Chains))
	} else {
		log.Printf("interop: warning - no chains in worker response, using config chain_ids")
	}

	log.Printf("interop: parsed uid=%s, setup_address=%s", resp.Uid, resp.Report.AnvilConfig.SetupAddress)

	if resp.Report.AnvilConfig.ContractAddress != "" {
		log.Printf("interop: contract deployed at %s", resp.Report.AnvilConfig.ContractAddress)
	}

	if pea.SetupAddress == "" || pea.PlayerPrivateKey == "" {
		return fmt.Errorf("worker returned empty setup_address or player_private_key")
	}

	return nil
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
