package main

import (
	"encoding/hex"
	"fmt"
	"testing"
)

func TestHashDatum(t *testing.T) {
	tests := []struct{
		input string // hex encded 
		expectedHash string // hex encoded
	}{
		{
			"9fd8799fd8799f581c3a5904074323a4cddfe1103969962a5807c6c37495db9df48d019f9affd8799fd8799fd8799f581c5a0987ee3ec775d90cb16851a5f3cc9d8b03bd6492329e8936844229ffffffff001a000fac941a05265c00ff",
			"2506404fab413208f28981b818d544f2128bfc9480e489662513cf4659fef24d",
		},
	}

	for i, tt := range tests{
		t.Run(fmt.Sprintf("inline datum %d", i), func(t *testing.T) {
			inputBytes, err := hex.DecodeString(tt.input)
			if err != nil {
				t.Fatalf("unexpected input decoding err for test %d: %v", i, err)
			}

			got := HashDatum(inputBytes)

			if got != tt.expectedHash {
				t.Errorf("for inline datum hash test %d, got %s but want %s", i, got, tt.expectedHash)
			}
		})
	}
}