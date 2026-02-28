package forwarder

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// HTTPProxy forwards JSON-RPC HTTP traffic from /eth/{id} to the orchestrator.
type HTTPProxy struct {
	base   *url.URL
	client *http.Client
	logger *log.Logger
}

// NewHTTPProxy creates a new HTTP proxy targeting the orchestrator base URL.
func NewHTTPProxy(orchestratorBase *url.URL, client *http.Client, logger *log.Logger) (*HTTPProxy, error) {
	if orchestratorBase == nil {
		return nil, errors.New("orchestrator base URL is required")
	}
	if orchestratorBase.Scheme != "http" && orchestratorBase.Scheme != "https" {
		return nil, errors.New("orchestrator HTTP URL must use http or https scheme")
	}

	base := cloneURL(orchestratorBase)
	if client == nil {
		client = http.DefaultClient
	}

	return &HTTPProxy{
		base:   base,
		client: client,
		logger: logger,
	}, nil
}

// ServeHTTP implements http.Handler and proxies the request to the orchestrator.
func (p *HTTPProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, chainId, ok := extractEthIDWithChain(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	var targetPath string
	if chainId == "" {
		targetPath = singleJoiningSlash(p.base.Path, path.Join("anvil", id))
	} else {
		targetPath = singleJoiningSlash(p.base.Path, path.Join("anvil", id, chainId))
	}
	query := r.URL.RawQuery

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		if p.logger != nil {
			p.logger.Printf("failed to read request body: %v", err)
		}
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	if blockedResp := checkRPCMethods(bodyBytes); blockedResp != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(blockedResp)
		return
	}

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = p.base.Scheme
			req.URL.Host = p.base.Host
			req.Host = p.base.Host
			req.URL.Path = targetPath
			req.URL.RawPath = targetPath
			req.URL.RawQuery = query
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			req.ContentLength = int64(len(bodyBytes))
			if _, ok := req.Header["User-Agent"]; !ok {
				req.Header.Set("User-Agent", "")
			}
		},
		Transport: p.client.Transport,
		ErrorLog:  p.logger,
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			if p.logger != nil {
				p.logger.Printf("http proxy error: %v", err)
			}
			http.Error(rw, "upstream error", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

func extractEthID(pathValue string) (string, bool) {
	if !strings.HasPrefix(pathValue, "/eth/") {
		return "", false
	}

	candidate := strings.TrimPrefix(pathValue, "/eth/")
	if candidate == "" {
		return "", false
	}

	slash := strings.Index(candidate, "/")
	if slash >= 0 {
		candidate = candidate[:slash]
	}

	return candidate, candidate != ""
}

func extractEthIDWithChain(pathValue string) (string, string, bool) {
	if !strings.HasPrefix(pathValue, "/eth/") {
		return "", "", false
	}

	candidate := strings.TrimPrefix(pathValue, "/eth/")
	if candidate == "" {
		return "", "", false
	}

	slash := strings.Index(candidate, "/")
	if slash < 0 {
		return candidate, "", true
	}

	id := candidate[:slash]
	chainId := candidate[slash+1:]

	return id, chainId, id != ""
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		if a == "" {
			return "/" + b
		}
		return a + "/" + b
	}
	return a + b
}

func cloneURL(u *url.URL) *url.URL {
	if u == nil {
		return nil
	}
	cloned := *u
	return &cloned
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

// checkRPCMethods checks if any RPC method in the request is blocked
// Returns JSON-RPC error response if blocked, nil if allowed
func checkRPCMethods(bodyBytes []byte) []byte {
	var singleReq rpcRequest
	if err := json.Unmarshal(bodyBytes, &singleReq); err == nil && singleReq.Method != "" {
		if isMethodBlocked(singleReq.Method) {
			return createBlockedResponse(singleReq.ID)
		}
		return nil
	}

	var batchReq []rpcRequest
	if err := json.Unmarshal(bodyBytes, &batchReq); err == nil && len(batchReq) > 0 {
		var blockedIDs []interface{}
		for _, req := range batchReq {
			if isMethodBlocked(req.Method) {
				blockedIDs = append(blockedIDs, req.ID)
			}
		}
		if len(blockedIDs) > 0 {
			var responses []rpcResponse
			for _, id := range blockedIDs {
				responses = append(responses, rpcResponse{
					JsonRPC: "2.0",
					ID:      id,
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

func createBlockedResponse(id json.RawMessage) []byte {
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
