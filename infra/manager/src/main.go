package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"blockchain.hanz.dev/manager/integration"
	"blockchain.hanz.dev/manager/interop"
	"blockchain.hanz.dev/manager/types"
)

var rpcPublicHost = os.Getenv("RPC_PUBLIC_HOST")

var peas = make(map[string]types.Pea)
var peasMu sync.RWMutex
var timeoutManager = NewTimeoutManager()

func convertRpcUrls(chains []types.ChainInfo) []types.ChainInfo {
	result := make([]types.ChainInfo, len(chains))
	for i, chain := range chains {
		result[i] = chain
		// Replace internal orchestrator URL with public webui host
		if rpcPublicHost != "" {
			result[i].Rpc = strings.Replace(chain.Rpc, "orchestrator:8080", rpcPublicHost, 1)
		}
	}
	return result
}

func main() {
	e := echo.New()
	e.HideBanner = true

	e.Any("/", func(c echo.Context) error {
		return Jsonify(c, map[string]string{"message": "hello from manager"})
	})

	// Interface for `webui`
	e.POST("/stop", func(c echo.Context) error {
		accessToken := c.Request().Header.Get("Token")

		if accessToken == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		player := integration.CTFDGetMe(types.Pea{AccessToken: accessToken})
		if !player.IsValid() {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		peasMu.RLock()
		_, ok := peas[accessToken]
		pea := peas[accessToken]
		peasMu.RUnlock()

		if !ok {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		interop.Stop(pea)
		interop.StopJob(pea)

		timeoutManager.Cancel(accessToken)

		peasMu.Lock()
		delete(peas, accessToken)
		peasMu.Unlock()
		return Jsonify(c, map[string]any{"success": true})
	})

	e.POST("/create", func(c echo.Context) error {
		accessToken := c.Request().Header.Get("Token")
		challengeHash := c.Request().Header.Get("Challenge")
		playerIP := c.RealIP()

		if accessToken == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		} else if challengeHash == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		player := integration.CTFDGetMe(types.Pea{AccessToken: accessToken})
		if !player.IsValid() {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		peasMu.RLock()
		if pea, ok := peas[accessToken]; ok {
			peasMu.RUnlock()
			if pea.ChallengeHash != challengeHash {
				return Jsonify(c, map[string]any{"error": "Error: You have existing instance for another challenge"})
			}
			response := map[string]any{
				"id":                 pea.Id,
				"setup_address":      pea.SetupAddress,
				"player_private_key": pea.PlayerPrivateKey,
			}
			if len(pea.Chains) > 1 {
				response["chains"] = convertRpcUrls(pea.Chains)
			}
			return Jsonify(c, response)
		}
		peasMu.RUnlock()

		config, err := types.LoadChallengeConfig(challengeHash)
		if err != nil {
			log.Printf("main: failed to load challenge config: %v", err)
			return Jsonify(c, map[string]any{"error": "Failed to load challenge config"})
		}

		pea := types.Pea{
			Id:            uuid.NewString(),
			AccessToken:   accessToken,
			ChallengeHash: challengeHash,
			ChainIds:      config.GetChainIds(),
		}

		if err := interop.Deploy(pea); err != nil {
			log.Printf("main: Deploy failed: %v", err)
			return Jsonify(c, map[string]any{"error": "Failed to deploy instance"})
		}

		if err := interop.DelegateJob(challengeHash, &pea); err != nil {
			log.Printf("main: DelegateJob failed: %v", err)
			return Jsonify(c, map[string]any{"error": err.Error()})
		}

		peasMu.Lock()
		peas[accessToken] = pea
		peasMu.Unlock()

		timeoutManager.Register(accessToken, config.TimeoutMinutes)

		integration.NotifyPeaCreationTelegram(pea, playerIP)

		response := map[string]any{
			"id":                 pea.Id,
			"setup_address":      pea.SetupAddress,
			"player_private_key": pea.PlayerPrivateKey,
		}

		if len(pea.Chains) > 1 {
			response["chains"] = convertRpcUrls(pea.Chains)
		}

		return Jsonify(c, response)
	})

	e.GET("/flag", func(c echo.Context) error {
		accessToken := c.Request().Header.Get("Token")
		playerIP := c.RealIP()

		if accessToken == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		player := integration.CTFDGetMe(types.Pea{AccessToken: accessToken})
		if !player.IsValid() {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		peasMu.RLock()
		pea, ok := peas[accessToken]
		peasMu.RUnlock()

		if !ok {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		res, err := http.Get("http://worker:8080/validate/" + pea.WorkerJobUid)
		if err != nil {
			return Jsonify(c, map[string]any{"error": "Validation service unavailable"})
		}
		defer res.Body.Close()

		body, err := io.ReadAll(res.Body)
		if err != nil {
			return Jsonify(c, map[string]any{"error": "Validation read failed"})
		}

		var validation struct {
			Solved bool   `json:"solved"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal(body, &validation); err != nil {
			return Jsonify(c, map[string]any{"error": "Validation parse failed"})
		}

		if !validation.Solved {
			errMsg := "Challenge not solved"
			if validation.Error != "" {
				errMsg = validation.Error
			}
			return Jsonify(c, map[string]any{"error": errMsg})
		}

		flag := GenerateFlag(pea)
		integration.NotifyFlagTelegram(pea, playerIP, flag)

		timeoutManager.Cancel(accessToken)

		return Jsonify(c, map[string]any{"flag": flag})
	})

	e.GET("/challenges", func(c echo.Context) error {
		files, err := os.ReadDir("challenges")
		if err != nil {
			return Jsonify(c, map[string]any{"error": "FS: Error reading challenges"})
		}

		challenges := make([]string, 0, len(files))
		readmes := make([]string, 0, len(files))
		for i := range files {
			f := files[i]
			if f.IsDir() {
				dirname := f.Name()
				challenges = append(challenges, dirname)

				content, err := os.ReadFile("challenges/" + dirname + "/README.md")
				if err != nil {
					readmes = append(readmes, "")
				} else {
					readmes = append(readmes, string(content))
				}
			}
		}

		return Jsonify(c, map[string]any{"challenges": challenges, "readmes": readmes})
	})

	// CTFd helpers
	e.GET("/profile", func(c echo.Context) error {
		player := integration.CTFDGetMe(types.Pea{AccessToken: c.QueryParam("accessToken")})

		if !player.IsValid() {
			return Jsonify(c, map[string]any{"error": "Unauthorized"})
		}

		return Jsonify(c, map[string]any{"data": player.ToJSON()})
	})

	e.Logger.Fatal(e.Start(":8080"))
}
