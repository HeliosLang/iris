package main

import (
	"strings"

	cg "github.com/echovl/cardano-go"
	"github.com/echovl/cardano-go/crypto"
	bip39 "github.com/tyler-smith/go-bip39"
)

func firstEnterpriseAddress(words []string, network string) (string, error) {
	mnemonic := strings.Join(words, " ")
	entropy, err := bip39.EntropyFromMnemonic(mnemonic)
	if err != nil {
		return "", err
	}
	root := crypto.NewXPrvKeyFromEntropy(entropy, "")
	account := root.Derive(1852 + 0x80000000).Derive(1815 + 0x80000000).Derive(0x80000000)
	chain := account.Derive(0)
	addrKey := chain.Derive(0)
	payment, err := cg.NewKeyCredential(addrKey.PubKey())
	if err != nil {
		return "", err
	}
	net := cg.Testnet
	if network == "mainnet" {
		net = cg.Mainnet
	}
	addr, err := cg.NewEnterpriseAddress(net, payment)
	if err != nil {
		return "", err
	}
	return addr.Bech32(), nil
}
