package main

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

func Jsonify(ctx echo.Context, obj any) error {
	return ctx.JSON(http.StatusOK, obj)
}
