SELECT encode(tx.hash, 'hex') AS "hash",
  encode(b.hash, 'hex') AS "block",
  block_no AS "block_height",
  extract(
    epoch
    FROM b.time
  )::INTEGER AS "block_time",
  b.slot_no AS "slot",
  tx.block_index AS "index"
FROM tx
  JOIN block b ON (tx.block_id = b.id)
WHERE encode(tx.hash, 'hex') = $1
