package forwarder

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHTTPForwardingWithChainID(t *testing.T) {
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

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/eth/123/1", strings.NewReader(body))
	rec := httptest.NewRecorder()

	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if receivedPath != "/anvil/123/1" {
		t.Fatalf("expected upstream path /anvil/123/1, got %s", receivedPath)
	}
}

func TestAllowedMethodIsProxied(t *testing.T) {
	var receivedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read request body: %v", err)
			return
		}
		receivedBody = string(raw)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
	}))
	defer upstream.Close()

	service, err := NewService(Config{OrchestratorURL: upstream.URL})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"eth_call","params":[]}`
	req := httptest.NewRequest(http.MethodPost, "/eth/123", strings.NewReader(body))
	rec := httptest.NewRecorder()

	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if rec.Body.String() != `{"jsonrpc":"2.0","id":1,"result":"0x1"}` {
		t.Fatalf("expected upstream response to pass through, got %s", rec.Body.String())
	}
	if receivedBody != body {
		t.Fatalf("expected upstream to receive the original body, got %s", receivedBody)
	}
}

func TestBlockedMethodReturnsMethodNotAllowed(t *testing.T) {
	blocked := []string{
		// account state
		"anvil_setBalance", "hardhat_setBalance", "tenderly_setBalance",
		"anvil_addBalance", "hardhat_addBalance", "tenderly_addBalance",
		"anvil_dealERC20", "hardhat_dealERC20", "anvil_setERC20Balance",
		"anvil_setERC20Allowance",
		"anvil_setCode", "hardhat_setCode",
		"anvil_setNonce", "hardhat_setNonce", "evm_setAccountNonce",
		"anvil_setStorageAt", "hardhat_setStorageAt",
		"anvil_setCoinbase", "hardhat_setCoinbase",
		"anvil_setChainId", "anvil_dumpState", "hardhat_dumpState",
		"anvil_loadState", "hardhat_loadState",
		// impersonation and signing
		"anvil_impersonateAccount", "hardhat_impersonateAccount",
		"anvil_stopImpersonatingAccount", "hardhat_stopImpersonatingAccount",
		"anvil_autoImpersonateAccount", "hardhat_autoImpersonateAccount",
		"anvil_impersonateSignature", "eth_sendUnsignedTransaction",
		// time and blocks
		"anvil_mine", "hardhat_mine", "evm_mine",
		"anvil_mine_detailed", "evm_mine_detailed",
		"anvil_setAutomine", "evm_setAutomine",
		"anvil_setIntervalMining", "evm_setIntervalMining",
		"anvil_increaseTime", "evm_increaseTime",
		"anvil_setNextBlockTimestamp", "evm_setNextBlockTimestamp",
		"anvil_setTime", "evm_setTime",
		"anvil_setBlockGasLimit", "evm_setBlockGasLimit",
		"anvil_setBlockTimestampInterval", "anvil_removeBlockTimestampInterval",
		"anvil_snapshot", "evm_snapshot",
		"anvil_revert", "evm_revert",
		"anvil_setLoggingEnabled", "hardhat_setLoggingEnabled",
		"anvil_setMinGasPrice", "hardhat_setMinGasPrice",
		"anvil_setNextBlockBaseFeePerGas", "hardhat_setNextBlockBaseFeePerGas",
		"anvil_enableTraces",
		// mempool and chain topology
		"anvil_dropTransaction", "hardhat_dropTransaction",
		"anvil_dropAllTransactions", "hardhat_dropAllTransactions",
		"anvil_removePoolTransactions",
		"anvil_reset", "hardhat_reset", "anvil_setRpcUrl",
		"anvil_reorg", "anvil_rollback",
		// wallet delegation
		"anvil_addCapability", "anvil_setExecutor",
	}

	service, err := NewService(Config{OrchestratorURL: "http://orchestrator:8080"})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	for _, method := range blocked {
		body := fmt.Sprintf(`{"jsonrpc":"2.0","id":"1","method":%q,"params":[]}`, method)
		req := httptest.NewRequest(http.MethodPost, "/eth/123", strings.NewReader(body))
		rec := httptest.NewRecorder()

		service.Handler().ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("%s: expected status 200, got %d", method, rec.Code)
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Errorf("%s: failed to parse response: %v", method, err)
			continue
		}
		if resp.Error == nil || resp.Error.Code != -32601 {
			t.Errorf("%s: expected method-not-allowed error, got %+v", method, resp.Error)
		}
	}
}

func TestBlockedBatchMethodsReturnErrors(t *testing.T) {
	service, err := NewService(Config{OrchestratorURL: "http://orchestrator:8080"})
	if err != nil {
		t.Fatalf("failed to create service: %v", err)
	}

	body := `[
		{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]},
		{"jsonrpc":"2.0","id":2,"method":"evm_snapshot","params":[]},
		{"jsonrpc":"2.0","id":3,"method":"anvil_reorg","params":{"depth":1}}
	]`
	req := httptest.NewRequest(http.MethodPost, "/eth/123", strings.NewReader(body))
	rec := httptest.NewRecorder()

	service.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var responses []rpcResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &responses); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(responses) != 2 {
		t.Fatalf("expected 2 error responses for the blocked batch entries, got %d", len(responses))
	}
	for _, resp := range responses {
		if resp.Error == nil || resp.Error.Code != -32601 {
			t.Errorf("expected method-not-allowed error, got %+v", resp.Error)
		}
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
