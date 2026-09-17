package gateway

import (
	"log"
	"net/http"
	"net/url"
	"strings"

	"rate-limiter-api/internal/config"
	"rate-limiter-api/internal/proxy"
	"rate-limiter-api/internal/ratelimiter"
)

type Gateway struct {
	routes    []config.Route
	limiter   *ratelimiter.RateLimiter
	ruleCache *ratelimiter.RuleCache
	proxy     *proxy.ReverseProxy
}

func New(routes []config.Route, limiter *ratelimiter.RateLimiter,
	ruleCache *ratelimiter.RuleCache, rp *proxy.ReverseProxy) *Gateway {
	return &Gateway{
		routes:    routes,
		limiter:   limiter,
		ruleCache: ruleCache,
		proxy:     rp,
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

	target, err := url.Parse(route.Upstream)
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
