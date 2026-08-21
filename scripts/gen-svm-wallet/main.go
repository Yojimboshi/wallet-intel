// Generate a fresh Solana (SVM) wallet (address + private key).
//
// Usage:
//
//	go run ./scripts/gen-svm-wallet
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math/big"
	"os"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

func main() {
	log.SetFlags(0)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate key: %v\n", err)
		os.Exit(1)
	}

	address := base58Encode(pub)
	secretKey := base58Encode(priv)
	seed := priv.Seed()

	fmt.Println("SVM (Solana) wallet")
	fmt.Println("-------------------")
	fmt.Printf("address:           %s\n", address)
	fmt.Printf("private_key:       %s\n", secretKey)
	fmt.Printf("seed_hex:          %s\n", hex.EncodeToString(seed))
	fmt.Println()
	fmt.Println("private_key is base58-encoded 64-byte keypair (Phantom / Solflare import format).")
	fmt.Println("Store it offline. Anyone with it controls the wallet.")
}

func base58Encode(b []byte) string {
	if len(b) == 0 {
		return ""
	}

	zeros := 0
	for zeros < len(b) && b[zeros] == 0 {
		zeros++
	}

	x := new(big.Int).SetBytes(b)
	radix := big.NewInt(58)
	mod := new(big.Int)
	var encoded []byte
	for x.Sign() > 0 {
		x.DivMod(x, radix, mod)
		encoded = append(encoded, base58Alphabet[mod.Int64()])
	}
	for i := 0; i < zeros; i++ {
		encoded = append(encoded, base58Alphabet[0])
	}
	for i, j := 0, len(encoded)-1; i < j; i, j = i+1, j-1 {
		encoded[i], encoded[j] = encoded[j], encoded[i]
	}
	return string(encoded)
}
