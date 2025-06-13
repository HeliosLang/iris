package main

import (
	"context"
	"fmt"
	"log"

	"github.com/jackc/pgx/v5"
)

type DB struct {
	conn *pgx.Conn
}

type AssetAddress struct {
	Address string `json:"address"`
	Quantity string `json:"quantity"` // can be very large, so use string instead of finite precision number (JSON doesn't support unbounded integers, and Golang doesn't have native support for unbounded integers)
}

type PolicyAsset struct {
	Asset string `json:"asset"`
	Quantity string `json:"quantity"`
}

type TxBlockInfo struct {
	Hash string `json:"hash"` // TODO: is this field truly necessary?
	BlockID string `json:"block"`
	BlockHeight uint `json:"block_height"`
	BlockTime uint64 `json:"block_time"`
	Slot uint64 `json:"slot"`
	Index uint `json:"index"`
}

func NewDB(networkName string) (*DB, error) {
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, "user=root host=/var/run/postgresql port=5432 dbname=cardano_" + networkName)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to Postgres: %v", err)
	}

	log.Printf("Connected to postgres")

	return &DB{
		conn,
	}, nil
}

func (db *DB) AssetAddresses(asset string, ctx context.Context) ([]AssetAddress, error) {
	rows, err := db.conn.Query(ctx, queries["unpaged/assets_asset_addresses"], "desc", asset)
	if err != nil {
		return nil, err
	}

	addresses := make([]AssetAddress, 0)

	var address string
	var quantity string

	_, err = pgx.ForEachRow(rows, []any{&address, &quantity}, func () error {
		addresses = append(addresses, AssetAddress{address, quantity})

		return nil
	})

	return addresses, err
}

func (db *DB) LatestEpoch(ctx context.Context) (uint64, error) {
	var epoch uint64

	if err := db.conn.QueryRow(ctx, queries["network_epoch"]).Scan(&epoch); err != nil {
		return 0, err
	}

	return epoch, nil
}

func (db *DB) PolicyAssets(policy string, ctx context.Context) ([]PolicyAsset, error) {
	var asset string
	var quantity string

	assets := make([]PolicyAsset, 0)
	
	rows, err := db.conn.Query(ctx, queries["unpaged/assets_policy_policy_id"], "desc", policy)
	if err != nil {
		return nil, err
	}

	_, err = pgx.ForEachRow(rows, []any{&asset, &quantity}, func () error {
		assets = append(assets, PolicyAsset{asset, quantity})

		return nil
	})

	return assets, err
}

func (db *DB) TxBlockInfo(txID string, ctx context.Context) (TxBlockInfo, error) {
	var hash string
	var block string
	var blockHeight uint
	var blockTime uint64
	var slot uint64
	var index uint

	query := db.conn.QueryRow(ctx, queries["txs_hash_summary"], txID)

	if err := query.Scan(
		&hash,
		&block,
		&blockHeight,
		&blockTime,
		&slot,
		&index,
	); err != nil {
		return TxBlockInfo{}, err
	}

	return TxBlockInfo{
		hash,
		block,
		blockHeight,
		blockTime,
		slot,
		index,
	}, nil
}