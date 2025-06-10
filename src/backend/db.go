package main

import (
	"context"
	"log"

	"github.com/jackc/pgx/v5"
)

type DB struct {
	conn *pgx.Conn
}

func NewDB(networkName string) *DB {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, "user=root host=/var/run/postgresql port=5432 dbname=cardano_" + networkName)
	if err != nil {
		log.Fatalf("Unable to connect to Postgres: %v", err)
	}

	log.Printf("Connected to postgres")

	return &DB{
		conn,
	}
}