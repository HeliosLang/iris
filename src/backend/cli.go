package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CardanoCLI struct {
	networkName string
}

func NewCardanoCLI(networkName string) *CardanoCLI {
	if networkName != "preprod" && networkName != "mainnet" {
		log.Fatalf("Unhandled network name %s", networkName)
		return nil
	}

	return &CardanoCLI{networkName}
}

// returns the list as CBOR bytes decoded from hex
func (c *CardanoCLI) AddressUTXOs(address string) ([]byte, error) {
	cborHex, err := c.invoke(
		"query", "utxo",
		"--address", address,
		"--output-cbor",
	)

	if err != nil {
		return nil, err
	}

	cbor, err := hex.DecodeString(strings.TrimSpace(cborHex))
	if err != nil {
		return nil, err
	}

	return cbor, nil
}

type CardanoCLIParameters struct {
	CollateralPercentage   int `json:"collateralPercentage"`
	CommitteeMaxTermLength int `json:"committeeMaxTermLength"`
	CommitteeMinSize       int `json:"committeeMinSize"`
	CostModels             struct {
		PlutusV1 []int `json:"PlutusV1"`
		PlutusV2 []int `json:"PlutusV2"`
		PlutusV3 []int `json:"PlutusV3"`
	} `json:"costModels"`
	DRepActivity         int   `json:"dRepActivity"`
	DRepDeposit          int64 `json:"dRepDeposit"`
	DRepVotingThresholds struct {
		CommitteeNoConfidence float64 `json:"committeeNoConfidence"`
		CommitteeNormal       float64 `json:"committeeNormal"`
		HardForkInitiation    float64 `json:"hardForkInitiation"`
		MotionNoConfidence    float64 `json:"motionNoConfidence"`
		PPEconomicGroup       float64 `json:"ppEconomicGroup"`
		PPGovGroup            float64 `json:"ppGovGroup"`
		PPTechnicalGroup      float64 `json:"ppTechnicalGroup"`
		TreasuryWithdrawal    float64 `json:"treasuryWithdrawal"`
		UpdateToConstitution  float64 `json:"updateToConstitution"`
	} `json:"dRepVotingThresholds"`
	ExecutionUnitPrices struct {
		PriceMemory float64 `json:"priceMemory"`
		PriceSteps  float64 `json:"priceSteps"`
	} `json:"executionUnitPrices"`
	GovActionDeposit       int64 `json:"govActionDeposit"`
	GovActionLifetime      int   `json:"govActionLifeTime"`
	MaxBlockBodySize       int   `json:"maxBlockBodySize"`
	MaxBlockExecutionUnits struct {
		Memory int64 `json:"memory"`
		Steps  int64 `json:"steps"`
	} `json:"maxBlockExecutionUnits"`
	MaxBlockHeaderSize  int `json:"maxBlockHeaderSize"`
	MaxCollateralInputs int `json:"maxCollateralInputs"`
	MaxTxExecutionUnits struct {
		Memory int64 `json:"memory"`
		Steps  int64 `json:"steps"`
	} `json:"maxTxExecutionUnits"`
	MaxTxSize                  int     `json:"maxTxSize"`
	MaxValueSize               int     `json:"maxValueSize"`
	MinFeeRefScriptCostPerByte int     `json:"minFeeRefScriptCostPerByte"`
	MinPoolCost                int64   `json:"minPoolCost"`
	MonetaryExpansion          float64 `json:"monetaryExpansion"`
	PoolPledgeInfluence        float64 `json:"poolPledgeInfluence"`
	PoolRetireMaxEpoch         int     `json:"poolRetireMaxEpoch"`
	PoolVotingThresholds       struct {
		CommitteeNoConfidence float64 `json:"committeeNoConfidence"`
		CommitteeNormal       float64 `json:"committeeNormal"`
		HardForkInitiation    float64 `json:"hardForkInitiation"`
		MotionNoConfidence    float64 `json:"motionNoConfidence"`
		PPSecurityGroup       float64 `json:"ppSecurityGroup"`
	} `json:"poolVotingThresholds"`
	ProtocolVersion struct {
		Major int `json:"major"`
		Minor int `json:"minor"`
	} `json:"protocolVersion"`
	StakeAddressDeposit int64   `json:"stakeAddressDeposit"`
	StakePoolDeposit    int64   `json:"stakePoolDeposit"`
	StakePoolTargetNum  int     `json:"stakePoolTargetNum"`
	TreasuryCut         float64 `json:"treasuryCut"`
	TxFeeFixed          int     `json:"txFeeFixed"`
	TxFeePerByte        int     `json:"txFeePerByte"`
	UTXOCostPerByte     int     `json:"utxoCostPerByte"`
}

func (c *CardanoCLI) Parameters() (CardanoCLIParameters, error) {
	obj, err := c.invoke(
		"query", "protocol-parameters",
	)

	if err != nil {
		return CardanoCLIParameters{}, err
	}

	var params CardanoCLIParameters
	if err := json.Unmarshal([]byte(obj), &params); err != nil {
		return CardanoCLIParameters{}, err
	}

	return params, nil
}

func (c *CardanoCLI) SubmitTx(txPath string) (string, error) {
	return c.invoke(
		"latest", "transaction", "submit",
		"--tx-file", txPath,
	)
}

type CardanoCLITip struct {
	Block           int    `json:"block"`
	Epoch           int    `json:"epoch"`
	Era             string `json:"era"`
	Hash            string `json:"hash"`
	Slot            uint64 `json:"slot"`
	SlotInEpoch     int    `json:"slotInEpoch"`
	SlotsToEpochEnd int    `json:"slotsToEpochEnd"`
	SyncProgress    string `json:"syncProgress"`
}

func (c *CardanoCLI) Tip() (CardanoCLITip, error) {
	obj, err := c.invoke(
		"query", "tip",
	)

	if err != nil {
		return CardanoCLITip{}, err
	}

	var tip CardanoCLITip
	if err := json.Unmarshal([]byte(obj), &tip); err != nil {
		return CardanoCLITip{}, err
	}

	return tip, nil
}

func (c *CardanoCLI) ConvertTimeToSlot(t time.Time) (uint64, error) {
	f := t.UTC().Format("2006-01-02T15:04:05Z")

	//fmt.Printf("refTime in seconds: %d, refTime formatted: %s", t.Unix(), f)
	obj, err := c.invoke(
		"query", "slot-number", f,
	)

	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(obj, 10, 64)
}

// ConvertSlotsToTime returns the future time at which the given slot number
// will be reached. The provided slot must be an absolute slot number.
// The current tip is fetched to determine the offset.
func (c *CardanoCLI) ConvertSlotToTime(slot uint64) (time.Time, error) {
	refTime, refSlot, err := c.GetRefTimeAndSlot()
	if err != nil {
		return time.Time{}, err
	}

	diff := int64(slot) - int64(refSlot)

	return refTime.Add(time.Duration(diff) * time.Second), nil
}

func (c *CardanoCLI) GetRefTimeAndSlot() (time.Time, uint64, error) {
	// remove ms
	// this ensures that the number is properly rounded for downstream use (TODO: all refTipTimes should be in seconds instead of milliseconds)
	refTime := time.Unix(time.Now().Unix(), 0) 

	refSlot, err := c.ConvertTimeToSlot(refTime)

	return refTime, refSlot, err
}

func (c *CardanoCLI) DeriveParameters() (HeliosNetworkParams, error) {
	params, err := c.Parameters()
	if err != nil {
		return HeliosNetworkParams{}, err
	}

	refTime, refSlot, err := c.GetRefTimeAndSlot()
	if err != nil {
		return HeliosNetworkParams{}, err
	}

	heliosParams := HeliosNetworkParams{
		CollateralPercentage: params.CollateralPercentage,
		CostModelParamsV1:    params.CostModels.PlutusV1,
		CostModelParamsV2:    params.CostModels.PlutusV2,
		CostModelParamsV3:    params.CostModels.PlutusV3,
		ExCPUFeePerUnit:      params.ExecutionUnitPrices.PriceSteps,
		ExMemFeePerUnit:      params.ExecutionUnitPrices.PriceMemory,
		MaxCollateralInputs:  params.MaxCollateralInputs,
		MaxTxExCPU:           params.MaxTxExecutionUnits.Steps,
		MaxTxExMem:           params.MaxTxExecutionUnits.Memory,
		MaxTxSize:            params.MaxTxSize,
		RefScriptsFeePerByte: params.MinFeeRefScriptCostPerByte,
		RefTipSlot:           int64(refSlot),
		RefTipTime:           refTime.Unix()*1000, 
		SecondsPerSlot:       1,
		StakeAddrDeposit:     params.StakeAddressDeposit,
		TxFeeFixed:           params.TxFeeFixed,
		TxFeePerByte:         params.TxFeePerByte,
		UTXODepositPerByte:   params.UTXOCostPerByte,
	}

	return heliosParams, nil
}

func (c *CardanoCLI) UTXO(txID string, utxoIndex int) ([]byte, error) {
	cborHex, err := c.invoke(
		"query", "utxo",
		"--tx-in", fmt.Sprintf("%s#%d", txID, utxoIndex),
		"--output-cbor",
	)

	if err != nil {
		return nil, err
	} else if cborHex == "a0" {
		// The route handler can use the postgres table to determine if UTXO was spent or not
		return nil, nil
	}

	cbor, err := hex.DecodeString(strings.TrimSpace(cborHex))
	if err != nil {
		return nil, err
	}

	return cbor, nil
}

func (c *CardanoCLI) invoke(args ...string) (string, error) {
	if c.networkName == "mainnet" {
		args = append(args, "--mainnet")
	} else {
		args = append(args, "--testnet-magic", "1")
	}

	args = append(args, "--socket-path", "/run/cardano-node/node.socket")

	cmd := exec.Command("cardano-cli", args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return "", fmt.Errorf("command failed: %w, %s", err, stderr.String())
	}

	return stdout.String(), nil
}
