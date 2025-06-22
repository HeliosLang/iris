SELECT asset AS "asset",
  quantity::TEXT AS "quantity" -- cast to TEXT to avoid number overflow
FROM (
    SELECT MIN(mtm.id),
      CONCAT(encode(ma.policy, 'hex'), encode(ma.name, 'hex')) AS "asset",
      SUM(quantity) AS "quantity"
    FROM ma_tx_mint mtm
      JOIN multi_asset ma ON (mtm.ident = ma.id)
    WHERE encode(policy, 'hex') = $1
    GROUP BY policy, name
  ) AS "ordered assets"