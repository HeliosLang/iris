package main

import (
	"strings"
	"testing"
)

func TestFirstEnterpriseAddress(t *testing.T) {
	tests := []struct {
		mnemonic string
		expected string
	}{
		{
			mnemonic: "abandon amount liar amount expire adjust cage candy arch gather drum bullet absurd math era live bid rhythm alien crouch range attend journey unaware",
			expected: "addr_test1vqzkxpwrnvu3ylqvj6wupde0pjk4w28zu9893wu55z4upfc2504tp",
		},
		{
			mnemonic: "zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo vote",
			expected: "addr_test1vqlrq4h2xvj7x49shr65uxrsgkfmpq65la8lpvmxn06gprckcj4al",
		},
		{
			mnemonic: "abuse boss fly battle rubber wasp afraid hamster guide essence vibrant task banana pencil owner cube social job emotion member joy sting dash trouble",
			expected: "addr_test1vz8hjzqpaqypchy7mt254vz5n5wfwse0hvkg6gl03q5erlstefrjd",
		},
	}

	for i, tt := range tests {
		got, err := firstEnterpriseAddress(strings.Fields(tt.mnemonic), "preprod")
		if err != nil {
			t.Fatalf("test %d unexpected err: %v", i, err)
		}
		if got != tt.expected {
			t.Fatalf("test %d got %s want %s", i, got, tt.expected)
		}
	}
}
