package main

import (
	"git.urbach.dev/go/web"
	"github.com/google/uuid"

	"blockchain.hanz.dev/manager/integration"
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
		if pea, ok := peas[accessToken]; ok {
			return Jsonify(ctx, map[string]any{"id": pea.Id})
		}

		pea := types.Pea{Id: uuid.NewString(), AccessToken: accessToken}
		peas[accessToken] = pea
		// TODO: Notify of Pea creation
		return Jsonify(ctx, map[string]any{"id": pea.Id})
	})

	s.Get("/flag", func(ctx web.Context) error {
		accessToken := ctx.Request().Header("Token")
		playerIP := ctx.Request().Header("PlayerIP")
		pea, ok := peas[accessToken]
		if accessToken == "" || !ok {
			return Jsonify(ctx, map[string]any{"error": "Access token invalid"})
		}

		// TODO: Notify of Flag creation
		flag := GenerateFlag(pea)
		integration.NotifyPeaCreationTelegram(pea, playerIP, flag)

		return Jsonify(ctx, map[string]any{"flag": flag})
	})

	s.Run(":8080")
}
