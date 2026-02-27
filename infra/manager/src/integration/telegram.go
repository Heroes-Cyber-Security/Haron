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
		return
	}
	chatID := mustGetEnv("TELEGRAM_CHAT_ID")

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

func notifyTelegram(pea types.Pea, playerIP, event, extraInfo string) {
	challengeName := ResolveChallengeName(pea.ChallengeHash)

	playerName := CTFDGetMe(pea).Name
	format := "<code>%s</code> (<code>%s</code>) %s <code>%s</code>\n%s<code>%s</code>"
	extraTag := ""
	if extraInfo != "" {
		extraTag = "\n<code>" + extraInfo + "</code>"
	}
	msg := fmt.Sprintf(format, playerName, playerIP, event, challengeName, extraTag, pea.Id)
	sendMessage(msg)
}

func NotifyFlagTelegram(pea types.Pea, playerIP string, flag string) {
	notifyTelegram(pea, playerIP, "solved", flag)
}

func NotifyPeaCreationTelegram(pea types.Pea, playerIP string) {
	notifyTelegram(pea, playerIP, "instantiated", "")
}
