package main

import (
	"testing"
)

func TestParseTxSubmitError(t *testing.T) {
	tests := []struct {
		name   string
		errStr string
		check  func(t *testing.T, e CardanoCLITxSubmitError)
	}{
		{
			name:   "bad inputs + value mismatch + missing input",
			errStr: "ShelleyTxValidationError ShelleyBasedEraConway (ApplyTxError (ConwayUtxowFailure (UtxoFailure (BadInputsUTxO (fromList [TxIn (TxId {unTxId = SafeHash \"82e7dc25de3699cb0cfd3e55c4115ac8c23ffd18471645ca6d2832cdb1be65f0\"}) (TxIx {unTxIx = 1})]))) :| [ConwayUtxowFailure (UtxoFailure (ValueNotConservedUTxO (Mismatch {mismatchSupplied = MaryValue (Coin 0) (MultiAsset (fromList [(PolicyID {policyID = ScriptHash \"737693ec75c198b82cc287418cddd90d762fda772814fd228e74bad7\"},fromList [(\"\",1)])])), mismatchExpected = MaryValue (Coin 4827635) (MultiAsset (fromList [(PolicyID {policyID = ScriptHash \"737693ec75c198b82cc287418cddd90d762fda772814fd228e74bad7\"},fromList [(\"\",1)])]))}))),ConwayUtxowFailure (UtxoFailure (UtxosFailure (CollectErrors [BadTranslation (BabbageContextError (AlonzoContextError (TranslationLogicMissingInput (TxIn (TxId {unTxId = SafeHash \"82e7dc25de3699cb0cfd3e55c4115ac8c23ffd18471645ca6d2832cdb1be65f0\"}) (TxIx {unTxIx = 1})))))])))]))",
			check: func(t *testing.T, e CardanoCLITxSubmitError) {
				if len(e.BadInputs) != 1 {
					t.Fatalf("expected one bad input, got %d", len(e.BadInputs))
				}
				if e.BadInputs[0].TxID != "82e7dc25de3699cb0cfd3e55c4115ac8c23ffd18471645ca6d2832cdb1be65f0" || e.BadInputs[0].Index != 1 {
					t.Fatalf("bad input not parsed correctly: %#v", e.BadInputs[0])
				}
				if e.ValueMismatch == nil || e.ValueMismatch.Expected != 4827635 || e.ValueMismatch.Supplied != 0 {
					t.Fatalf("value mismatch not parsed")
				}
				if len(e.MissingInputs) != 1 {
					t.Fatalf("expected one missing input")
				}
			},
		},
		{
			name:   "insufficient collateral",
			errStr: "ShelleyTxValidationError ShelleyBasedEraConway (ApplyTxError (ConwayUtxowFailure (UtxoFailure (InsufficientCollateral (DeltaCoin (-4549920)) (Coin 277715))) :| [ConwayUtxowFailure (UtxoFailure NoCollateralInputs),ConwayUtxowFailure (UtxoFailure (BadInputsUTxO (fromList [TxIn (TxId {unTxId = SafeHash \"b1e73eb15c6088753206aa356773a037c8d18c392c6803d1d6c1ea940c9f8dac\"}) (TxIx {unTxIx = 1})])))]))",
			check: func(t *testing.T, e CardanoCLITxSubmitError) {
				if e.InsufficientCollateral == nil || e.InsufficientCollateral.Delta != -4549920 || e.InsufficientCollateral.Provided != 277715 {
					t.Fatalf("insufficient collateral not parsed correctly: %#v", e.InsufficientCollateral)
				}
				if !e.NoCollateralInputs {
					t.Fatalf("no collateral inputs not detected")
				}
				if len(e.BadInputs) != 1 {
					t.Fatalf("bad inputs not parsed")
				}
			},
		},
		{
			name: "4 bad inputs",
			errStr: "ShelleyTxValidationError ShelleyBasedEraConway (ApplyTxError (ConwayUtxowFailure (UtxoFailure (BadInputsUTxO (fromList [TxIn (TxId {unTxId = SafeHash \"78ea3295609e6e7ae995774ebe924dc408accac86e1f8e972544fd6f25fb1049\"}) (TxIx {unTxIx = 0}),TxIn (TxId {unTxId = SafeHash \"cc2c9969c7a4e2f05170a2194cbd8edd11052f1ef072efbf4d133702cf8030e4\"}) (TxIx {unTxIx = 0}),TxIn (TxId {unTxId = SafeHash \"cc2c9969c7a4e2f05170a2194cbd8edd11052f1ef072efbf4d133702cf8030e4\"}) (TxIx {unTxIx = 1}),TxIn (TxId {unTxId = SafeHash \"f0cd1c37b54f70de78f4ebfb3db614847f8c5cc1f18d3a2e46c0e9fcd2d65ad2\"}) (TxIx {unTxIx = 0})]))) :| [ConwayUtxowFailure (UtxoFailure (ValueNotConservedUTxO (Mismatch {mismatchSupplied = MaryValue (Coin 0) (MultiAsset (fromList [])), mismatchExpected = MaryValue (Coin 7722228977) (MultiAsset (fromList [(PolicyID {policyID = ScriptHash \"737693ec75c198b82cc287418cddd90d762fda772814fd228e74bad7\"},fromList [(\"\",1)]),(PolicyID {policyID = ScriptHash \"be17b2cf8929e567bd635a32d808eb62e20833bffbeeedaa8fa353a4\"},fromList [(\"\",1)]),(PolicyID {policyID = ScriptHash \"de4a9efee37ff7bb123dd09d5e73e5f092e22bc1728e8baca30d5c21\"},fromList [(\"\",1)])]))})))]))",
			check: func(t *testing.T, e CardanoCLITxSubmitError) {
				if len(e.BadInputs) != 4 {
					t.Fatalf("bad inputs not parsed")
				}

				if e.BadInputs[0].TxID != "78ea3295609e6e7ae995774ebe924dc408accac86e1f8e972544fd6f25fb1049" {
					t.Fatalf("unexpected 1st bad input")
				}

				if e.BadInputs[1].TxID != "cc2c9969c7a4e2f05170a2194cbd8edd11052f1ef072efbf4d133702cf8030e4" {
					t.Fatalf("unexpected 2nd bad input")
				}

				if e.BadInputs[2].TxID != "cc2c9969c7a4e2f05170a2194cbd8edd11052f1ef072efbf4d133702cf8030e4" {
					t.Fatalf("unexpected 3rd bad input")
				}

				if e.BadInputs[3].TxID != "f0cd1c37b54f70de78f4ebfb3db614847f8c5cc1f18d3a2e46c0e9fcd2d65ad2" {
					t.Fatalf("unexpected 4th bad input")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTxSubmitError(tt.errStr)
			tt.check(t, got)
		})
	}
}
