package gateway

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"rate-limiter-api/internal/config"
	"rate-limiter-api/internal/proxy"
	"rate-limiter-api/internal/ratelimiter"
)

type Upstream struct {
	URL     string
	Healthy bool
	mu      sync.RWMutex
}

type Gateway struct {
	routes    []config.Route
	limiter   *ratelimiter.RateLimiter
	ruleCache *ratelimiter.RuleCache
	proxy     *proxy.ReverseProxy

	upstreams    map[string][]*Upstream
	nextUpstream atomic.Uint64
	healthClient *http.Client
}

func (u *Upstream) isHealthy() bool {
	u.mu.RLock()
	defer u.mu.RUnlock()

	return u.Healthy
}

func (u *Upstream) SetHealthy(healthy bool) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.Healthy = healthy
}

func (g *Gateway) checkUpstream(upstream *Upstream) {
	resp, err := g.healthClient.Get(upstream.URL + "/health")

	if err != nil {
		upstream.SetHealthy(false)
		return
	}

	defer resp.Body.Close()

	upstream.SetHealthy(resp.StatusCode >= 200 && resp.StatusCode < 300)
}

func (g *Gateway) checkAllUpstreams() {
	for _, upstreams := range g.upstreams {
		for _, upstream := range upstreams {
			g.checkUpstream(upstream)
		}
	}
}

func (g *Gateway) StartHealthChecks(interval time.Duration) {
	ticker := time.NewTicker(interval)

	go func() {
		defer ticker.Stop()
		for {
			g.checkAllUpstreams()

			<-ticker.C
		}
	}()
}

func (g *Gateway) selectUpstream(route *config.Route) string {

	upstreams := g.upstreams[route.Path]

	if len(upstreams) == 0 {
		return ""
	}
	start := g.nextUpstream.Add(1) - 1

	for i := uint64(0); i < uint64(len(upstreams)); i++ {
		index := (start + i) % uint64(len(upstreams))

		if upstreams[index].isHealthy() {
			return upstreams[index].URL
		}
	}

	return ""
}

func New(routes []config.Route, limiter *ratelimiter.RateLimiter,
	ruleCache *ratelimiter.RuleCache, rp *proxy.ReverseProxy) *Gateway {

	upstreams := make(map[string][]*Upstream)

	for _, route := range routes {
		for _, url := range route.Upstreams {
			upstreams[route.Path] = append(
				upstreams[route.Path],
				&Upstream{
					URL:     url,
					Healthy: true,
				},
			)
		}
	}

	healthClient := &http.Client{
		Timeout: 2 * time.Second,
	}

	return &Gateway{
		routes:       routes,
		limiter:      limiter,
		ruleCache:    ruleCache,
		proxy:        rp,
		upstreams:    upstreams,
		healthClient: healthClient,
	}
}

func (g *Gateway) MatchRoute(path string) (*config.Route, bool) {
	for i := range g.routes {
		route := &g.routes[i]

		if path == route.Path || strings.HasPrefix(path, route.Path+"/") {
			return route, true
		}
	}
	return nil, false
}

func apiHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte(`{"error": "no route matched"}`))
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

	ratelimiter.SetRateLimitHeaders(w, int64(rule.Limit), remaining, resetTime)

	if !allowed {
		ratelimiter.WriteDenied(w)
		return
	}

	route, ok := g.MatchRoute(r.URL.Path)

	if !ok {
		apiHandler(w, r)
		return
	}

	upstream := g.selectUpstream(route)

	if upstream == "" {
		http.Error(w, "No heakthy upstream available", http.StatusServiceUnavailable)
	}

	target, err := url.Parse(upstream)
	if err != nil {
		http.Error(w, "Invalid upstream", http.StatusBadGateway)
		return
	}

	g.proxy.ServeHTTP(w, r, proxy.ForwardOptions{
		Upstream:    target,
		StripPrefix: stripPrefixPath(route),
		Retries:     route.Retries,
	})
}

func stripPrefixPath(route *config.Route) string {
	if route.StripPrefix {
		return route.Path
	}
	return ""
}
