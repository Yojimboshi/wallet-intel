// Generate a fresh EVM wallet (address + private key).
//
// Usage:
//
//	go run ./scripts/gen-evm-wallet
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"

	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	log.SetFlags(0)

	key, err := ecdsa.GenerateKey(crypto.S256(), rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate key: %v\n", err)
		os.Exit(1)
	}

	priv := crypto.FromECDSA(key)
	addr := crypto.PubkeyToAddress(key.PublicKey)

	fmt.Println("EVM wallet")
	fmt.Println("----------")
	fmt.Printf("address:      %s\n", addr.Hex())
	fmt.Printf("private_key:  0x%s\n", hex.EncodeToString(priv))
	fmt.Println()
	fmt.Println("Store the private key offline. Anyone with it controls the wallet.")
}
