package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"blockchain.hanz.dev/manager/types"
)

func rctfGetMe(baseURL, accessToken string) types.CTFUser {
	invalid := types.CTFUser{Name: "invalid"}

	url := strings.TrimSuffix(baseURL, "/") + "/api/v2/users/me"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return invalid
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return invalid
	}

	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return invalid
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return invalid
	}

	user, err := parseRCTFUser(body)
	if err != nil {
		return invalid
	}
	return user
}

func parseRCTFUser(body []byte) (types.CTFUser, error) {
	var payload struct {
		Kind string `json:"kind"`
		Data struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return types.CTFUser{}, err
	}

	// rCTF wraps successful responses in a `good*` kind, e.g.
	// goodUserSelfDataV2. Errors use a `bad*` kind.
	if !strings.HasPrefix(payload.Kind, "good") {
		return types.CTFUser{}, errors.New("rctf: unsuccessful response")
	}
	if payload.Data.Id == "" {
		return types.CTFUser{}, errors.New("rctf: missing id")
	}
	if payload.Data.Name == "" {
		return types.CTFUser{}, errors.New("rctf: missing name")
	}

	return types.CTFUser{Id: payload.Data.Id, Name: payload.Data.Name}, nil
}
