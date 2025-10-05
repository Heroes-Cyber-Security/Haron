package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"

	"blockchain.hanz.dev/manager/types"
)

func CTFDGetMe(pea types.Pea) types.CTFDUser {
	ctfdBase, exists := os.LookupEnv("CTFD_BASE")
	if !exists {
		panic("missing CTFD_BASE")
	}

	url := strings.TrimSuffix(ctfdBase, "/") + "/api/v1/users/me"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return types.CTFDUser{Id: -1, Name: "invalid"}
	}
	req.Header.Set("Authorization", pea.AccessToken)

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return types.CTFDUser{Id: -1, Name: "invalid"}
	}

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return types.CTFDUser{Id: -1, Name: "invalid"}
	}

	intermediary := make(map[string]any)
	err = json.Unmarshal(body, &intermediary)
	if err != nil {
		return types.CTFDUser{Id: -1, Name: "invalid"}
	}
	success, ok := intermediary["success"].(bool)
	if !ok || !success {
		return types.CTFDUser{Id: -1, Name: "invalid"}
	}

	data, ok := intermediary["data"].(map[string]any)

	id, _ := data["id"].(int)
	name, _ := data["name"].(string)

	return types.CTFDUser{Id: id, Name: name}
}
