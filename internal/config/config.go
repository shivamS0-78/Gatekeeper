package config

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"rate-limiter-api/internal/ratelimiter"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port int
}

type Route struct {
	Path        string
	Upstream    string
	StripPrefix bool
	Retries     int
}

type Config struct {
	Server ServerConfig
	Routes []Route
}

type Gateway struct {
	routes    []Route
	client    *http.Client
	limiter   *ratelimiter.RateLimiter
	ruleCache *ratelimiter.RuleCache
}

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

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)

	if err != nil {
		return nil, err
	}

	var config Config

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (g *Gateway) MatchRoute(path string) (*Route, bool) {
	for i := range g.routes {
		route := &g.routes[i]

		if path == route.Path || strings.HasPrefix(path, route.Path+"/") {
			return route, true
		}
	}
	return nil, false
}

func removeHopByHopHeaders(header http.Header) {
	//Headers associated with connection explicitly
	if connection := header.Get("Connection"); connection != " " {
		for _, h := range strings.Split(connection, ",") {
			header.Del(strings.TrimSpace(h))
		}
	}
	//Standard hop-by-bop headers
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
}

func isRetryableStatus(status int) bool {
	switch status {
	case http.StatusBadGateway, // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout:     // 504
		return true
	default:
		return false
	}
}

func (g *Gateway) doWithRetry(req *http.Request, retries int) (*http.Response, error) {
	var resp *http.Response
	var err error

	for attempt := 0; attempt <= retries; attempt++ {
		// Recreate request body for retry if necessary
		if attempt > 0 && req.GetBody != nil {
			body, getBodyErr := req.GetBody()
			if getBodyErr != nil {
				return nil, getBodyErr
			}
			req.Body = body
		}

		resp, err = g.client.Do(req)
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

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		userID = r.RemoteAddr
	}

	rule, err := g.ruleCache.GetRule(nil, userID)
	if err != nil {
		log.Printf("[Rule error] %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	allowed, remaining, resetTime, err :=
		g.limiter.AllowSlidingWindow(
			ctx,
			userID,
			rule.Limit,
			rule.Window,
		)

	if err != nil {
		log.Printf("[Rate limiter error] %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set(
		"X-Rate-Limit-Limit",
		fmt.Sprintf("%d", rule.Limit),
	)

	w.Header().Set(
		"X-Rate-Limit-Remaining",
		fmt.Sprintf("%d", remaining),
	)

	w.Header().Set(
		"X-Rate-Limit-Reset",
		fmt.Sprintf("%d", resetTime),
	)

	if !allowed {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"rate limit exceeded"}`))
		return
	}

	route, ok := g.MatchRoute(r.URL.Path)

	if !ok {
		http.NotFound(w, r)
		return
	}

	target, err := url.Parse(route.Upstream)
	if err != nil {
		http.Error(w, "Invalid upstream", http.StatusBadGateway)
		return
	}

	// Create a copy of the incoming request
	outReq := r.Clone(r.Context())

	removeHopByHopHeaders(outReq.Header)

	// Forward the request to the configured upstream
	outReq.URL.Scheme = target.Scheme
	outReq.URL.Host = target.Host
	outReq.Host = target.Host

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		outReq.Header.Set("X-Forwarded-For", host)
	} else {
		outReq.Header.Set("X-Forwarded-For", r.RemoteAddr)
	}
	outReq.Header.Set("X-Forwarded-Host", r.Host)
	outReq.Header.Set("X-Forwarded-Proto", "http")

	if route.StripPrefix {
		outReq.URL.Path = strings.TrimPrefix(
			outReq.URL.Path,
			route.Path,
		)

		if outReq.URL.Path == "" {
			outReq.URL.Path = "/"
		}
	}

	resp, err := g.doWithRetry(outReq, route.Retries)

	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			http.Error(w, "Gateway Timeout", http.StatusGatewayTimeout)
			return
		}
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	removeHopByHopHeaders(outReq.Header)

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Copy status code
	w.WriteHeader(resp.StatusCode)

	_, err = io.Copy(w, resp.Body)
	if err != nil {
		log.Printf("Error copying response: %v", err)
	}
}

func main() {
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	limiter := ratelimiter.NewRateLimiter(rdb)
	ruleCache := ratelimiter.NewRuleCache()

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,

		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).DialContext,

		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	config, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	gateway := Gateway{
		routes:    config.Routes,
		client:    client,
		limiter:   limiter,
		ruleCache: ruleCache,
	}

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", config.Server.Port),
		Handler: &gateway,
	}

	log.Printf("Gateway running on :%d", config.Server.Port)

	if err := server.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
