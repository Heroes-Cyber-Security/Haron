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

// isMethodBlocked returns true if the RPC method should be rejected.
//
// The set mirrors every state/time/chain-manipulating method exposed by the
// pinned anvil version (crates/anvil/core/src/eth/mod.rs in foundry),
// including every alias the node accepts (anvil_*, hardhat_*, evm_*,
// tenderly_*). Read-only introspection methods such as anvil_getAutomine,
// anvil_nodeInfo and anvil_metadata stay open.
func isMethodBlocked(method string) bool {
	blocked := map[string]bool{
		// account state
		"anvil_setBalance":        true,
		"hardhat_setBalance":      true,
		"tenderly_setBalance":     true,
		"anvil_addBalance":        true,
		"hardhat_addBalance":      true,
		"tenderly_addBalance":     true,
		"anvil_dealERC20":         true,
		"hardhat_dealERC20":       true,
		"anvil_setERC20Balance":   true,
		"anvil_setERC20Allowance": true,
		"anvil_setCode":           true,
		"hardhat_setCode":         true,
		"anvil_setNonce":          true,
		"hardhat_setNonce":        true,
		"evm_setAccountNonce":     true,
		"anvil_setStorageAt":      true,
		"hardhat_setStorageAt":    true,
		"anvil_setCoinbase":       true,
		"hardhat_setCoinbase":     true,
		"anvil_setChainId":        true,
		"anvil_dumpState":         true,
		"hardhat_dumpState":       true,
		"anvil_loadState":         true,
		"hardhat_loadState":       true,
		// impersonation and signing
		"anvil_impersonateAccount":         true,
		"hardhat_impersonateAccount":       true,
		"anvil_stopImpersonatingAccount":   true,
		"hardhat_stopImpersonatingAccount": true,
		"anvil_autoImpersonateAccount":     true,
		"hardhat_autoImpersonateAccount":   true,
		"anvil_impersonateSignature":       true,
		"eth_sendUnsignedTransaction":      true,
		// time and blocks
		"anvil_mine":                         true,
		"hardhat_mine":                       true,
		"evm_mine":                           true,
		"anvil_mine_detailed":                true,
		"evm_mine_detailed":                  true,
		"anvil_setAutomine":                  true,
		"evm_setAutomine":                    true,
		"anvil_setIntervalMining":            true,
		"evm_setIntervalMining":              true,
		"anvil_increaseTime":                 true,
		"evm_increaseTime":                   true,
		"anvil_setNextBlockTimestamp":        true,
		"evm_setNextBlockTimestamp":          true,
		"anvil_setTime":                      true,
		"evm_setTime":                        true,
		"anvil_setBlockGasLimit":             true,
		"evm_setBlockGasLimit":               true,
		"anvil_setBlockTimestampInterval":    true,
		"anvil_removeBlockTimestampInterval": true,
		"anvil_snapshot":                     true,
		"evm_snapshot":                       true,
		"anvil_revert":                       true,
		"evm_revert":                         true,
		"anvil_setLoggingEnabled":            true,
		"hardhat_setLoggingEnabled":          true,
		"anvil_setMinGasPrice":               true,
		"hardhat_setMinGasPrice":             true,
		"anvil_setNextBlockBaseFeePerGas":    true,
		"hardhat_setNextBlockBaseFeePerGas":  true,
		"anvil_enableTraces":                 true,
		// mempool and chain topology
		"anvil_dropTransaction":        true,
		"hardhat_dropTransaction":      true,
		"anvil_dropAllTransactions":    true,
		"hardhat_dropAllTransactions":  true,
		"anvil_removePoolTransactions": true,
		"anvil_reset":                  true,
		"hardhat_reset":                true,
		"anvil_setRpcUrl":              true,
		"anvil_reorg":                  true,
		"anvil_rollback":               true,
		// wallet delegation
		"anvil_addCapability": true,
		"anvil_setExecutor":   true,
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
