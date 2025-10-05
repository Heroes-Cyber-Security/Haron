package main

import (
	"encoding/json"

	"git.urbach.dev/go/web"
)

func Jsonify(ctx web.Context, obj any) error {
	ctx.Response().SetHeader("Content-Type", "application/json")

	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	ctx.Response().SetBody(data)

	return nil
}
