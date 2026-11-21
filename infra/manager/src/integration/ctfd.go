package integration

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"blockchain.hanz.dev/manager/types"
)

func ctfdGetMe(baseURL, accessToken string) types.CTFUser {
	invalid := types.CTFUser{Name: "invalid"}

	url := strings.TrimSuffix(baseURL, "/") + "/api/v1/users/me"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return invalid
	}
	req.Header.Set("Authorization", "Token "+accessToken)
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

	user, err := parseCTFDUser(body)
	if err != nil {
		return invalid
	}
	return user
}

func parseCTFDUser(body []byte) (types.CTFUser, error) {
	intermediary := make(map[string]any)
	if err := json.Unmarshal(body, &intermediary); err != nil {
		return types.CTFUser{}, err
	}

	success, ok := intermediary["success"].(bool)
	if !ok || !success {
		return types.CTFUser{}, errors.New("ctfd: unsuccessful response")
	}

	data, ok := intermediary["data"].(map[string]any)
	if !ok {
		return types.CTFUser{}, errors.New("ctfd: missing data")
	}

	var id string
	switch value := data["id"].(type) {
	case float64:
		id = strconv.Itoa(int(value))
	case string:
		id = value
	default:
		return types.CTFUser{}, errors.New("ctfd: missing id")
	}

	name, ok := data["name"].(string)
	if !ok || name == "" {
		return types.CTFUser{}, errors.New("ctfd: missing name")
	}

	return types.CTFUser{Id: id, Name: name}, nil
}
