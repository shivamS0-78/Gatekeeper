package database

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
		pgConnStr = "postgres://postgres:password@localhost:5432/ratelimiter?sslmode=disable"
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
		redisAddr = "localhost:6379"
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
