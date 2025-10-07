package main

import (
	"git.urbach.dev/go/web"
	"github.com/google/uuid"

	"blockchain.hanz.dev/manager/integration"
	"blockchain.hanz.dev/manager/interop"
	"blockchain.hanz.dev/manager/types"
)

var peas = make(map[string]types.Pea)

func main() {
	s := web.NewServer()

	// CORS
	s.Use(func(ctx web.Context) error {
		ctx.Response().SetHeader("Access-Control-Allow-Origin", "*")
		ctx.Response().SetHeader("Access-Control-Allow-Methods", "*")
		ctx.Response().SetHeader("Access-Control-Allow-Headers", "*")

		ctx.Next(ctx)
		return nil
	})

	s.All("/", func(ctx web.Context) error {
		return Jsonify(ctx, map[string]string{"message": "hello from manager"})
	})

	// Interface for `webui`
	s.Post("/stop", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")

		if _, ok := peas[accessToken]; !ok {
			return Jsonify(ctx, map[string]any{"error": "Unauthorized: Instance not found"})
		}

		pea := peas[accessToken]
		interop.Stop(pea)

		delete(peas, accessToken)
		return Jsonify(ctx, map[string]any{"success": true})
	})

	s.Post("/create", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")
		challengeHash := ctx.Request().Header("Challenge")
		playerIP := ctx.RemoteIP()

		if accessToken == "" {
			return Jsonify(ctx, map[string]any{"error": "Unauthorized: Access Token"})
		} else if challengeHash == "" {
			return Jsonify(ctx, map[string]any{"error": "Unauthorized: Challenge Hash"})
		}

		if pea, ok := peas[accessToken]; ok {
			return Jsonify(ctx, map[string]any{"id": pea.Id})
		}

		pea := types.Pea{Id: uuid.NewString(), AccessToken: accessToken}
		peas[accessToken] = pea

		interop.DelegateJob(challengeHash, pea)
		interop.Deploy(pea)

		integration.NotifyPeaCreationTelegram(pea, playerIP)

		return Jsonify(ctx, map[string]any{"id": pea.Id})
	})

	s.Get("/flag", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")
		playerIP := ctx.RemoteIP()
		pea, ok := peas[accessToken]

		if accessToken == "" {
			return Jsonify(ctx, map[string]any{"error": "Unauthorized: Access Token"})
		} else if !ok {
			return Jsonify(ctx, map[string]any{"error": "Unauthorized: Instance does not exists"})
		}

		flag := GenerateFlag(pea)
		integration.NotifyFlagTelegram(pea, playerIP, flag)

		return Jsonify(ctx, map[string]any{"flag": flag})
	})

	// CTFd helpers
	s.Get("/profile", func(ctx web.Context) error {
		player := integration.CTFDGetMe(types.Pea{AccessToken: ctx.Request().Query().Param("id")})

		return Jsonify(ctx, map[string]any{"data": player.ToJSON()})
	})

	s.Run(":8080")
}
