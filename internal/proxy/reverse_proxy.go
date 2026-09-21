package proxy

import (
	"net"
	"net/http"
	"net/url"
	"strings"
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

type ReverseProxy struct {
	client *http.Client
}

func New(client *http.Client) *ReverseProxy {
	return &ReverseProxy{client: client}
}

func RemoveHopByHopHeaders(header http.Header) {
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

func (p *ReverseProxy) Do(req *http.Request) (*http.Response, error) {
	return p.client.Do(req)
}

func (p *ReverseProxy) PrepareRequest(
	r *http.Request,
	target *url.URL,
	stripPrefix string,
) *http.Request {
	outReq := r.Clone(r.Context())

	outReq.RequestURI = ""

	RemoveHopByHopHeaders(outReq.Header)

	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host

	setForwardedHeaders(outReq, r)

	if stripPrefix != "" {
		outReq.URL.Path = strings.TrimPrefix(
			outReq.URL.Path,
			stripPrefix,
		)

		if outReq.URL.Path == "" {
			outReq.URL.Path = "/"
		}
	}

	return outReq
}

func IsRetryableStatus(status int) bool {
	switch status {
	case http.StatusBadGateway, // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}
