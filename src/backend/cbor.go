package main

import (
	"encoding/hex"
	"math/big"

	"github.com/blinklabs-io/gouroboros/ledger"
)

type EncodedPair struct {
	Key []byte
	Value []byte
}

// the cbor packages that operate unsing Marshal/Unmarshal and tags aren't flexible enough

func EncodeAddress(addr string) ([]byte, error) {
	a, err := ledger.NewAddress(addr)
	if err != nil {
		return nil, err
	}

	return a.MarshalCBOR()
}

func EncodeAssets(assets []PolicyAsset) ([]byte, error) {
	// first collect per policy
	m := make(map[string]map[string]string)

	for _, asset := range assets {
		mph := asset.Asset[0:56]
		tokenName := asset.Asset[56:]

		if prev, ok := m[mph]; ok {
			prev[tokenName] = asset.Quantity
		} else {
			m[mph] = map[string]string{
				tokenName: asset.Quantity,
			}
		}
	}

	outerPairs := []EncodedPair{}

	for policy, tokens := range m {
		innerPairs := []EncodedPair{}

		for tokenName, qty := range tokens {
			encodedTokenName, err := EncodeHexBytes(tokenName)
			if err != nil {
				return nil, err
			}

			encodedQuantity, err := EncodeLargeInt(qty)
			if err != nil {
				return nil, err
			}

			innerPairs = append(innerPairs, EncodedPair{
				Key: encodedTokenName,
				Value: encodedQuantity,
			})
		}

		encodedPolicy, err := EncodeHexBytes(policy)
		if err != nil {
			return nil, err
		}

		outerPairs = append(
			outerPairs, EncodedPair{
				Key: encodedPolicy,
				Value: EncodeMap(innerPairs),
			},
		)
	}
	
	return EncodeMap(outerPairs), nil
}

func EncodeBytes(bs []byte) []byte {
	wrapped := encodeDefHead(2, big.NewInt(int64(len(bs))))
	wrapped = append(wrapped, bs...)

	return wrapped
}   

func EncodeDefList(entries [][]byte) []byte {
	bs := encodeDefListStart(len(entries))

	for _, entry := range entries {
		bs = append(bs, entry...)
	}

	return bs
}

func EncodeDefMap(pairs []EncodedPair) []byte {
	bs := encodeDefMapStart(len(pairs))

	for _, pair := range pairs {
		bs = append(bs, (pair.Key)...)
		bs = append(bs, (pair.Value)...)
	}

	return bs
}



func EncodeHashedDatum(hash string) ([]byte, error) {
	bs, err := hex.DecodeString(hash)
	if err != nil {
		return nil, err
	}

	return EncodeTuple(
		EncodeInt(0),
		EncodeBytes(bs),
	), nil
}

func EncodeIndefList(entries [][]byte) []byte {
	bs := encodeIndefListStart()

	for _, entry := range entries {
		bs = append(bs, entry...)
	}

	bs = append(bs, encodeIndefListEnd()...)

	return bs
}

func EncodeIndefMap(pairs []EncodedPair) []byte {
	bs := encodeIndefMapStart()

	for _, pair := range pairs {
		bs = append(bs, pair.Key...)
		bs = append(bs, pair.Value...)
	}

	return bs
}

func EncodeInlineDatum(inlineDatum string) ([]byte, error) {
	bs, err := hex.DecodeString(inlineDatum)
	if err != nil {
		return nil, err
	}

	return EncodeTuple(
		EncodeInt(1),
		append(EncodeTag(24), EncodeBytes(bs)...),
	), nil
}

func EncodeInt(x int64) []byte {
	return encodeInt(big.NewInt(x))
}


func EncodeLargeInt(x string) ([]byte, error) {
	var z big.Int

	if err := (&z).UnmarshalText([]byte(x)); err != nil {
		return nil, err
	}

	return encodeInt(&z), nil
}

func EncodeList(entries [][]byte) []byte {
	if len(entries) > 0 {
		return EncodeIndefList(entries)
	} else {
		return EncodeDefList(entries)
	}
}

func EncodeMap(pairs []EncodedPair) []byte {
	return EncodeDefMap(pairs)
}

func EncodeObjectIKey(fields map[int][]byte) []byte {
	pairs := []EncodedPair{}

	for i, f := range fields {
		pairs = append(pairs, EncodedPair{
			Key: EncodeInt(int64(i)),
			Value: f,
		})
	}

	return EncodeDefMap(pairs)
}

func EncodeRefScript(refScript string) ([]byte, error) {
	// these are the raw inner flat bytes
	bs, err := hex.DecodeString(refScript)
	if err != nil {
		return nil, err
	}

	return append(EncodeTag(24), EncodeBytes(
		EncodeTuple(
			EncodeInt(2), // tag for PlutusV2 (TODO: get plutus version from somewhere)
			EncodeBytes(bs), // the flat bytes must be wrapped at least once
		),
	)...), nil
}

func EncodeTag(tag int) []byte {
	return encodeDefHead(6, big.NewInt(int64(tag)))
}

// the entries are already encoded
func EncodeTuple(entries ...[]byte) []byte {
	return EncodeDefList(entries)
}

func EncodeTxOutput(
	addr string,
	lovelace string,
	assets []PolicyAsset,
	datumHash string,
	inlineDatum string,
	refScript string,
) ([]byte, error) {
	fields := map[int][]byte{}
	var err error

	fields[0], err = EncodeAddress(addr)
	if err != nil {
		return nil, err
	}

	fields[1], err = EncodeValue(lovelace, assets)
	if err != nil {
		return nil, err
	}

	if datumHash != "" {
		if inlineDatum == "" {
			fields[2], err = EncodeHashedDatum(datumHash)
			if err != nil {
				return nil, err
			}
		} else {
			fields[2], err = EncodeInlineDatum(inlineDatum)
			if err != nil {
				return nil, err
			}
		}
	}

	if refScript != "" {
		fields[3], err = EncodeRefScript(refScript)
		if err != nil {
			return nil, err
		}
	}

	return EncodeObjectIKey(fields), nil
}

func EncodeHexBytes(h string) ([]byte, error) {
	bs, err := hex.DecodeString(h)
	if err != nil {
		return nil, err
	}

	return EncodeBytes(bs), nil
}

func EncodeTxOutputID(txID string, outputIndex int) ([]byte, error) {
	encodedTxID, err := EncodeHexBytes(txID)
	if err != nil {
		return nil, err
	}

	return EncodeTuple(
		encodedTxID,
		EncodeInt(int64(outputIndex)),
	), nil
}

func EncodeUTXO(utxo UTXO) ([]byte, error) {
	encodedTxOutputID, err := EncodeTxOutputID(
		utxo.TxID,
		utxo.OutputIndex,
	)
	if err != nil {
		return nil, err
	}

	encodedTxOutput, err := EncodeTxOutput(
		utxo.Address,
		utxo.Lovelace,
		utxo.Assets,
		utxo.DatumHash,
		utxo.InlineDatum,
		utxo.RefScript,
	)

	if err != nil {
		return nil, err
	}

	return EncodeTuple(
		encodedTxOutputID,
		encodedTxOutput,
	), nil
}

func EncodeValue(lovelace string, assets []PolicyAsset) ([]byte, error) {
	if len(assets) == 0 {
		return EncodeLargeInt(lovelace)
	} else {
		encodedLovelace, err := EncodeLargeInt(lovelace)
		if err != nil {
			return nil, err
		}

		encodedAssets, err := EncodeAssets(assets)
		if err != nil {
			return nil, err
		}

		return EncodeTuple(
			encodedLovelace,
			encodedAssets,
		), nil
	}
}

func encodeDefHead(m int, n *big.Int) []byte {
	if (n.Cmp(big.NewInt(23)) < 1) {
        return []byte{byte(32 * m + int(n.Int64()))}
    } else if (n.Cmp(big.NewInt(24)) >= 0 && n.Cmp(big.NewInt(256)) < 0) {
        return []byte{
			byte(32 * m + 24),
			byte(n.Int64()),
		}
    } else if (n.Cmp(big.NewInt(256)) >= 0 && n.Cmp(big.NewInt(256*256)) < 0) {
        return []byte{
            byte(32 * m + 25),
            byte((int(n.Int64()) / 256) % 256),
            byte(int(n.Int64()) % 256),
		}
	} else if (n.Cmp(big.NewInt(256*256)) >= 0 && n.Cmp(big.NewInt(256*256*256*256)) < 0) {
		e4 := encodeIntBigEndian(n)

		for len(e4) < 4 {
			e4 = append([]byte{0}, e4...)
		}

		return append([]byte{byte(32*m + 26)}, e4...)
    } else {
		L := big.NewInt(256*256*256*256)

		var z big.Int

		(&z).Mul(L, L)

		if n.Cmp(L) >= 0 && n.Cmp(&z) < 0 {
			e8 := encodeIntBigEndian(n)

			for len(e8) < 8 {
				e8 = append([]byte{0}, e8...)
			}

			return append([]byte{byte(32*m+27)}, e8...)
		} else {
			panic("unsupported n")
		}
	}
}

func encodeDefListStart(n int) []byte {
	return encodeDefHead(4, big.NewInt(int64(n)))
}

func encodeDefMapStart(n int) []byte {
	return encodeDefHead(5, big.NewInt(int64(n)))
}

func encodeIndefHead(m int) []byte {
	return []byte{byte(32*m  + 31)}
}

func encodeIndefListEnd() []byte {
	return []byte{255}
}

func encodeIndefListStart() []byte {
	return encodeIndefHead(4)
}

func encodeIndefMapEnd() []byte {
	return []byte{255}
}

func encodeIndefMapStart() []byte {
	return encodeIndefHead(5)
}

func encodeInt(x *big.Int) []byte {
	var lim big.Int
	var negLim big.Int

	(&lim).Lsh(big.NewInt(2), 63)
	(&negLim).Neg(&lim)

	if (x.Cmp(big.NewInt(0)) >= 0 && x.Cmp(&lim) < 0) {
        return encodeDefHead(0, x)
	} else if (x.Cmp(&lim) >= 0) {
		bs := encodeDefHead(6, big.NewInt(2))

		return append(bs, EncodeBytes(encodeIntBigEndian(x))...)
	} else if (x.Cmp(big.NewInt(-1)) <= 0 && x.Cmp(&negLim) >= 0) {
		var z big.Int
		(&z).Neg(x)

		var z_ big.Int

		(&z_).Sub(&z, big.NewInt(-1))

        return encodeDefHead(1, &z_)
    } else {
		bs := encodeDefHead(6, big.NewInt(3))

		var z big.Int
		(&z).Neg(x)

		var z_ big.Int

		(&z_).Sub(&z, big.NewInt(-1))

        return encodeDefHead(1, &z_)

		return append(bs, EncodeBytes(encodeIntBigEndian(&z_))...)
    }
}

func encodeIntBigEndian(x *big.Int) []byte {
	if (x.Cmp(big.NewInt(0)) == 0) {
        return []byte{0}
    } else {
        res := []byte{}

        for x.Cmp(big.NewInt(0)) > 0 {
			var z big.Int
			var m big.Int

			z.DivMod(x, big.NewInt(256), &m)

			res = append([]byte{byte(m.Int64())}, res...)

            x = &z
        }

        return res
    }
}