package forwarder

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHTTPForwarding(t *testing.T) {
	var receivedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","result":"ok"}`))
	}))
	defer upstream.Close()

	service, err := NewService(Config{OrchestratorURL: upstream.URL})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/eth/123", http.NoBody)
	rec := httptest.NewRecorder()

	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if receivedPath != "/anvil/123" {
		t.Fatalf("expected upstream path /anvil/123, got %s", receivedPath)
	}
}

func TestWebSocketForwardingWithEthSubscribe(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	errCh := make(chan error, 1)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			errCh <- err
			return
		}

		go func() {
			defer conn.Close()
			mt, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			var payload map[string]any
			if err := json.Unmarshal(msg, &payload); err != nil {
				errCh <- err
				return
			}
			if payload["method"] != "eth_subscribe" {
				errCh <- fmt.Errorf("expected eth_subscribe, got %v", payload["method"])
				return
			}

			response := []byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`)
			if err := conn.WriteMessage(mt, response); err != nil {
				errCh <- err
				return
			}
			notification := []byte(`{"jsonrpc":"2.0","method":"eth_subscription","params":{"result":"0xfeed"}}`)
			time.Sleep(50 * time.Millisecond)
			if err := conn.WriteMessage(mt, notification); err != nil {
				errCh <- err
				return
			}
			errCh <- nil
		}()
	}))
	defer upstream.Close()

	service, err := NewService(Config{OrchestratorURL: upstream.URL})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	forwarderServer := httptest.NewServer(service.Handler())
	defer forwarderServer.Close()

	wsURL := "ws" + forwarderServer.URL[len("http"):]
	wsURL += "/eth/123"

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("forwarder dial failed: %v", err)
	}
	defer conn.Close()

	request := []byte(`{"jsonrpc":"2.0","id":1,"method":"eth_subscribe","params":["newHeads"]}`)
	if err := conn.WriteMessage(websocket.TextMessage, request); err != nil {
		t.Fatalf("write request failed: %v", err)
	}

	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read response failed: %v", err)
	}
	if string(msg) != "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":\"0x1\"}" {
		t.Fatalf("unexpected response: %s", string(msg))
	}

	_, notify, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read notification failed: %v", err)
	}
	if string(notify) != "{\"jsonrpc\":\"2.0\",\"method\":\"eth_subscription\",\"params\":{\"result\":\"0xfeed\"}}" {
		t.Fatalf("unexpected notification: %s", string(notify))
	}

	if upstreamErr := <-errCh; upstreamErr != nil {
		t.Fatalf("upstream error: %v", upstreamErr)
	}
}
