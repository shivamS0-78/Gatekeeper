package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

type Clients struct {
	PG    *sql.DB
	Redis *redis.Client
}

func InitDB() (*Clients, error) {
	pgConnStr := os.Getenv("DATABASE_URL")
	if pgConnStr == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}

	db, err := sql.Open("postgres", pgConnStr)
	if err != nil {
		return nil, fmt.Errorf("postgres connect error : %w", err)
	}
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("postgres ping error : %w", err)
	}

	redisAddr := os.Getenv("REDDIS_ADDR")
	if redisAddr == "" {
		return nil, fmt.Errorf("REDDIS_ADDR not set")
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis connect error %w", err)
	}

	log.Println("Database Connection Established")

	return &Clients{PG: db, Redis: rdb}, nil
}
