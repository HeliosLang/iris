package main

import (
	"log"
	"os"
	"strings"
)

const (
	WalletFile     = "/etc/cardano-iris/wallet"
	CollateralFile = "/etc/cardano-iris/collateral"
	NetworkFile    = "/etc/cardano-iris/network"
)

// Config holds global configuration settings.
type Config struct {
	Wallet      []string
	Collateral  string
	NetworkName string
}

// NewConfig reads configuration from disk.
func NewConfig() *Config {
	return &Config{
		Wallet:      readWalletPhrase(),
		Collateral:  readCollateral(),
		NetworkName: readNetworkName(),
	}
}

func readWalletPhrase() []string {
	data, err := os.ReadFile(WalletFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		log.Fatalf("Error reading file %s: %v", WalletFile, err)
	}
	words := strings.Fields(string(data))
	return words
}

func readCollateral() string {
	data, err := os.ReadFile(CollateralFile)
	if err != nil {
		if os.IsNotExist(err) {
			return ""
		}
		log.Fatalf("Error reading file %s: %v", CollateralFile, err)
	}
	return strings.TrimSpace(string(data))
}

func readNetworkName() string {
	data, err := os.ReadFile(NetworkFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "preprod"
		}

		log.Fatalf("Error reading file %s: %v\n", NetworkFile, err)
	}

	name := strings.TrimSpace(string(data))

	if name != "preprod" && name != "mainnet" {
		log.Fatalf("Expected preprod or mainnet in %s, got %v\n", NetworkFile, name)
	}

	return name
}
