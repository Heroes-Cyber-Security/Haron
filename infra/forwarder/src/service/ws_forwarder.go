package forwarder

import (
	"context"
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

	go wsp.pipe(ctx, &wg, clientConn, upstreamConn, errCh)
	go wsp.pipe(ctx, &wg, upstreamConn, clientConn, errCh)

	select {
	case err := <-errCh:
		shutdown(err)
	case <-ctx.Done():
		shutdown(ctx.Err())
	}

	wg.Wait()
}

func (wsp *WSForwarder) pipe(ctx context.Context, wg *sync.WaitGroup, src, dst *websocket.Conn, errCh chan<- error) {
	defer wg.Done()
	for {
		mt, msg, err := src.ReadMessage()
		if err != nil {
			errCh <- err
			return
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
