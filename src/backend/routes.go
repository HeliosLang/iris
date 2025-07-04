package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/blinklabs-io/gouroboros/cbor"
	"github.com/blinklabs-io/gouroboros/ledger"
	"github.com/blinklabs-io/gouroboros/ledger/common"
)

type Handler struct {
	config      *Config
	cli         *CardanoCLI
	db          *DB
	store       *Store
	paramsCache *ParametersCache
	mempool     *Mempool
	selector    *CoinSelector
	mu          sync.RWMutex // top-level RW Mutex. All read queries should call RLock, and all write queries should call Lock
}

type ParametersCache struct {
	ttl    time.Time
	params []byte
	mu     sync.RWMutex
}

type URLHelper struct {
	url *url.URL
	pos int
}

type SelectRequest struct {
	Lovelace    string `json:"lovelace"`
	Asset       string `json:"asset"`
	MinQuantity string `json:"minQuantity"`
	Algorithm   string `json:"algorithm"`
}

func NewHandler(cfg *Config) (*Handler, error) {
	cli := NewCardanoCLI(cfg.NetworkName)

	db, err := NewDB(cfg.NetworkName)
	if err != nil {
		return nil, err
	}

	// this might take a while
	store, err := LoadStore(filepath.Join("/var/cache/cardano-node", cfg.NetworkName))
	if err != nil {
		return nil, err
	}

	handler := &Handler{
		cfg,
		cli,
		db,
		store,
		&ParametersCache{},
		NewMempool(db),
		NewCoinSelector(),
		sync.RWMutex{},
	}

	go func() {
		for {
			time.Sleep(5 * time.Second)

			tip, err := handler.cli.Tip()
			if err == nil && strings.HasPrefix(tip.SyncProgress, "100") {
				handler.store.NotifyTip(tip.Hash)
			}
		}
	}()

	// wait 2 minutes to create the indices that speed up queries a lot
	go func() {
		for {
			time.Sleep(120 * time.Second)

			if err := db.CreateIndices(); err != nil {
				log.Printf("failed to create indices, retrying later (%v)", err)
			} else {
				break
			}
		}
	}()

	return handler, nil
}

func NewURLHelper(url *url.URL) URLHelper {
	return URLHelper{url, 0}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// allow cross-origin requests from any domain
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	if r.Method == http.MethodOptions {
		// preflight request, return empty response
		w.WriteHeader(http.StatusNoContent)
		return
	}

	cmp, url := NewURLHelper(r.URL).Pop()

	switch cmp {
	case "api":
		h.api(w, r, url)
	case "config":
		h.configRoutes(w, r, url)
	default:
		h.page(w, r)
	}
}

func (h *Handler) api(w http.ResponseWriter, r *http.Request, url URLHelper) {
	cmp, url := url.Pop()

	switch cmp {
	case "address":
		h.address(w, r, url)
	case "block":
		h.block(w, r, url)
	case "chain":
		h.chain(w, r, url)
	case "parameters":
		h.parameters(w, r)
	case "policy":
		h.policy(w, r, url)
	case "mempool":
		h.mempoolTxs(w, r)
	case "tx":
		h.tx(w, r, url)
	case "utxo":
		h.utxo(w, r, url)
	default:
		invalidEndpoint(w, r)
	}
}

func (h *Handler) configRoutes(w http.ResponseWriter, r *http.Request, url URLHelper) {
	cmp, _ := url.Pop()

	if r.Method != http.MethodGet {
		invalidMethod(w, r)
		return
	}

	switch cmp {
	case "wallet":
		h.configWallet(w, r)
	case "collateral":
		h.configCollateral(w, r)
	default:
		invalidEndpoint(w, r)
	}
}

func (h *Handler) configWallet(w http.ResponseWriter, _ *http.Request) {
	if h.config.Wallet == nil {
		http.Error(w, "wallet not configured", http.StatusNotFound)
		return
	}
	addr, err := firstEnterpriseAddress(h.config.Wallet, h.config.NetworkName)
	if err != nil {
		internalError(w, err)
		return
	}
	respondWithJSON(w, map[string]string{"address": addr})
}

func (h *Handler) configCollateral(w http.ResponseWriter, _ *http.Request) {
	if h.config.Collateral == "" {
		http.Error(w, "collateral not set", http.StatusNotFound)
		return
	}
	respondWithJSON(w, map[string]string{"collateral": h.config.Collateral})
}

func (h *Handler) validAddress(addr string) bool {
	if h.config.NetworkName == "mainnet" {
		if !strings.HasPrefix(addr, "addr1") {
			return false
		}
	} else {
		if !strings.HasPrefix(addr, "addr_test1") {
			return false
		}
	}

	return true
}

func (h *Handler) address(w http.ResponseWriter, r *http.Request, url URLHelper) {
	addr, url := url.Pop()
	if addr == "" {
		invalidEndpoint(w, r)
		return
	}

	if !h.validAddress(addr) {
		http.Error(w, "invalid address", http.StatusNotFound)
		return
	}

	cmp, url := url.Pop()

	switch cmp {
	case "utxos":
		switch r.Method {
		case http.MethodGet:
			h.addressUTXOs(w, r, addr, url)
		case http.MethodPost:
			h.selectUTXOs(w, r, addr, url)
		default:
			invalidMethod(w, r)
		}
	default:
		invalidEndpoint(w, r)
	}
}

// read query
func (h *Handler) addressUTXOs(w http.ResponseWriter, r *http.Request, addr string, url URLHelper) {
	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	asset := ""
	if vals, ok := r.URL.Query()["asset"]; ok {
		if len(vals) != 1 {
			http.Error(w, fmt.Sprintf("asset query parameter used %d times instead of once", len(vals)), http.StatusBadRequest)
			return
		}
		asset = vals[0]
	}

	obj, err := h.getAddressUTXOs(r.Context(), addr, asset)
	if err != nil {
		internalError(w, err)
		return
	}

	respondWithUTXOs(w, r, obj)
}

// write query
func (h *Handler) selectUTXOs(w http.ResponseWriter, r *http.Request, addr string, url URLHelper) {
	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	var req SelectRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("failed to decode request: %v", err), http.StatusBadRequest)
		return
	}

	utxos, err := h.getAddressUTXOs(r.Context(), addr, req.Asset)
	if err != nil {
		internalError(w, err)
		return
	}

	h.selector.mu.Lock()
	defer h.selector.mu.Unlock()
	h.selector.pruneExpired()

	filtered := make([]UTXO, 0, len(utxos))
	for _, u := range utxos {
		if h.selector.isLocked(utxoKey(u)) {
			continue
		}
		filtered = append(filtered, u)
	}

	utxos = filtered

	sort.Slice(utxos, func(i, j int) bool {
		li, _ := new(big.Int).SetString(utxos[i].Lovelace, 10)
		lj, _ := new(big.Int).SetString(utxos[j].Lovelace, 10)
		if strings.EqualFold(req.Algorithm, "largest") || strings.EqualFold(req.Algorithm, "largest-first") {
			return li.Cmp(lj) > 0
		}
		return li.Cmp(lj) < 0
	})

	needLov, _ := new(big.Int).SetString(req.Lovelace, 10)
	gotLov := big.NewInt(0)
	needAsset, _ := new(big.Int).SetString(req.MinQuantity, 10)
	gotAsset := big.NewInt(0)

	selected := []UTXO{}
	for _, u := range utxos {
		lv, _ := new(big.Int).SetString(u.Lovelace, 10)
		gotLov.Add(gotLov, lv)

		if req.Asset != "" {
			for _, a := range u.Assets {
				if strings.EqualFold(a.Asset, req.Asset) {
					q, _ := new(big.Int).SetString(a.Quantity, 10)
					gotAsset.Add(gotAsset, q)
				}
			}
		}

		selected = append(selected, u)
		if gotLov.Cmp(needLov) >= 0 && (req.Asset == "" || gotAsset.Cmp(needAsset) >= 0) {
			break
		}
	}

	if gotLov.Cmp(needLov) < 0 || (req.Asset != "" && gotAsset.Cmp(needAsset) < 0) {
		http.Error(w, "not enough UTXOs", http.StatusNotFound)
		return
	}

	for _, u := range selected {
		h.selector.lock(utxoKey(u), 10*time.Second)
	}

	respondWithUTXOs(w, r, selected)
}

// internal method used by addressUTXOs and selectUTXOs
func (h *Handler) getAddressUTXOs(ctx context.Context, addr string, asset string) ([]UTXO, error) {
	var (
		obj    []UTXO
		err    error
		filter func(UTXO) bool
	)

	if asset != "" {
		obj, err = h.db.AddressUTXOsWithAsset(addr, asset, ctx)
		if err != nil {
			return nil, err
		}

		lower := strings.ToLower(asset)
		if lower != "lovelace" {
			filter = func(u UTXO) bool {
				if u.Address != addr {
					return false
				}
				for _, a := range u.Assets {
					if strings.EqualFold(a.Asset, asset) {
						return true
					}
				}
				return false
			}
		} else {
			filter = func(u UTXO) bool { return u.Address == addr && len(u.Assets) == 0 }
		}
	} else {
		obj, err = h.db.AddressUTXOs(addr, ctx)
		if err != nil {
			return nil, err
		}

		filter = func(u UTXO) bool { return u.Address == addr }
	}

	obj = h.mempool.Overlay(obj, filter)

	return obj, nil
}

func (h *Handler) block(w http.ResponseWriter, r *http.Request, url URLHelper) {
	blockID, url := url.Pop()

	if blockID == "" {
		invalidEndpoint(w, r)
		return
	}

	cmp, url := url.Pop()

	if cmp == "" {
		h.blockBytes(w, r, blockID)
	} else {
		switch cmp {
		case "tx":
			h.blockTx(w, r, url, blockID)
		default:
			invalidEndpoint(w, r)
		}
	}
}

func (h *Handler) blockBytes(w http.ResponseWriter, r *http.Request, blockID string) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	block, err := h.store.Block(blockID)
	if err != nil {
		internalError(w, err)
		return
	}

	if block == nil {
		http.Error(w, fmt.Sprintf("block %s not found", blockID), http.StatusNotFound)
		return
	}

	respondWithCBOR(w, r, block.Cbor())
}

// read query, but doesn't depend on recent write operations, so no need to lock
func (h *Handler) blockTx(w http.ResponseWriter, r *http.Request, url URLHelper, blockID string) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	txIndexStr, url := url.Pop()
	if txIndexStr == "" {
		invalidEndpoint(w, r)
		return
	}

	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	txIndex, err := strconv.ParseInt(txIndexStr, 10, 32)
	if err != nil {
		invalidEndpoint(w, r)
		return
	}

	if txIndex < 0 {
		http.Error(w, fmt.Sprintf("invalid tx index %d\n", txIndex), http.StatusNotFound)
		return
	}

	tx, err := h.store.BlockTx(blockID, int(txIndex))
	if err != nil {
		internalError(w, err)
		return
	}

	if tx == nil {
		http.Error(w, fmt.Sprintf("transaction %d of block %s not found\n", txIndex, blockID), http.StatusNotFound)
		return
	}

	respondWithCBOR(w, r, tx.Cbor())
}

func (h *Handler) chain(w http.ResponseWriter, r *http.Request, url URLHelper) {
	cmp, url := url.Pop()
	if cmp == "" {
		invalidEndpoint(w, r)
		return
	}

	switch cmp {
	case "tip":
		h.chainTip(w, r, url)
	default:
		invalidEndpoint(w, r)
	}
}

// read query, but doesnt depend on recent write operations, so no need to lock
func (h *Handler) chainTip(w http.ResponseWriter, r *http.Request, url URLHelper) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	tip, err := h.cli.Tip()
	if err != nil {
		internalError(w, err)
		return
	}

	respondWithJSON(w, tip)
}

type HeliosNetworkParams struct {
	CollateralUTXO       string  `json:"collateralUTXO,omitempty"`
	CollateralPercentage int     `json:"collateralPercentage"`
	CostModelParamsV1    []int   `json:"costModelParamsV1"`
	CostModelParamsV2    []int   `json:"costModelParamsV2"`
	CostModelParamsV3    []int   `json:"costModelParamsV3"`
	ExCPUFeePerUnit      float64 `json:"exCpuFeePerUnit"`
	ExMemFeePerUnit      float64 `json:"exMemFeePerUnit"`
	MaxCollateralInputs  int     `json:"maxCollateralInputs"`
	MaxTxExCPU           int64   `json:"maxTxExCpu"`
	MaxTxExMem           int64   `json:"maxTxExMem"`
	MaxTxSize            int     `json:"maxTxSize"`
	RefScriptsFeePerByte int     `json:"refScriptsFeePerByte"`
	RefTipSlot           int64   `json:"refTipSlot"`
	RefTipTime           int64   `json:"refTipTime"`
	SecondsPerSlot       int     `json:"secondsPerSlot"`
	StakeAddrDeposit     int64   `json:"stakeAddrDeposit"`
	TxFeeFixed           int     `json:"txFeeFixed"`
	TxFeePerByte         int     `json:"txFeePerByte"`
	UTXODepositPerByte   int     `json:"utxoDepositPerByte"`
}

// read query, but doesn't depend on recent write operations, so no need to lock the global mutex
func (h *Handler) parameters(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	h.paramsCache.mu.RLock()
	cachedParams := h.paramsCache.params
	ttl := h.paramsCache.ttl
	h.paramsCache.mu.RUnlock()

	if cachedParams != nil && time.Now().Before(ttl) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(cachedParams); err != nil {
			http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
		}
		return
	}

	h.paramsCache.mu.Lock()
	defer h.paramsCache.mu.Unlock()

	heliosParams, err := h.cli.DeriveParameters()
	if err != nil {
		internalError(w, err)
		return
	}

	if h.config.Collateral != "" && h.config.Wallet != nil && len(h.config.Collateral) > 64 {
		txID := h.config.Collateral[:64]
		outputIndexStr := h.config.Collateral[64:]
		if idx, err := strconv.Atoi(outputIndexStr); err == nil {
			utxo, err := h.db.UTXO(txID, idx, r.Context())
			if err == nil && utxo.ConsumedBy == "" {
				addr, err := firstEnterpriseAddress(h.config.Wallet, h.config.NetworkName)
				if err == nil && utxo.Address == addr {
					heliosParams.CollateralUTXO = h.config.Collateral
				}
			}
		}
	}

	tip, err := h.cli.Tip()
	if err != nil {
		internalError(w, err)
		return
	}

	ttlTime := time.Now().Add(time.Duration(tip.SlotsToEpochEnd) * time.Second)

	content, err := json.Marshal(heliosParams)
	if err != nil {
		internalError(w, err)
		return
	}

	encodedParams := []byte(content)
	h.paramsCache.params = encodedParams
	h.paramsCache.ttl = ttlTime

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(encodedParams); err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
	}
}

func (h *Handler) policy(w http.ResponseWriter, r *http.Request, url URLHelper) {
	policyHex, url := url.Pop()
	if policyHex == "" {
		invalidEndpoint(w, r)
		return
	}

	policy, err := hex.DecodeString(policyHex)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid policy: %v", err), http.StatusNotFound)
		return
	}

	if len(policy) != 28 {
		http.Error(w, "invalid policy length", http.StatusNotFound)
		return
	}

	cmp, url := url.Pop()
	if cmp == "" {
		invalidEndpoint(w, r)
		return
	}

	switch cmp {
	case "asset":
		h.policyAsset(w, r, url, policy)
	case "assets":
		h.policyAssets(w, r, policy)
	default:
		invalidEndpoint(w, r)
	}
}

func (h *Handler) policyAsset(w http.ResponseWriter, r *http.Request, url URLHelper, policy []byte) {
	assetNameHex, url := url.Pop()
	// empty assetName is allowed

	assetName, err := hex.DecodeString(assetNameHex)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid asset name: %v", err), http.StatusNotFound)
		return
	}

	if len(assetName) > 32 {
		http.Error(w, "asset name too big", http.StatusNotFound)
		return
	}

	fullAssetName := hex.EncodeToString(policy) + hex.EncodeToString(assetName)

	cmp, url := url.Pop()
	switch cmp {
	case "addresses":
		h.policyAssetAddresses(w, r, fullAssetName, url)
	default:
		invalidEndpoint(w, r)
	}
}

func (h *Handler) policyAssetAddresses(w http.ResponseWriter, r *http.Request, asset string, url URLHelper) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	// TODO: overlay recent TXs
	addresses, err := h.db.AssetAddresses(asset, r.Context())
	if err != nil {
		internalError(w, err)
		return
	}

	respondWithJSON(w, addresses)
}

func (h *Handler) policyAssets(w http.ResponseWriter, r *http.Request, policy []byte) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	// TODO: overlay recent txs
	assets, err := h.db.PolicyAssets(hex.EncodeToString(policy), r.Context())
	if err != nil {
		internalError(w, err)
		return
	}

	respondWithJSON(w, assets)
}

func (h *Handler) tx(w http.ResponseWriter, r *http.Request, url URLHelper) {
	txID, url := url.Pop()
	if txID == "" {
		if r.Method == "POST" {
			h.submitTx(w, r)
			return
		} else {
			invalidEndpoint(w, r)
			return
		}
	}

	cmp, url := url.Pop()

	switch cmp {
	case "":
		h.txContent(w, r, txID)
	case "block":
		h.txBlockInfo(w, r, txID)
	case "output":
		h.txOutput(w, r, url, txID)
	default:
		invalidEndpoint(w, r)
	}
}

type TxEnvelope struct {
	CBORHex     string `json:"cborHex"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

type SubmitTxResponse struct {
	TxID            string   `json:"txID"`
	Message         string   `json:"message,omitempty"`
	ExtraSignatures []string `json:"extraSignatures,omitempty"`
}

// write query
func (h *Handler) submitTx(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if r.Method != "POST" {
		invalidMethod(w, r)
		return
	}

	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()

	if err != nil {
		internalError(w, err)
		return
	}

	var txBytes []byte

	switch r.Header.Get("Content-Type") {

	case "application/cbor":
		txBytes = body
	case "application/json":
		if !utf8.Valid(body) {
			http.Error(w, "request body isn't valid utf-8", http.StatusBadRequest)
			return
		}

		var structuredBody TxEnvelope

		if err := json.Unmarshal(body, &structuredBody); err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}

		txBytes, err = hex.DecodeString(string(structuredBody.CBORHex))
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}
	case "text/plain":
	default:
		if !utf8.Valid(body) {
			http.Error(w, "request body isn't valid utf-8", http.StatusBadRequest)
			return
		}

		txBytes, err = hex.DecodeString(string(body))
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
			return
		}
	}

	if len(txBytes) > 17000 {
		http.Error(w, "tx too big", http.StatusBadRequest)
		return
	}

	tx, err := decodeTx(txBytes)
	if err != nil {
		internalError(w, err)
		return
	}

	tx, extraSignature, err := h.signCollateral(tx)
	if err != nil {
		internalError(w, err)
		return
	}

	// save the tx JSON representation to a temporary file
	txEnv := TxEnvelope{
		hex.EncodeToString(tx.Cbor()),
		"Tx ConwayEra", // TODO: automatic updating during hardforks
		"Submitted through the Helios gateway",
	}

	content, err := json.Marshal(txEnv)
	if err != nil {
		internalError(w, err)
		return
	}

	txPath := getTxTmpPath(tx)
	if err := os.WriteFile(txPath, content, 0444); err != nil {
		internalError(w, err)
		return
	}

	message, err := h.submitTxWithRetries(txPath)
	if err != nil {
		internalError(w, err)
		return
	}

	// save to mempool
	ttlTime := time.Now().Add(10 * time.Minute)
	if ttl := tx.TTL(); ttl != 0 {
		if t, err := h.cli.ConvertSlotToTime(ttl); err == nil {
			if t.Before(ttlTime) {
				ttlTime = t
			}
		}
	}

	h.mempool.AddTx(tx, ttlTime)

	txID := tx.Hash()

	response := SubmitTxResponse{
		TxID:            hex.EncodeToString(txID[:]),
		Message:         message,
		ExtraSignatures: []string{},
	}

	if extraSignature != "" {
		response.ExtraSignatures = append(response.ExtraSignatures, extraSignature)
	}

	respondWithJSON(w, response)
}

func (h *Handler) signCollateral(tx ledger.Transaction) (ledger.Transaction, string, error) {
	if h.config.Wallet == nil {
		return tx, "", nil
	}

	if h.config.Collateral == "" {
		return tx, "", nil
	}

	if !isBabbageOrConwayTx(tx) {
		return tx, "", nil
	}

	if len(tx.Collateral()) != 1 {
		return tx, "", nil
	}

	if tx.CollateralReturn() != nil {
		return tx, "", nil
	}

	input := tx.Collateral()[0]
	inputID := fmt.Sprintf("%s%d", input.Id().String(), input.Index())

	if inputID != h.config.Collateral {
		return tx, "", nil
	}

	key, err := firstEnterprisePrvKey(h.config.Wallet)
	if err != nil {
		fmt.Printf("unable to generate collateral wallet key (%v)\n", err)
		return tx, "", nil
	}

	hash := tx.Hash().Bytes()
	witness := common.VkeyWitness{
		Vkey:      key.PubKey(),
		Signature: key.Sign(hash),
	}

	witnessBytes, err := cbor.Encode(witness)
	if err != nil {
		return nil, "", err
	}

	witness_, err := Decode(witnessBytes)
	if err != nil {
		return nil, "", err
	}

	d, err := Decode(tx.Cbor())
	if err != nil {
		return nil, "", err
	}

	txList, ok := d.(*DecodedList)
	if !ok || len(txList.Items) != 4 {
		fmt.Println("decoded tx isn't a tuple with 4 entries")
		return tx, "", nil
	}

	txWitnessesMap, ok := (txList.Items[1]).(*DecodedMap)
	if !ok || len(txWitnessesMap.Pairs) < 1 {
		fmt.Println("decoded tx witnesses isn't a map with at least one entry")
		return tx, "", nil
	}

	signaturesI := -1
	for i, pair := range txWitnessesMap.Pairs {
		key, ok := pair.Key.(*DecodedInt)
		if ok && key.Value.Uint64() == 0 {
			signaturesI = i
			break
		}
	}

	if signaturesI == -1 {
		// TODO: what to do if no signatures are needed because all inputs are from public smart contract?
		// -> add to end
		fmt.Println("signatures entry not found in map")
		txWitnessesMap.Pairs = append(txWitnessesMap.Pairs, DecodedPair{
			Key: &DecodedInt{big.NewInt(0)},
			Value: &DecodedList{
				Type:  "set", // TODO: we might need to detect this from other entries
				Items: []Decoded{witness_},
			},
		})
	} else {
		pair := txWitnessesMap.Pairs[signaturesI]
		signatures, ok := (pair.Value).(*DecodedList)
		if !ok {
			fmt.Println("signatures entry isn't a list")
			return tx, "", nil
		}

		signatures.Items = append(signatures.Items, witness_)
	}

	updatedTxBytes := d.Cbor()
	tx, err = decodeTx(updatedTxBytes)
	if err != nil {
		return nil, "", fmt.Errorf("failed to update tx bytes with signature for collateral (%v)", err)
	}

	return tx, hex.EncodeToString(witnessBytes), nil
}

func isBabbageOrConwayTx(tx ledger.Transaction) bool {
	switch tx.(type) {
	case *ledger.BabbageTransaction:
		return true
	case *ledger.ConwayTransaction:
		return true
	default:
		return false
	}
}

// retries twice (first time after 5 seconds delay, second time after 10 seconds after first retry)
func (h *Handler) submitTxWithRetries(txPath string) (string, error) {
	var (
		result string
		err    error
	)

	for attempt := range 3 {
		result, err = h.cli.SubmitTx(txPath)
		if err == nil {
			return result, nil
		}

		parsedErr := ParseTxSubmitError(err.Error())

		if len(parsedErr.MissingInputs) == 0 {
			return "", err
		}

		time.Sleep(time.Second * time.Duration((attempt+1)*5))
	}

	return result, err
}

func (h *Handler) submitTxWithDeps(txPath string) (string, error) {
	result, err := h.cli.SubmitTx(txPath)
	if err == nil {
		return result, nil
	}

	parsedErr := ParseTxSubmitError(err.Error())

	if len(parsedErr.MissingInputs) == 0 {
		return "", err
	}

	done := make(map[string]struct{})

	for _, missingInput := range parsedErr.MissingInputs {
		// get from disc instead of mempool, because disc maintains much more information
		txID := missingInput.TxID

		if _, ok := done[txID]; ok {
			continue
		}

		done[txID] = struct{}{}

		p := getTmpPath(missingInput.TxID)

		if _, err := os.Stat(p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				fmt.Printf("tx %s not found, skipping\n", p)
			} else {
				fmt.Printf("problem reading tx %s, skipping (%v)\n", p, err)
			}
			continue
			// path/to/whatever does not exist
		}

		fmt.Printf("resubmitting %s\n", missingInput.TxID)

		// anything in the mempool should also have its content written to its tmp path
		_, err := h.submitTxWithDeps(p)
		if err != nil {
			fmt.Printf("failed to resubmit %s: %v\n", missingInput.TxID, err)
		}
	}

	// retry, with all mempool txs recently submitted
	return h.cli.SubmitTx(txPath)
}

func getTmpPath(txID string) string {
	return "/tmp/" + txID
}

func getTxTmpPath(tx ledger.Transaction) string {
	return getTmpPath(tx.Hash().String())
}

func decodeTx(txBytes []byte) (ledger.Transaction, error) {
	txType, err := ledger.DetermineTransactionType(txBytes)
	if err != nil {
		return nil, err
	}

	return ledger.NewTransactionFromCbor(txType, txBytes)
}

// read query
func (h *Handler) txContent(w http.ResponseWriter, r *http.Request, txID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	tx := h.mempool.GetTx(txID)
	if tx != nil {
		respondWithCBOR(w, r, tx.Cbor())
		return
	}

	// TODO: fetch block id and index efficiently using a specific sql query
	txBlockInfo, err := h.db.TxBlockInfo(txID, r.Context())
	if err != nil {
		// TODO: return and detect NotFound errors
		http.Error(w, fmt.Sprintf("failed to get tx %s: %v", txID, err), http.StatusNotFound)
		return
	}

	tx, err = h.store.BlockTx(txBlockInfo.BlockID, int(txBlockInfo.Index))
	if err != nil {
		internalError(w, err)
		return
	}

	if tx == nil {
		http.Error(w, fmt.Sprintf("transaction %s not found", txID), http.StatusNotFound)
		return
	}

	respondWithCBOR(w, r, tx.Cbor())
}

// read query, but doesn't depend on recent write operations, so don't lock mutex
func (h *Handler) txBlockInfo(w http.ResponseWriter, r *http.Request, txID string) {
	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	txBlockInfo, err := h.db.TxBlockInfo(txID, r.Context())
	if err != nil {
		// TODO: return and detect NotFound errors
		internalError(w, err)
		return
	}

	respondWithJSON(w, txBlockInfo)
}

// read query
func (h *Handler) txOutput(w http.ResponseWriter, r *http.Request, url URLHelper, txID string) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	indexStr, url := url.Pop()
	if indexStr == "" {
		invalidEndpoint(w, r)
		return
	}

	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	outputIndex, err := strconv.ParseUint(indexStr, 10, 32)
	if err != nil {
		invalidEndpoint(w, r)
		return
	}

	var cbor []byte

	if utxo, found := h.mempool.GetUTXO(txID, int(outputIndex)); found {
		cbor, err = EncodeUTXO(utxo)
		if err != nil {
			internalError(w, err)
			return
		}
	} else {
		// TODO: what about spent outputs?
		cbor, err = h.cli.UTXO(txID, int(outputIndex))
		if err != nil {
			internalError(w, err)
			return
		}

		if cbor == nil {
			http.Error(w, fmt.Sprintf("Tx output %s#%d not found", txID, outputIndex), http.StatusNotFound)
			return
		}
	}

	respondWithCBOR(w, r, cbor)
}

func (h *Handler) utxo(w http.ResponseWriter, r *http.Request, url URLHelper) {
	utxoID, url := url.Pop()
	if utxoID == "" {
		invalidEndpoint(w, r)
		return
	}

	txID := utxoID[0:64]
	if len(txID) != 64 {
		invalidEndpoint(w, r)
		return
	}

	outputIndexStr := utxoID[64:]
	if outputIndexStr == "" {
		invalidEndpoint(w, r)
		return
	}

	outputIndex, err := strconv.ParseInt(outputIndexStr, 10, 32)
	if err != nil {
		invalidEndpoint(w, r)
		return
	}

	cmp, url := url.Pop()

	switch cmp {
	case "":
		h.utxoContent(w, r, txID, int(outputIndex), url)
	default:
		invalidEndpoint(w, r)
	}
}

// read query
func (h *Handler) utxoContent(w http.ResponseWriter, r *http.Request, txID string, outputIndex int, url URLHelper) {
	if !url.Empty() {
		invalidEndpoint(w, r)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	var (
		utxo  UTXO
		found bool
		err   error
	)

	utxo, found = h.mempool.GetUTXO(txID, outputIndex)
	if !found {
		utxo, err = h.db.UTXO(txID, outputIndex, r.Context())
		if err != nil {
			http.Error(w, fmt.Sprintf("UTXO %s#%d not found (%v)", txID, outputIndex, err), http.StatusNotFound)
			return
		}
	}

	code := http.StatusOK
	if utxo.ConsumedBy != "" {
		w.Header().Set("Consumed-By", utxo.ConsumedBy)
		code = http.StatusConflict
	}

	if r.Header.Get("Accept") != "application/cbor" {
		respondWithJSONWithStatus(w, utxo, code)
	} else {
		cbor, err := EncodeUTXO(utxo)
		if err != nil {
			internalError(w, err)
			return
		}

		respondWithCBORWithStatus(w, r, cbor, code)
	}
}

// read query
func (h *Handler) mempoolTxs(w http.ResponseWriter, r *http.Request) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if r.Method != "GET" {
		invalidMethod(w, r)
		return
	}

	h.mempool.prune()

	hashes := h.mempool.Hashes()

	respondWithJSON(w, hashes)
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	is404 := false

	encodedContent, ok := embeddedFiles[path]
	if !ok {
		is404 = path != "" && path != "/"
		path = "/index.html"
		encodedContent, ok = embeddedFiles[path]
		if !ok {
			internalError(w, fmt.Errorf("static asset '%s' not available", path))
			return
		}
	}

	mimeType := deriveMimeTypeFromExt(path)

	w.Header().Set("Content-Type", mimeType)

	content, err := base64.RawStdEncoding.DecodeString(encodedContent)
	if err != nil {
		internalError(w, err)
		return
	}

	if is404 {
		w.WriteHeader(http.StatusNotFound)
	}

	if _, err = w.Write(content); err != nil {
		internalError(w, err)
		return
	}
}

func (url URLHelper) components() []string {
	// ignores the first and last slash
	return strings.Split(strings.Trim(url.url.Path, "/"), "/")
}

// returns an empty string if there is nothing else to pop
func (url URLHelper) Pop() (string, URLHelper) {
	cmps := url.components()

	if url.pos < len(cmps) {
		return cmps[url.pos], URLHelper{url.url, url.pos + 1}
	} else {
		return "", url
	}
}

func (url URLHelper) Empty() bool {
	return url.pos >= len(url.components())
}

func deriveMimeTypeFromExt(p string) string {
	ext := filepath.Ext(p)

	switch ext {
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	case ".ico":
		return "image/x-icon"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	case ".ttf":
		return "font/ttf"
	case ".eot":
		return "application/vnd.ms-fontobject"
	case ".otf":
		return "font/otf"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".ogg":
		return "audio/ogg"
	case ".mp3":
		return "audio/mpeg"
	case ".txt":
		return "text/plain"
	case ".xml":
		return "application/xml"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".wasm":
		return "application/wasm"
	default:
		return "application/octet-stream"
	}
}

func invalidEndpoint(w http.ResponseWriter, r *http.Request) {
	http.Error(w, fmt.Sprintf("invalid endpoint '%s'", r.URL.Path), http.StatusNotFound)
}

func invalidMethod(w http.ResponseWriter, r *http.Request) {
	http.Error(w, fmt.Sprintf("invalid method '%s' for endpoint '%s'", r.Method, r.URL.Path), http.StatusNotFound)
}

func internalError(w http.ResponseWriter, err error) {
	http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
}

type CBORJSONEnvelope struct {
	Hex string `json:"cborHex"`
}

func NewCBORJSONEnvelope(cbor []byte) CBORJSONEnvelope {
	return CBORJSONEnvelope{hex.EncodeToString(cbor)}
}

func respondWithCBOR(w http.ResponseWriter, r *http.Request, cbor []byte) {
	respondWithCBORWithStatus(w, r, cbor, http.StatusOK)
}

func respondWithCBORWithStatus(w http.ResponseWriter, r *http.Request, cbor []byte, statusCode int) {
	accept := r.Header.Get("Accept")

	switch accept {
	case "application/cbor":
		w.Header().Set("Content-Type", "application/cbor")

		w.WriteHeader(statusCode)

		if _, err := w.Write(cbor); err != nil {
			internalError(w, err)
		}
	case "application/json":
		respondWithJSONWithStatus(w, NewCBORJSONEnvelope(cbor), statusCode)
	default:
		w.Header().Set("Content-Type", "text/plain")

		w.WriteHeader(statusCode)

		if _, err := w.Write([]byte(hex.EncodeToString(cbor))); err != nil {
			internalError(w, err)
		}
	}
}

func respondWithJSON(w http.ResponseWriter, v any) {
	respondWithJSONWithStatus(w, v, http.StatusOK)
}

func respondWithJSONWithStatus(w http.ResponseWriter, v any, statusCode int) {
	w.Header().Set("Content-Type", "application/json")

	content, err := json.Marshal(v)
	if err != nil {
		internalError(w, err)
		return
	}

	w.WriteHeader(statusCode)

	if _, err := w.Write([]byte(content)); err != nil {
		http.Error(w, fmt.Sprintf("internal error: %v", err), http.StatusInternalServerError)
	}
}

func respondWithUTXOs(w http.ResponseWriter, r *http.Request, utxos []UTXO) {
	if r.Header.Get("Accept") != "application/cbor" {
		respondWithJSON(w, utxos)
		return
	}

	entries := make([][]byte, 0, len(utxos))
	for _, utxo := range utxos {
		encodedUTXO, err := EncodeUTXO(utxo)
		if err != nil {
			internalError(w, err)
			return
		}
		entries = append(entries, encodedUTXO)
	}

	cbor := EncodeList(entries)
	respondWithCBOR(w, r, cbor)
}
