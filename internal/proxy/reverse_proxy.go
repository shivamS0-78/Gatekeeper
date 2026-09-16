package proxy

import (
	"fmt"
	"io"
	"log"
	"net/http"
)

func proxyHandler(w http.ResponseWriter, r *http.Request) {
	targetUrl := "http://localhost:9000" + r.URL.Path

	req, err := http.NewRequest(
		r.Method,
		targetUrl,
		r.Body,
	)

	if err != nil {
		fmt.Printf("Not able to create request", http.StatusInternalServerError)
		return
	}

	req.Header = r.Header.Clone()

	resp, err := http.DefaultClient.Do(req)

	if err != nil {
		fmt.Printf("backend unavailable", http.StatusBadGateway)
	}

	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func main() {
	log.Println("Gatekeeper listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", http.HandlerFunc(proxyHandler)))
}
