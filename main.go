package main

import (
	"log"
)

func main() {
	clients, err := InitDB()
	if err != nil {
		log.Fatalf("Failed to Initialize DB : %v", err)
	}

	defer clients.PG.Close()

}
