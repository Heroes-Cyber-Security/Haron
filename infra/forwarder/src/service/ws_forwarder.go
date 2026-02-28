package forwarder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSForwarder proxies WebSocket connections between clients and the orchestrator.
type WSForwarder struct {
	base     *url.URL
	logger   *log.Logger
	upgrader websocket.Upgrader
	dialer   *websocket.Dialer
}

// NewWSForwarder creates a WebSocket forwarder targeting the orchestrator base URL.
func NewWSForwarder(orchestratorBase *url.URL, dialer *websocket.Dialer, logger *log.Logger) (*WSForwarder, error) {
	if orchestratorBase == nil {
		return nil, errors.New("orchestrator base URL is required")
	}
	if orchestratorBase.Scheme != "http" && orchestratorBase.Scheme != "https" {
		return nil, errors.New("orchestrator URL must use http or https scheme")
	}

	baseCopy := cloneURL(orchestratorBase)
	if dialer == nil {
		dialer = websocket.DefaultDialer
	}

	return &WSForwarder{
		base:   baseCopy,
		logger: logger,
		upgrader: websocket.Upgrader{
			CheckOrigin:       func(r *http.Request) bool { return true },
			EnableCompression: true,
			HandshakeTimeout:  10 * time.Second,
		},
		dialer: dialer,
	}, nil
}

// ServeHTTP upgrades the connection and bridges traffic to the orchestrator.
func (wsp *WSForwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, chainId, ok := extractEthIDWithChain(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	targetURL := wsp.buildTargetURL(id, chainId)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	upstreamConn, resp, err := wsp.dialer.DialContext(ctx, targetURL.String(), nil)
	if err != nil {
		status := http.StatusBadGateway
		if resp != nil {
			status = resp.StatusCode
		}
		http.Error(w, fmt.Sprintf("failed to connect upstream: %v", err), status)
		return
	}
	defer upstreamConn.Close()

	clientConn, err := wsp.upgrader.Upgrade(w, r, nil)
	if err != nil {
		if wsp.logger != nil {
			wsp.logger.Printf("client upgrade failed: %v", err)
		}
		return
	}
	defer clientConn.Close()

	clientConn.EnableWriteCompression(true)
	upstreamConn.EnableWriteCompression(true)

	var once sync.Once
	shutdown := func(err error) {
		if err != nil && !isExpectedClose(err) && wsp.logger != nil {
			wsp.logger.Printf("websocket bridge closed: %v", err)
		}
		once.Do(func() {
			cancel()
			_ = clientConn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
			_ = upstreamConn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
		})
	}

	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go wsp.pipe(ctx, &wg, clientConn, upstreamConn, errCh, true)
	go wsp.pipe(ctx, &wg, upstreamConn, clientConn, errCh, false)

	select {
	case err := <-errCh:
		shutdown(err)
	case <-ctx.Done():
		shutdown(ctx.Err())
	}

	wg.Wait()
}

func (wsp *WSForwarder) pipe(ctx context.Context, wg *sync.WaitGroup, src, dst *websocket.Conn, errCh chan<- error, filterClientToUpstream bool) {
	defer wg.Done()
	for {
		mt, msg, err := src.ReadMessage()
		if err != nil {
			errCh <- err
			return
		}

		if filterClientToUpstream && mt == websocket.TextMessage {
			if filteredResp := checkWebSocketMessage(msg); filteredResp != nil {
				if err := src.WriteMessage(websocket.TextMessage, filteredResp); err != nil {
					errCh <- err
					return
				}
				continue
			}
		}

		if err := dst.WriteMessage(mt, msg); err != nil {
			errCh <- err
			return
		}
	}
}

func (wsp *WSForwarder) buildTargetURL(id string, chainId string) *url.URL {
	wsURL := cloneURL(wsp.base)
	wsURL.Scheme = toWebSocketScheme(wsURL.Scheme)
	if chainId == "" {
		wsURL.Path = singleJoiningSlash(wsURL.Path, path.Join("anvil", id))
	} else {
		wsURL.Path = singleJoiningSlash(wsURL.Path, path.Join("anvil", id, chainId))
	}
	return wsURL
}

func toWebSocketScheme(httpScheme string) string {
	switch strings.ToLower(httpScheme) {
	case "https":
		return "wss"
	default:
		return "ws"
	}
}

func isExpectedClose(err error) bool {
	if err == nil {
		return true
	}
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return true
	}
	return errors.Is(err, context.Canceled)
}

// checkWebSocketMessage checks if a WebSocket message contains a blocked RPC method
// Returns JSON-RPC error response if blocked, nil if allowed
func checkWebSocketMessage(msg []byte) []byte {
	var singleReq rpcRequest
	if err := json.Unmarshal(msg, &singleReq); err == nil && singleReq.Method != "" {
		if isMethodBlocked(singleReq.Method) {
			return createBlockedWSResponse(singleReq.ID)
		}
		return nil
	}

	var batchReq []rpcRequest
	if err := json.Unmarshal(msg, &batchReq); err == nil && len(batchReq) > 0 {
		var blockedRequests []rpcRequest
		for _, req := range batchReq {
			if isMethodBlocked(req.Method) {
				blockedRequests = append(blockedRequests, req)
			}
		}
		if len(blockedRequests) > 0 {
			var responses []rpcResponse
			for _, req := range blockedRequests {
				var parsedID interface{}
				if err := json.Unmarshal(req.ID, &parsedID); err != nil {
					parsedID = nil
				}
				responses = append(responses, rpcResponse{
					JsonRPC: "2.0",
					ID:      parsedID,
					Error: &rpcError{
						Code:    -32601,
						Message: "Method not allowed",
					},
				})
			}
			respBytes, _ := json.Marshal(responses)
			return respBytes
		}
		return nil
	}

	return nil
}

func createBlockedWSResponse(id json.RawMessage) []byte {
	var parsedID interface{}
	if err := json.Unmarshal(id, &parsedID); err != nil {
		parsedID = nil
	}
	resp := rpcResponse{
		JsonRPC: "2.0",
		ID:      parsedID,
		Error: &rpcError{
			Code:    -32601,
			Message: "Method not allowed",
		},
	}
	respBytes, _ := json.Marshal(resp)
	return respBytes
}
