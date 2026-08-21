package parse

import (
	"math/big"
	"testing"

	"github.com/Yojimboshi/wallet-intel/internal/chain"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

const (
	testTxHash  = "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testBlock   = uint64(18_000_000)
	testWallet  = "0x000000000000000000000000000000000000a001"
	testToken   = "0x000000000000000000000000000000000000c0de"
	testSender  = "0x000000000000000000000000000000000000b001"
	testAmount  = "0x00000000000000000000000000000000000000000017d84aa7481b9053a64ff2"
)

func TestParseTransferBuy(t *testing.T) {
	wallet := common.HexToAddress(testWallet)
	watched := map[common.Address]string{wallet: "test-wallet"}

	log := types.Log{
		Address: common.HexToAddress(testToken),
		Topics: []common.Hash{
			topicTransfer,
			common.BytesToHash(common.LeftPadBytes(common.HexToAddress(testSender).Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(wallet.Bytes(), 32)),
		},
		Data:  common.FromHex(testAmount),
		Index: 1,
	}

	trades := ParseLogs(
		[]*types.Log{&log},
		watched,
		chain.EthereumMainnet,
		common.HexToHash(testTxHash),
		testBlock,
	)

	if len(trades) != 1 {
		t.Fatalf("expected 1 trade, got %d", len(trades))
	}

	tr := trades[0]
	if tr.Side != sideBuy {
		t.Fatalf("expected buy, got %s", tr.Side)
	}
	if tr.Wallet != wallet {
		t.Fatalf("unexpected wallet %s", tr.Wallet.Hex())
	}
	if tr.Token != common.HexToAddress(testToken) {
		t.Fatalf("unexpected token %s", tr.Token.Hex())
	}
	if tr.TokenAmount.Cmp(bigFromHex(testAmount)) != 0 {
		t.Fatalf("unexpected amount %s", tr.TokenAmount.String())
	}
	if tr.TxHash != common.HexToHash(testTxHash) {
		t.Fatalf("unexpected tx hash")
	}
	if tr.BlockNumber != testBlock {
		t.Fatalf("unexpected block %d", tr.BlockNumber)
	}
}

func bigFromHex(h string) *big.Int {
	n := new(big.Int)
	n.SetString(h[2:], 16)
	return n
}
