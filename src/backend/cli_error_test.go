package main

import "testing"

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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTxSubmitError(tt.errStr)
			tt.check(t, got)
		})
	}
}
