SELECT encode(tx.hash, 'hex') AS "txID",
  txo.index AS "outputIndex",
  txo.value::TEXT AS "lovelace", -- cast to TEXT to avoid number overflow
  (
    SELECT json_agg(
        json_build_object(
          'asset',
          CONCAT(encode(ma.policy, 'hex'), encode(ma.name, 'hex')),
          'quantity',
          mto.quantity::TEXT -- cast to TEXT to avoid number overflow
        )
      )
    FROM ma_tx_out mto
      JOIN multi_asset ma ON (mto.ident = ma.id)
    WHERE mto.tx_out_id = txo.id
  ) AS "assets",
  encode(data_hash, 'hex') AS "datumHash",
  encode(dat.bytes, 'hex') AS "inlineDatum",
  encode(scr.bytes, 'hex') AS "refScript"
FROM tx
  JOIN tx_out txo ON (tx.id = txo.tx_id)
  LEFT JOIN datum dat ON (txo.inline_datum_id = dat.id)
  LEFT JOIN script scr ON (txo.reference_script_id = scr.id)
WHERE txo.address = $1 AND txo.consumed_by_tx_id IS NULL