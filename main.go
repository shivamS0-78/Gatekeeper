package main

import (
	"log"
	"net/http"
)

func main() {
	clients, err := InitDB()
	if err != nil {
		log.Fatalf("Failed to Initialize DB : %v", err)
	}

	defer clients.PG.Close()

	ruleCache := NewRuleCache()

	mux := http.NewServeMux()

	apiHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("COntent-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "request processed successfully"}`))
	})

	mux.Handle("/api/resource", RateLimit(clients.PG, clients.Redis, ruleCache, apiHandler))

	log.Println("Rate limiter running at port 8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("Server crashed : %v", err)
	}
}
