package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const ExtraIndices = false

type DB struct {
	pool *pgxpool.Pool
}

type AssetAddress struct {
	Address  string `json:"address"`
	Quantity string `json:"quantity"` // can be very large, so use string instead of finite precision number (JSON doesn't support unbounded integers, and Golang doesn't have native support for unbounded integers)
}

type PolicyAsset struct {
	Asset    string `json:"asset"`
	Quantity string `json:"quantity"`
}

type UTXO struct {
	TxID        string        `json:"txID"`
	OutputIndex int           `json:"outputIndex"`
	Address     string        `json:"address"`
	Lovelace    string        `json:"lovelace"`
	Assets      []PolicyAsset `json:"assets,omitempty"`
	DatumHash   string        `json:"datumHash,omitempty"`
	InlineDatum string        `json:"inlineDatum,omitempty"`
	RefScript   string        `json:"refScript,omitempty"`
	ConsumedBy  string        `json:"consumedBy,omitempty"`
}

type TxBlockInfo struct {
	Hash        string `json:"hash"` // TODO: is this field truly necessary?
	BlockID     string `json:"block"`
	BlockHeight uint   `json:"block_height"`
	BlockTime   uint64 `json:"block_time"`
	Slot        uint64 `json:"slot"`
	Index       uint   `json:"index"`
}

func NewDB(networkName string) (*DB, error) {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, "user=root host=/var/run/postgresql port=5432 dbname=cardano_"+networkName)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to Postgres: %v", err)
	}

	return &DB{
		pool,
	}, nil
}

func (db *DB) AddressUTXOs(addr string, ctx context.Context) ([]UTXO, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	defer conn.Release()

	rows, err := conn.Query(ctx, queries["addresses_address_utxos_pure"], addr)
	if err != nil {
		return nil, err
	}

	utxos := make([]UTXO, 0)

	var (
		txID           string
		outputIndex    int
		lovelace       string
		rawAssets      *string
		rawDatumHash   *string
		rawInlineDatum *string
		rawRefScript   *string
	)

	_, err = pgx.ForEachRow(rows, []any{
		&txID,
		&outputIndex,
		&lovelace,
		&rawAssets,
		&rawDatumHash,
		&rawInlineDatum,
		&rawRefScript,
	}, func() error {
		assets := []PolicyAsset{}

		if rawAssets != nil {
			if err := json.Unmarshal([]byte(*rawAssets), &assets); err != nil {
				return err
			}
		}

		datumHash := ""
		if rawDatumHash != nil {
			datumHash = *rawDatumHash
		}

		inlineDatum := ""
		if rawInlineDatum != nil {
			inlineDatum = *rawInlineDatum
		}

		refScript := ""
		if rawRefScript != nil {
			refScript = *rawRefScript
		}

		utxos = append(utxos, UTXO{
			TxID:        txID,
			OutputIndex: outputIndex,
			Address:     addr,
			Lovelace:    lovelace,
			Assets:      assets,
			DatumHash:   datumHash,
			InlineDatum: inlineDatum,
			RefScript:   refScript,
			ConsumedBy:  "",
		})

		return nil
	})

	return utxos, err
}

func (db *DB) AddressUTXOsWithAsset(addr string, asset string, ctx context.Context) ([]UTXO, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	defer conn.Release()

	rows, err := conn.Query(ctx, queries["addresses_address_utxos_asset_pure"], addr, asset)
	if err != nil {
		return nil, err
	}

	utxos := make([]UTXO, 0)

	var (
		txID           string
		outputIndex    int
		lovelace       string
		rawAssets      *string
		rawDatumHash   *string
		rawInlineDatum *string
		rawRefScript   *string
	)

	_, err = pgx.ForEachRow(rows, []any{
		&txID,
		&outputIndex,
		&lovelace,
		&rawAssets,
		&rawDatumHash,
		&rawInlineDatum,
		&rawRefScript,
	}, func() error {
		assets := []PolicyAsset{}

		if rawAssets != nil {
			if err := json.Unmarshal([]byte(*rawAssets), &assets); err != nil {
				return err
			}
		}

		datumHash := ""
		if rawDatumHash != nil {
			datumHash = *rawDatumHash
		}

		inlineDatum := ""
		if rawInlineDatum != nil {
			inlineDatum = *rawInlineDatum
		}

		refScript := ""
		if rawRefScript != nil {
			refScript = *rawRefScript
		}

		utxos = append(utxos, UTXO{
			TxID:        txID,
			OutputIndex: outputIndex,
			Address:     addr,
			Lovelace:    lovelace,
			Assets:      assets,
			DatumHash:   datumHash,
			InlineDatum: inlineDatum,
			RefScript:   refScript,
			ConsumedBy:  "",
		})

		return nil
	})

	return utxos, err
}

func (db *DB) AssetAddresses(asset string, ctx context.Context) ([]AssetAddress, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	defer conn.Release()

	rows, err := conn.Query(ctx, queries["assets_asset_addresses"], asset)
	if err != nil {
		return nil, err
	}

	addresses := make([]AssetAddress, 0)

	var (
		address  string
		quantity string
	)

	_, err = pgx.ForEachRow(rows, []any{&address, &quantity}, func() error {
		addresses = append(addresses, AssetAddress{address, quantity})

		return nil
	})

	return addresses, err
}

func (db *DB) CreateIndices() error {
	ctx := context.Background()

	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return err
	}

	defer conn.Release()

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_block_hash_hex ON block USING HASH (encode(hash, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_datum_hash_hex ON datum USING HASH (encode(hash, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_ma_tx_mint_ident ON ma_tx_mint USING btree (ident)"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_ma_tx_out_ident ON ma_tx_out USING btree (ident)"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_multi_asset_policy_hex ON multi_asset USING HASH (encode(policy, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_multi_asset_policy_name_hex ON multi_asset USING HASH ((encode(policy, 'hex') || encode(name, 'hex')))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_redeemer_data_hash_hex ON redeemer_data USING HASH (encode(hash, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_redeemer_script_hash_hex ON redeemer USING HASH (encode(script_hash, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_redeemer_tx_id ON redeemer USING btree (tx_id)"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_reward_addr_epoch_covering ON reward (addr_id, spendable_epoch) INCLUDE (amount)"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_reward_rest_addr_id ON reward_rest USING btree (addr_id)"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_reward_rest_spendable_epoch ON reward_rest USING btree (spendable_epoch)"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_script_hash_hex ON script USING HASH (encode(hash, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_tx_hash_hex ON tx USING HASH (encode(hash, 'hex'))"); err != nil {
		return err
	}

	if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_tx_out_address ON tx_out USING HASH (address)"); err != nil {
		return err
	}

	if ExtraIndices {
		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_collateral_tx_in_tx_in_id ON collateral_tx_in (tx_in_id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_collateral_tx_out_tx_id ON collateral_tx_out USING btree (tx_id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_delegation_vote_addr_id ON delegation_vote USING hash (addr_id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_delegation_vote_drep_addr_txid ON delegation_vote (drep_hash_id, addr_id, tx_id, id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_drep_hash_has_script ON drep_hash USING hash (has_script)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_drep_hash_raw ON drep_hash USING hash (raw)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS bf_idx_drep_hash_raw_has_script ON drep_hash (raw, has_script)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_drep_hash_view ON drep_hash USING hash (view)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_drep_registration_hash_deposit ON drep_registration (drep_hash_id, deposit, tx_id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_pool_hash_view ON pool_hash USING HASH (view)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_reference_tx_in_tx_in_id ON reference_tx_in (tx_in_id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_stake_deregistration_addr_txid ON stake_deregistration (addr_id, tx_id)"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_tx_out_unspent_covering ON tx_out (stake_address_id) INCLUDE (value) WHERE consumed_by_tx_id IS NULL"); err != nil {
			return err
		}

		if _, err := conn.Exec(ctx, "CREATE UNIQUE INDEX IF NOT EXISTS unique_epoch_stake_epoch_and_id ON epoch_stake (epoch_no, id)"); err != nil {
			return err
		}
	}

	return nil
}

func (db *DB) LatestEpoch(ctx context.Context) (uint64, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return 0, err
	}

	defer conn.Release()

	query := conn.QueryRow(ctx, queries["network_epoch"])

	var epoch uint64

	if err := query.Scan(&epoch); err != nil {
		return 0, err
	}

	return epoch, nil
}

func (db *DB) PolicyAssets(policy string, ctx context.Context) ([]PolicyAsset, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	defer conn.Release()

	rows, err := conn.Query(ctx, queries["assets_policy_policy_id"], policy)
	if err != nil {
		return nil, err
	}

	assets := make([]PolicyAsset, 0)

	var (
		asset    string
		quantity string
	)

	_, err = pgx.ForEachRow(rows, []any{&asset, &quantity}, func() error {
		assets = append(assets, PolicyAsset{asset, quantity})

		return nil
	})

	return assets, err
}

func (db *DB) TxBlockInfo(txID string, ctx context.Context) (TxBlockInfo, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return TxBlockInfo{}, err
	}

	defer conn.Release()

	query := conn.QueryRow(ctx, queries["txs_hash_summary"], txID)

	var (
		hash        string
		block       string
		blockHeight uint
		blockTime   uint64
		slot        uint64
		index       uint
	)

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

func (db *DB) FilterMissingTxs(txIDs []string, ctx context.Context) ([]string, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}

	defer conn.Release()

	rows, err := conn.Query(ctx, queries["txs_filter_missing"], txIDs)
	if err != nil {
		return nil, err
	}

	missing := make([]string, 0)
	var id string
	_, err = pgx.ForEachRow(rows, []any{&id}, func() error {
		missing = append(missing, id)
		return nil
	})

	return missing, err
}

func (db *DB) UTXO(txID string, outputIndex int, ctx context.Context) (UTXO, error) {
	conn, err := db.pool.Acquire(ctx)
	if err != nil {
		return UTXO{}, err
	}

	defer conn.Release()

	query := conn.QueryRow(ctx, queries["utxo"], txID, outputIndex)

	var (
		address        string
		lovelace       string
		rawAssets      *string
		rawDatumHash   *string
		rawInlineDatum *string
		rawRefScript   *string
		rawConsumedBy  *string
	)

	if err := query.Scan(
		&address,
		&lovelace,
		&rawAssets,
		&rawDatumHash,
		&rawInlineDatum,
		&rawRefScript,
		&rawConsumedBy,
	); err != nil {
		return UTXO{}, err
	}

	assets := []PolicyAsset{}

	if rawAssets != nil {
		if err := json.Unmarshal([]byte(*rawAssets), &assets); err != nil {
			return UTXO{}, err
		}
	}

	datumHash := ""
	if rawDatumHash != nil {
		datumHash = *rawDatumHash
	}

	inlineDatum := ""
	if rawInlineDatum != nil {
		inlineDatum = *rawInlineDatum
	}

	refScript := ""
	if rawRefScript != nil {
		refScript = *rawRefScript
	}

	consumedBy := ""
	if rawConsumedBy != nil {
		consumedBy = *rawConsumedBy
	}

	return UTXO{
		TxID:        txID,
		OutputIndex: outputIndex,
		Address:     address,
		Lovelace:    lovelace,
		Assets:      assets,
		DatumHash:   datumHash,
		InlineDatum: inlineDatum,
		RefScript:   refScript,
		ConsumedBy:  consumedBy,
	}, nil
}
