package main

import (
	"net/http"
	"os"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"blockchain.hanz.dev/manager/integration"
	"blockchain.hanz.dev/manager/interop"
	"blockchain.hanz.dev/manager/types"
)

var peas = make(map[string]types.Pea)

func main() {
	e := echo.New()
	e.HideBanner = true

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions},
		AllowHeaders: []string{"*"},
	}))

	e.Any("/", func(c echo.Context) error {
		return Jsonify(c, map[string]string{"message": "hello from manager"})
	})

	// Interface for `webui`
	e.POST("/stop", func(c echo.Context) error {
		accessToken := c.Request().Header.Get("Token")

		if _, ok := peas[accessToken]; !ok {
			return Jsonify(c, map[string]any{"error": "Unauthorized: Instance not found"})
		}

		pea := peas[accessToken]
		interop.Stop(pea)

		delete(peas, accessToken)
		return Jsonify(c, map[string]any{"success": true})
	})

	e.POST("/create", func(c echo.Context) error {
		accessToken := c.Request().Header.Get("Token")
		challengeHash := c.Request().Header.Get("Challenge")
		playerIP := c.RealIP()

		if accessToken == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized: Access Token"})
		} else if challengeHash == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized: Challenge Hash"})
		}

		if pea, ok := peas[accessToken]; ok {
			// TODO: Be consistent. Id or Hash?
			if pea.ChallengeId != challengeHash {
				return Jsonify(c, map[string]any{"error": "Error: You have existing instance for another challenge"})
			}
			return Jsonify(c, map[string]any{"id": pea.Id})
		}

		pea := types.Pea{
			Id:          uuid.NewString(),
			AccessToken: accessToken,
			ChallengeId: challengeHash,
		}
		peas[accessToken] = pea

		interop.DelegateJob(challengeHash, pea)
		interop.Deploy(pea)

		integration.NotifyPeaCreationTelegram(pea, playerIP)

		return Jsonify(c, map[string]any{"id": pea.Id})
	})

	e.GET("/flag", func(c echo.Context) error {
		accessToken := c.Request().Header.Get("Token")
		playerIP := c.RealIP()
		pea, ok := peas[accessToken]

		if accessToken == "" {
			return Jsonify(c, map[string]any{"error": "Unauthorized: Access Token"})
		} else if !ok {
			return Jsonify(c, map[string]any{"error": "Unauthorized: Instance does not exists"})
		}

		flag := GenerateFlag(pea)
		integration.NotifyFlagTelegram(pea, playerIP, flag)

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
		player := integration.CTFDGetMe(types.Pea{AccessToken: c.QueryParam("id")})

		return Jsonify(c, map[string]any{"data": player.ToJSON()})
	})

	e.Logger.Fatal(e.Start(":8080"))
}
