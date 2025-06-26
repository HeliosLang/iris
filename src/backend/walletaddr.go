package main

import (
	"strings"

	cg "github.com/echovl/cardano-go"
	"github.com/echovl/cardano-go/crypto"
	bip39 "github.com/tyler-smith/go-bip39"
)

func firstEnterpriseAddressKey(words []string) (crypto.XPrvKey, error) {
	mnemonic := strings.Join(words, " ")
	entropy, err := bip39.EntropyFromMnemonic(mnemonic)
	if err != nil {
		return nil, err
	}
	root := crypto.NewXPrvKeyFromEntropy(entropy, "")
	account := root.Derive(1852 + 0x80000000).Derive(1815 + 0x80000000).Derive(0x80000000)
	chain := account.Derive(0)
	addrKey := chain.Derive(0)
	return addrKey, nil
}

func firstEnterpriseAddress(words []string, network string) (string, error) {
	addrKey, err := firstEnterpriseAddressKey(words)
	if err != nil {
		return "", err
	}
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

func firstEnterprisePrvKey(words []string) (crypto.PrvKey, error) {
	addrKey, err := firstEnterpriseAddressKey(words)
	if err != nil {
		return nil, err
	}
	return addrKey.PrvKey(), nil
}
