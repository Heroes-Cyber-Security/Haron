package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"blockchain.hanz.dev/manager/types"
)

func sendMessage(text string) {
	apiKey, exists := os.LookupEnv("TELEGRAM_API_KEY")
	if !exists {
		panic("missing TELEGRAM_API_KEY")
	}

	chatID, exists := os.LookupEnv("TELEGRAM_CHAT_ID")
	if !exists {
		panic("missing TELEGRAM_CHAT_ID")
	}

	payload := map[string]interface{}{
		"chat_id":    chatID,
		"text":       text,
		"parse_mode": "HTML",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return
	}

	url := "https://api.telegram.org/bot" + apiKey + "/sendMessage"
	response, err := http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		// log.Default()
		return
	}

	response.Body.Close()
}

func NotifyFlagTelegram(pea types.Pea, playerIP string, flag string) {
	challengeName, exists := os.LookupEnv("CHALLENGE_NAME")
	if !exists {
		panic("missing CHALLENGE_NAME")
	}

	playerName := CTFDGetMe(pea).Name
	format := "<code>%s</code> (<code>%s</code>) solved <code>%s</code>\n<code>%s</code>\n<code>%s</code>"
	msg := fmt.Sprintf(format, playerName, playerIP, challengeName, flag, pea.Id)
	sendMessage(msg)
}

func NotifyPeaCreationTelegram(pea types.Pea, playerIP string) {
	challengeName, exists := os.LookupEnv("CHALLENGE_NAME")
	if !exists {
		panic("missing CHALLENGE_NAME")
	}

	playerName := CTFDGetMe(pea).Name
	format := "<code>%s</code> (<code>%s</code>) instantiated <code>%s</code>\n<code>%s</code>"
	msg := fmt.Sprintf(format, playerName, playerIP, challengeName, pea.Id)
	sendMessage(msg)
}
