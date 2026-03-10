package forwarder

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"

	"github.com/gorilla/websocket"
)

// Service wires HTTP and WebSocket forwarding together behind a single handler.
type Service struct {
	httpProxy  *HTTPProxy
	wsProxy    *WSForwarder
	httpServer http.Handler
}

// Config collects runtime configuration for the forwarding service.
type Config struct {
	// OrchestratorURL should point to the orchestrator HTTP endpoint (e.g. http://orchestrator:8080).
	OrchestratorURL string
	// Logger is optional; if nil the service uses the default logger.
	Logger *log.Logger
	// HTTPClient optionally overrides the HTTP client for proxying.
	HTTPClient *http.Client
	// WebSocketDialer optionally overrides the dialer used for upstream WebSocket connections.
	WebSocketDialer *websocket.Dialer
}

// NewService creates a forwarding service using the provided configuration.
func NewService(cfg Config) (*Service, error) {
	if cfg.OrchestratorURL == "" {
		return nil, errors.New("orchestrator URL is required")
	}

	parsed, err := url.Parse(cfg.OrchestratorURL)
	if err != nil {
		return nil, err
	}

	httpProxy, err := NewHTTPProxy(parsed, cfg.HTTPClient, cfg.Logger)
	if err != nil {
		return nil, err
	}

	wsProxy, err := NewWSForwarder(parsed, cfg.WebSocketDialer, cfg.Logger)
	if err != nil {
		return nil, err
	}

	service := &Service{
		httpProxy: httpProxy,
		wsProxy:   wsProxy,
	}
	service.httpServer = http.HandlerFunc(service.serve)

	return service, nil
}

// Handler exposes the combined HTTP/WebSocket handler.
func (s *Service) Handler() http.Handler {
	return s.httpServer
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request) {
	if isWebSocketRequest(r) {
		s.wsProxy.ServeHTTP(w, r)
		return
	}

	s.httpProxy.ServeHTTP(w, r)
}

func isWebSocketRequest(r *http.Request) bool {
	return websocket.IsWebSocketUpgrade(r)
}

// isMethodBlocked returns true if the RPC method should be rejected
func isMethodBlocked(method string) bool {
	blocked := map[string]bool{
		"anvil_setBalance":                 true,
		"anvil_impersonateAccount":         true,
		"anvil_stopImpersonatingAccount":   true,
		"anvil_setStorageAt":               true,
		"anvil_mine":                       true,
		"anvil_setIntervalMining":          true,
		"anvil_setCode":                    true,
		"anvil_setNonce":                   true,
		"anvil_setCoinbase":                true,
		"anvil_removeTx":                   true,
		"anvil_dropTransaction":            true,
		"anvil_reset":                      true,
		"anvil_setLoggingEnabled":          true,
		"anvil_dumpState":                  true,
		"anvil_loadState":                  true,
		"anvil_setChainId":                 true,
		"anvil_setBlockGasLimit":           true,
		"anvil_setBlockTimestamp":          true,
		"anvil_increaseTime":               true,
		"anvil_setNextBlockTimestamp":      true,
		"anvil_enableTraces":               true,
		"anvil_snapshot":                   true,
		"anvil_revert":                     true,
		"anvil_autoImpersonate":            true,
		"evm_increaseTime":                 true,
		"evm_setNextBlockTimestamp":        true,
		"hardhat_setBalance":               true,
		"hardhat_impersonateAccount":       true,
		"hardhat_stopImpersonatingAccount": true,
	}
	return blocked[method]
}

// rpcRequest represents a JSON-RPC 2.0 request
type rpcRequest struct {
	JsonRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	ID      json.RawMessage `json:"id"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse represents a JSON-RPC 2.0 error response
type rpcResponse struct {
	JsonRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Error   *rpcError   `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
