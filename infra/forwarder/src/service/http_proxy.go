package forwarder

import (
	"errors"
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

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = p.base.Scheme
			req.URL.Host = p.base.Host
			req.Host = p.base.Host
			req.URL.Path = targetPath
			req.URL.RawPath = targetPath
			req.URL.RawQuery = query
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
