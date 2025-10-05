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

	s.All("/", func(ctx web.Context) error {
		return Jsonify(ctx, map[string]string{"message": "hello from manager"})
	})

	// Interface for `webui`
	s.Post("/stop", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")
		if _, ok := peas[accessToken]; !ok {
			return Jsonify(ctx, map[string]any{"error": "Instance not found"})
		}

		delete(peas, accessToken)
		return Jsonify(ctx, map[string]any{"success": true})
	})

	s.Post("/create", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")
		challengeHash := ctx.Request().Header("Challenge")
		playerIP := ctx.Request().RemoteIP()

		if pea, ok := peas[accessToken]; ok {
			return Jsonify(ctx, map[string]any{"id": pea.Id})
		}

		pea := types.Pea{Id: uuid.NewString(), AccessToken: accessToken}
		peas[accessToken] = pea

		interop.DelegateJob(challengeHash, pea)

		integration.NotifyPeaCreationTelegram(pea, playerIP)

		return Jsonify(ctx, map[string]any{"id": pea.Id})
	})

	s.Get("/flag", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")
		playerIP := ctx.Request().RemoteIP()
		pea, ok := peas[accessToken]
		if accessToken == "" || !ok {
			return Jsonify(ctx, map[string]any{"error": "Access token invalid"})
		}

		flag := GenerateFlag(pea)
		integration.NotifyFlagTelegram(pea, playerIP, flag)

		return Jsonify(ctx, map[string]any{"flag": flag})
	})

	s.Run(":8080")
}
