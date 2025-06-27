package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"unicode/utf8"

	"github.com/blinklabs-io/gouroboros/ledger"
)

// This is quick-n-dirty CBOR encoding/decoding functionality.
// I implemented this because gouroboros doesn't seem to do some things correctly.
// Implementing this took about 12 hours, and we needed it quickly and couldn't wait for resolution of bugs in gouroboros repo

type Decoded interface {
	Cbor() []byte
}

type DecodedBool struct {
	Value bool
}

type DecodedBytes struct {
	Type string // "indef" or "def"
	Bytes []byte
}

type DecodedConstr struct {
	Tag int
	Fields *DecodedList
}

type DecodedEnvelope struct {
	Tag int
	Type string // "indef" or "def"
	Inner Decoded
}

type DecodedInt struct {
	Value *big.Int
}

type DecodedString struct {
	Type string // "single" or "list"
	Value string
}

// also used for set
type DecodedList struct {
	Type string // "indef", "def" or "set"
	Items []Decoded
}

type DecodedMap struct {
	Type string // "indef" or "def"
	Pairs []DecodedPair
}

type DecodedNull struct {}

type DecodedPair struct {
	Key Decoded
	Value Decoded
}

type EncodedPair struct {
	Key []byte
	Value []byte
}

type Stream struct {
	cbor []byte
	pos int
}

func NewStream(cbor []byte) (*Stream, error) {
	if len(cbor) == 0 {
		return nil, errors.New("expected at least 1 byte")
	}

	return &Stream{cbor, 0}, nil
}

// the cbor packages that operate using Marshal/Unmarshal and tags aren't flexible enough

// simply call recursively
func Decode(cbor []byte) (Decoded, error) {
	stream, err := NewStream(cbor)
	if err != nil {
		return nil, err
	}

	return decode(stream)
}

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
	return encodeDefBytes(bs)
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

func encodeDefBytes(bs []byte) []byte {
	wrapped := encodeDefHead(2, big.NewInt(int64(len(bs))))
	wrapped = append(wrapped, bs...)

	return wrapped
}

func encodeIndefBytes(bs []byte) []byte {
	wrapped := encodeIndefHead(2)

	for len(bs) > 0 {
		chunk := bs[0:64]
		bs = bs[64:]

		wrapped = append(wrapped, encodeDefHead(2, big.NewInt(int64(len(chunk))))...)
		wrapped = append(wrapped, chunk...)
	}

	wrapped = append(wrapped, 255)

	return wrapped
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

// calculate -x -1
func negMinusOne(x *big.Int) *big.Int {
	var a big.Int
	(&a).Neg(x)

	var b big.Int
	(&b).Sub(&a, big.NewInt(1))

	return &b
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
        return encodeDefHead(1, negMinusOne(x))
    } else {
		bs := encodeDefHead(6, big.NewInt(3))

		return append(bs, EncodeBytes(encodeIntBigEndian(negMinusOne(x)))...)
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

func majorType(bs []byte) int {
	return int(bs[0])/32
}

func decode(s *Stream) (Decoded, error) {
	// peaks the first byte
	if s.isBool() {
		return decodeBool(s)
	} else if s.isBytes() {
		return decodeBytes(s)
	} else if s.isConstr() {
		return decodeConstr(s)
	} else if s.isInt() {
		return decodeInt(s)
	} else if s.isString() {
		return decodeString(s)
	} else if s.isList() {
		return decodeList(s)
	} else if s.isSet() {
		return decodeSet(s)
	} else if s.isTag() {
		return decodeEnvelope(s)
	} else if s.isMap() {
		return decodeMap(s)
	} else if s.isNull() {
		return decodeNull(s)
	} else {
		b, err := s.peekOne()
		if err != nil {
			return nil, err
		}

		return nil, fmt.Errorf("unhandled cbor type %d (major: %d)", b, majorType([]byte{b}))
	}
}

func decodeBytes(s *Stream) (*DecodedBytes, error) {
	if s.isIndefBytes() {
		return decodeIndefBytes(s)
	} else {
		return decodeDefBytes(s)
	}
}

func decodeConstr(s *Stream) (*DecodedConstr, error) {
	tag, err := decodeConstrTag(s)
	if err != nil {
		return nil, err
	}

	fields, err := decodeList(s)
	if err != nil {
		return nil, err
	}

	return &DecodedConstr{tag, fields}, nil
}

func decodeConstrTag(s *Stream) (int, error) {
	_, n, err := decodeDefHead(s)
	if err != nil {
		return 0, err
	}

	if (n.Uint64() < 102) {
		return 0, errors.New("unexpected constr tag")
	} else if (n.Uint64() == 102) {
		mCheck, nCheck, err := decodeDefHead(s)
		if err != nil {
			return 0, err
		} else if mCheck != 4 {
			return 0, errors.New("unexpected")
		} else if nCheck.Uint64() != 2 {
			return 0, errors.New("unexpected")
		}

		di, err := decodeInt(s)
		if err != nil {
			return 0, err
		}

		return int(di.Value.Uint64()), nil
	} else if n.Uint64() < 121 {
		return 0, errors.New("unexpected")
	} else if n.Uint64() <= 127 {
		return int(n.Uint64()) - 121, nil
	} else {
		return 0, errors.New("unhandled constr tag")
	}
}

func encodeConstrTag(tag int) []byte {
    if (tag >= 0 && tag <= 6) {
        return encodeDefHead(6, big.NewInt(int64(121 + tag)))
    } else if (tag >= 7 && tag <= 127) {
        return encodeDefHead(6, big.NewInt(int64(1280 + tag - 7)))
    } else {
		bs := append(encodeDefHead(6, big.NewInt(102)), encodeDefHead(4, big.NewInt(2))...)
		bs = append(bs, encodeInt(big.NewInt(int64(tag)))...)
		
        return bs
    }
}

func (d *DecodedConstr) Cbor() []byte {
	bs := encodeConstrTag(d.Tag)

	bs = append(bs, d.Fields.Cbor()...)

	return bs
}

func decodeIntBigEndian(bs []byte) (*big.Int, error) {
	if len(bs) == 0 {
		return nil, errors.New("empty bytes")
	}

	var (
		a big.Int
		b_ big.Int
		c big.Int
	)

	p := big.NewInt(1)
    total := big.NewInt(0)

    for i := len(bs) - 1; i >= 0; i-- {
        b := bs[i]

		(&a).Mul(big.NewInt(int64(b)), p)
		(&b_).Add(total, &a)
		total = &b_

		(&c).Mul(p, big.NewInt(256))
		p = &c
    }

    return total, nil
}

func decodeDefHead(s *Stream) (int, *big.Int, error) {
	if s.isAtEnd() {
		return 0, nil, errors.New("empty cbor head")
	}

	first, err := s.shiftOne()
	if err != nil {
		return 0, nil, err
	}

	m, n0 := decodeFirstHeadByte(first)

	if n0 <= 23 {
		return m, big.NewInt(int64(n0)), nil
	} else if n0 == 24 {
		nbs, err := s.shiftMany(1)
		if err != nil {
			return 0, nil, err
		}

		n, err := decodeIntBigEndian(nbs)
		return m, n, err
	} else if n0 == 25 {
		nbs, err := s.shiftMany(2)
		if err != nil {
			return 0, nil, err
		}
		n, err := decodeIntBigEndian(nbs)
		return m, n, err
	} else if n0 == 26 {
		nbs, err := s.shiftMany(4)
		if err != nil {
			return 0, nil, err
		}
		n, err := decodeIntBigEndian(nbs)
		return m, n, err
	} else if n0 == 27 {
		nbs, err := s.shiftMany(8)
		if err != nil {
			return 0, nil, err
		}
		n, err := decodeIntBigEndian(nbs)
		return m, n, err
	} else {
		return 0, nil, errors.New("unexpected header")
	}
}

func decodeFirstHeadByte(b0 byte) (int, int) {
	m := b0/32
	n0 := b0%32

	return int(m), int(n0)
}

func decodeBool(s *Stream) (*DecodedBool, error) {
	b, err := s.shiftOne()
	if err != nil {
		return nil, err
	}

	if b == 245 {
		return &DecodedBool{true}, nil
	} else if b == 244 {
		return &DecodedBool{false}, nil
	} else {
		return nil, errors.New("invalid bool byte")
	}
}

func (d *DecodedBool) Cbor() []byte {
	if d.Value {
		return []byte{245}
	} else {
		return []byte{244}
	}
}

func decodeIndefBytes(s *Stream) (*DecodedBytes, error) {
	res := make([]byte, 0)

	b, err := s.peekOne()
	if err != nil {
		return nil, err
	}

	for b != 255 {
		_, n, err := decodeDefHead(s)
		if err != nil {
			return nil, err
		}

		chunk, err := s.shiftMany(int(n.Uint64()))
		if err != nil {
			return nil, err
		}

		res = append(res, chunk...)

		b, err = s.peekOne()
		if err != nil {
			return nil, err
		}
	}

	b, err = s.shiftOne()
	if err != nil {
		return nil, err
	} else if b != 255 {
		return nil, errors.New("invalid indef bytes termination byte")
	}

	return &DecodedBytes{
		Type: "indef",
		Bytes: res,
	}, nil
}

func decodeDefBytes(s *Stream) (*DecodedBytes, error) {
	_, n, err := decodeDefHead(s)
	if err != nil {
		return nil, err
	}

	bs, err := s.shiftMany(int(n.Uint64()))
	if err != nil {
		return nil, err
	}

	return &DecodedBytes{
		Type: "def",
		Bytes: bs,
	}, nil
}

func (d *DecodedBytes) Cbor() []byte {
	if d.Type == "indef" {
		return encodeIndefBytes(d.Bytes)
	} else if d.Type == "def" {
		return encodeDefBytes(d.Bytes)
	} else {
		panic("unhandled DecodedBytes.Type")
	}
}

func decodeEnvelope(s *Stream) (*DecodedEnvelope, error) {
	tag, err := decodeTag(s)
	if err != nil {
		return nil, err
	}

	bs, err := decodeBytes(s)
	if err != nil {
		return nil, err
	}

	s_, err := NewStream(bs.Bytes)
	if err != nil {
		return nil, err
	}

	inner, err := decode(s_)
	if err != nil {
		return nil, err
	}

	return &DecodedEnvelope{tag, bs.Type, inner}, nil
}

func (d *DecodedEnvelope) Cbor() []byte {
	inner := d.Inner.Cbor()
	bs := EncodeTag(d.Tag)

	if d.Type == "indef" {
		return append(bs, encodeIndefBytes(inner)...)
	} else if d.Type == "def" {
		return append(bs, encodeDefBytes(inner)...)
	} else {
		panic("unhandled envelope type")
	}
}

func decodeInt(s *Stream) (*DecodedInt, error) {
	m, n, err := decodeDefHead(s)
	if err != nil {
		return nil, err
	}

	if m == 0 {
		return &DecodedInt{n}, nil
	} else if m == 1 {
		return &DecodedInt{negMinusOne(n)}, nil
	} else if m == 6 {
		if n.Uint64() == 2 {
			bs, err := decodeBytes(s)
			if err != nil {
				return nil, err
			}

			nn, err := decodeIntBigEndian(bs.Bytes)
			if err != nil {
				return nil, err
			}

			return &DecodedInt{nn}, nil
		} else if n.Uint64() == 3 {
			bs, err := decodeBytes(s)
			if err != nil {
				return nil, err
			}

			nn, err := decodeIntBigEndian(bs.Bytes)
			if err != nil {
				return nil, err
			}

			return &DecodedInt{negMinusOne(nn)}, nil
		} else {
			return nil, errors.New("unexpected tag n")
		}
	} else {
		return nil, errors.New("unexpected tag m")
	}
}

func (d *DecodedInt) Cbor() []byte {
	return encodeInt(d.Value)
}

func decodeIndefList(s *Stream, fn func(s *Stream) error) error {
	s.shiftOne()

	b, err := s.peekOne()
	if err != nil {
		return err
	}

	for b != 255 {
		if err := fn(s); err != nil {
			return err
		}

		b, err = s.peekOne()
		if err != nil {
			return err
		}
	}

	b, err = s.shiftOne()
	if err != nil {
		return err
	} else if b != 255 {
		return errors.New("unexpected termination byte")
	}

	return nil
}

func decodeDefList(s *Stream, fn func(s *Stream) error) error {
	m, n, err := decodeDefHead(s)
	if err != nil {
		return err
	}

	if m != 4 {
		return errors.New("unexpected list head")
	}

	for i := 0; i < int(n.Uint64()); i++ {
		if err := fn(s); err != nil {
			return err
		}
	}

	return nil
}

func decodeList(s *Stream) (*DecodedList, error) {
	items := make([]Decoded, 0)
	var t string

	if s.isIndefList() {
		err := decodeIndefList(s, func (s *Stream) error {
			item, err := decode(s)
			if err != nil {
				return err
			}

			items = append(items, item)
			return nil
		})

		if err != nil {
			return nil, err
		}

		t = "indef"
	} else {
		err := decodeDefList(s, func (s *Stream) error {
			item, err := decode(s)
			if err != nil {
				return err
			}

			items = append(items, item)
			return nil
		})

		if err != nil {
			return nil, err
		}

		t = "def"
	}

	return &DecodedList{Type: t, Items: items}, nil
}

func (d *DecodedList) Cbor() []byte {
	items := make([][]byte, 0)
	for _, item := range d.Items {
		items = append(items, item.Cbor())
	}

	if d.Type == "indef" {
		return EncodeIndefList(items)	
	} else if d.Type == "def" {
		return EncodeDefList(items)
	} else if d.Type == "set" {
		bs := EncodeTag(258)
		return append(bs, EncodeDefList(items)...)
	} else {
		panic("unhandled list type")
	}
}

func decodeIndefMap(s *Stream, fnKey func(s *Stream) error, fnValue func(s *Stream) error) error {
	b, err := s.peekOne()
	if err != nil {
		return err
	}

	for b != 255 {
		if err := fnKey(s); err != nil {
			return err
		}

		if err := fnValue(s); err != nil {
			return err
		}

		b, err = s.peekOne()
		if err != nil {
			return err
		}
	}

	b, err = s.shiftOne()
	if err != nil {
		return err
	} else if b != 255 {
		return errors.New("unexpected")
	}

	return nil
}

func decodeDefMap(s *Stream, fnKey func (s *Stream) error, fnValue func (s *Stream) error) error {
	m, n, err := decodeDefHead(s)
	if err != nil {
		return err
	}

	if m != 5 {
		return errors.New("invalid def map head")
	}

	for i := 0; i < int(n.Uint64()); i++ {
		if err := fnKey(s); err != nil {
			return err
		}

		if err := fnValue(s); err != nil {
			return err
		}
    }

	return nil
}

func decodeMap(s *Stream) (*DecodedMap, error) {
	keys := make([]Decoded, 0)
	values := make([]Decoded, 0)

	var t string
	if s.isIndefMap() {
		s.shiftOne()

		err := decodeIndefMap(s, func (s *Stream) error {
			k, err := decode(s)
			if err != nil {
				return err
			}

			keys = append(keys, k)
			return nil
		}, func (s *Stream) error {
			v, err := decode(s)
			if err != nil {
				return err
			}

			values = append(values, v)

			return nil
		})

		if err != nil {
			return nil, err
		}

		t = "indef"
	} else {
		err := decodeDefMap(s, func (s *Stream) error {
			k, err := decode(s)
			if err != nil {
				return err
			}

			keys = append(keys, k)
			return nil
		}, func (s *Stream) error {
			v, err := decode(s)
			if err != nil {
				return err
			}

			values = append(values, v)

			return nil
		})

		if err != nil {
			return nil, err
		}

		t = "def"
	}

	if len(keys) != len(values) {
		return nil, errors.New("unexpected")
	}

	pairs := make([]DecodedPair, 0)
	for i, k := range keys {
		pairs = append(pairs, DecodedPair{
			Key: k,
			Value: values[i], 
		})
	}

	return &DecodedMap{Type: t, Pairs: pairs}, nil
}

func (d *DecodedMap) Cbor() []byte {
	pairs := make([]EncodedPair, 0)

	for _, pair := range d.Pairs {
		pairs = append(pairs, EncodedPair{
			Key: pair.Key.Cbor(),
			Value: pair.Value.Cbor(),
		})
	}

	if d.Type == "indef" {
		return EncodeIndefMap(pairs)
	} else if d.Type == "def" {
		return EncodeDefMap(pairs)
	} else {
		panic("unhandled map type")
	}
}

func decodeNull(s *Stream) (*DecodedNull, error) {
	b, err := s.shiftOne()
	if err != nil {
		return nil, err
	} else if b != 246 {
		return nil, errors.New("unexpect null byte")
	}

	return &DecodedNull{}, nil
}

func (d *DecodedNull) Cbor() []byte {
	return []byte{246}
}

func decodeSet(s *Stream) (*DecodedList, error) {
	tag, err := decodeTag(s)
	if err != nil {
		return nil, err
	}

	if tag != 258 {
		return nil, errors.New("unexpected set tag")
	}

	items := make([]Decoded, 0)

	err = decodeDefList(s, func (s *Stream) error {
		item, err := decode(s)
		if err != nil {
			return err
		}

		items = append(items, item)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &DecodedList{Type: "set", Items: items}, nil
}

func decodeString(s *Stream) (*DecodedString, error) {
	if s.isDefList() {
		res := ""

		err := decodeDefList(s, func (s *Stream)  error {
			s_, err := decodeStringInternal(s)
			if err != nil {
				return err
			}
			res = res + s_
			return nil
		})

		if err != nil {
			return nil, err
		}

		return &DecodedString{
			Type: "list",
			Value: res,
		}, nil
	} else {
		str, err := decodeStringInternal(s)
		if err != nil {
			return nil, err
		}

		return &DecodedString{
			Type: "single",
			Value: str,
		}, nil
	}
}

func (s *DecodedString) Cbor() []byte {
	panic("not yet implemented")
}

func decodeStringInternal(s *Stream) (string, error) {
	m, n, err := decodeDefHead(s)
	if err != nil {
		return  "", err
	}

	if (m != 3) {
		return "", errors.New("unexpected tag m")
	}

	bs, err := s.shiftMany(int(n.Uint64()))
	if err != nil {
		return "", err
	}

	if !utf8.Valid(bs) {
		return "", errors.New("invalid utf8")
	}

	return string(bs), nil
}

func decodeTag(s *Stream) (int, error) {
	m, n, err := decodeDefHead(s)
	if err != nil {
		return 0, err
	}

	if m != 6 {
		return 0, errors.New("invalid tag head")
	}

	return int(n.Uint64()), nil
}

func (s *Stream) isAtEnd() bool {
	return s.pos >= len(s.cbor)
}

func (s *Stream) isBool() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return b == 244 || b == 245
}

func (s *Stream) isBytes() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return majorType([]byte{b}) == 2
}

func (s *Stream) isIndefBytes() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return int(b) == 2*32 + 31
}

func (s *Stream) copy() *Stream {
	return &Stream{cbor: s.cbor, pos: s.pos}
}

func (s *Stream) isConstr() bool {
	m, n, err := decodeDefHead(s.copy())
	if err != nil {
		return false
	}
	
	if m == 6 {
		n_ := n.Uint64()

		return n_ == 102 || (n_ >= 121 && n_ <= 127) || (n_ >= 1280 && n_ <= 1400)
	} else {
		return false
	}
}

func (s *Stream) isInt() bool {
	first, err := s.peekOne() 
	if err != nil {
		return false
	}

	m, n0 := decodeFirstHeadByte(first)

	if m == 0 || m == 1 {
		return true
	} else if m == 6 {
		return n0 == 2 || n0 == 3
	} else {
		return false
	}
}

func (s *Stream) isDefList() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return majorType([]byte{b}) == 4 && b != 4*32+31
}

func (s *Stream) isIndefList() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return 4*32+31 == b
}

func (s *Stream) isList() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return majorType([]byte{b}) == 4
}

func (s *Stream) isMap() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return majorType([]byte{b}) == 5
}

func (s *Stream) isIndefMap() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return 5*32+31 == b
}

func (s *Stream) isNull() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return b == 246
}

func (s *Stream) isSet() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	m, n0 := decodeFirstHeadByte(b)
	if m == 6 && n0 == 25 {
		nbs, err := s.peekMany(3)
		if err != nil {
			return false
		}

		n, err := decodeIntBigEndian(nbs[1:])
		if err != nil {
			return false
		}

		return n.Uint64() == 258
	} else {
		return false
	}
}

func (s *Stream) isString() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return majorType([]byte{b}) == 3
}

func (s *Stream) isTag() bool {
	b, err := s.peekOne()
	if err != nil {
		return false
	}

	return majorType([]byte{b}) == 6
}

func (s *Stream) peekOne() (byte, error) {
	if s.pos < len(s.cbor) {
		return s.cbor[s.pos], nil
	} else {
		return 0, errors.New("at end")
	}
}

func (s *Stream) peekMany(n int) ([]byte, error) {
	if n < 0 {
		return nil, errors.New("negative n")
	}

	if s.pos + n <= len(s.cbor) {
		res := s.cbor[s.pos:s.pos+n]
		return res, nil
	} else {
		return nil, errors.New("at end")
	}
}

func (s *Stream) shiftOne() (byte, error) {
	if s.pos < len(s.cbor) {
		b := s.cbor[s.pos]
		s.pos += 1
		return b, nil
	} else {
		return 0, errors.New("at end")
	}
}

func (s *Stream) shiftMany(n int) ([]byte, error) {
	if n < 0 {
		return nil, errors.New("negative n")
	}

	if s.pos + n <= len(s.cbor) {
		res := s.cbor[s.pos:s.pos+n]
		s.pos += n
		return res, nil
	} else {
		return nil, errors.New("at end")
	}
}
