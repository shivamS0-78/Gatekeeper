package proxy

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

type ForwardOptions struct {
	Upstream    *url.URL // parsed route.Upstream
	StripPrefix string   // route.Path to strip, "" = keep path as-is
	Retries     int
}

type ReverseProxy struct {
	client *http.Client
}

func New(client *http.Client) *ReverseProxy {
	return &ReverseProxy{client: client}
}

func removeHopByHopHeaders(header http.Header) {
	//Headers associated with connection explicitly
	if connection := header.Get("Connection"); connection != "" {
		for _, h := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(h))
		}
	}
	//Standard hop-by-bop headers
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
}

func setForwardedHeaders(outReq *http.Request, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}

	prior := outReq.Header.Get("X-Forwarded-For")
	if prior != "" {
		outReq.Header.Set("X-Forwarded-For", prior+", "+host)
	} else {
		outReq.Header.Set("X-Forwarded-For", host)
	}

	outReq.Header.Set("X-Forwarded-Host", r.Host)
	outReq.Header.Set("X-Forwarded-Proto", "http")
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusBadGateway,      // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

func (p *ReverseProxy) doWithRetry(req *http.Request, retries int) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= retries; attempt++ {
		// Recreate request body for retry if necessary
		if attempt > 0 {
			if req.GetBody == nil {
				if req.Body != nil {
					return nil, fmt.Errorf("cannot retry request: body was drained and GetBody is nil")
				}
			} else {
				body, getBodyErr := req.GetBody()
				if getBodyErr != nil {
					return nil, getBodyErr
				}
				req.Body = body
			}
		}

		resp, err = p.client.Do(req)
		if err == nil {
			// Retry certain upstream HTTP failures
			if !isRetryableStatus(resp.StatusCode) {
				return resp, nil
			}
			if attempt == retries {
				return resp, nil
			}
			resp.Body.Close()
		} else {
			if attempt == retries {
				return nil, err
			}
		}

		// Exponential backoff
		delay := 100 * time.Millisecond * time.Duration(1<<attempt)
		time.Sleep(delay)
	}

	return resp, err
}

func (p *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request, opts ForwardOptions) {
	target := opts.Upstream

	// Create a copy of the incoming request
	outReq := r.Clone(r.Context())

	outReq.RequestURI = ""

	removeHopByHopHeaders(outReq.Header)

	// Forward the request to the configured upstream
	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host

	setForwardedHeaders(outReq, r)

	if opts.StripPrefix != "" {
		outReq.URL.Path = strings.TrimPrefix(outReq.URL.Path, opts.StripPrefix)

		if outReq.URL.Path == "" {
			outReq.URL.Path = "/"
		}
	}

	resp, err := p.doWithRetry(outReq, opts.Retries)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHopHeaders(resp.Header)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("Error copying response: %v", err)
	}
}
