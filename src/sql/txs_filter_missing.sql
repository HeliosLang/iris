WITH input(txid) AS (
    SELECT unnest($1::text[])
)
SELECT txid
FROM input
WHERE NOT EXISTS (
    SELECT 1 FROM tx WHERE encode(hash, 'hex') = input.txid
);
