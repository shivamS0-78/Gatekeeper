package config

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	Port int
}

type Route struct {
	Path        string
	Upstream    string
	StripPrefix bool
}

type Config struct {
	Server ServerConfig
	Routes []Route
}

type Gateway struct {
	routes []Route
	client *http.Client
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

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	resp, err := g.client.Do(outReq)
	if err != nil {
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
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
	}

	config, err := LoadConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}

	gateway := Gateway{
		routes: config.Routes,
		client: client,
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
