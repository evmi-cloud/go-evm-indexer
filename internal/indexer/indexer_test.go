package indexer

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethTypes "github.com/ethereum/go-ethereum/core/types"
	evmi_database "github.com/evmi-cloud/go-evm-indexer/internal/database/evmi-database"
	"github.com/rs/zerolog"
)

const erc20TransferAbi = `[{
	"anonymous": false,
	"inputs": [
		{"indexed": true, "name": "from", "type": "address"},
		{"indexed": true, "name": "to", "type": "address"},
		{"indexed": false, "name": "value", "type": "uint256"}
	],
	"name": "Transfer",
	"type": "event"
}]`

func newDecoderForTest(t *testing.T, abiJson string) *SourceIndexerService {
	t.Helper()
	parsed, err := abi.JSON(strings.NewReader(abiJson))
	if err != nil {
		t.Fatalf("parse abi: %v", err)
	}
	return &SourceIndexerService{
		source:       evmi_database.EvmLogSource{Type: string(evmi_database.ContractLogSourceType)},
		contractName: "Token",
		abi:          parsed,
		logger:       zerolog.Nop(),
	}
}

func TestGetLogMetadataDecodesEvent(t *testing.T) {
	s := newDecoderForTest(t, erc20TransferAbi)

	from := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	to := common.HexToAddress("0x00000000000000000000000000000000000000bb")
	log := ethTypes.Log{
		Topics: []common.Hash{
			s.abi.Events["Transfer"].ID,
			common.BytesToHash(common.LeftPadBytes(from.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(to.Bytes(), 32)),
		},
		Data: common.LeftPadBytes(big.NewInt(1000).Bytes(), 32),
	}

	meta := s.GetLogMetadata(log)
	if meta.ContractName != "Token" || meta.EventName != "Transfer" {
		t.Fatalf("event not matched: %+v", meta)
	}
	if meta.Data["from"] != from.Hex() || meta.Data["to"] != to.Hex() {
		t.Errorf("indexed args not decoded: %+v", meta.Data)
	}
	if meta.Data["value"] != "1000" {
		t.Errorf("value = %q, want 1000", meta.Data["value"])
	}
}

func TestGetLogMetadataUnknownTopicAndAnonymousEvent(t *testing.T) {
	s := newDecoderForTest(t, erc20TransferAbi)

	// topic0 not in the ABI: no decode, but no failure either.
	meta := s.GetLogMetadata(ethTypes.Log{Topics: []common.Hash{common.HexToHash("0x1")}})
	if meta.EventName != "" || len(meta.Data) != 0 {
		t.Errorf("unknown topic should decode nothing, got %+v", meta)
	}

	// Anonymous event (no topics at all) must not panic on Topics[0].
	meta = s.GetLogMetadata(ethTypes.Log{})
	if meta.EventName != "Unknown" {
		t.Errorf("anonymous log should be Unknown, got %+v", meta)
	}
}

func TestGetLogMetadataSurvivesUndecodableData(t *testing.T) {
	s := newDecoderForTest(t, erc20TransferAbi)

	from := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	log := ethTypes.Log{
		Topics: []common.Hash{
			s.abi.Events["Transfer"].ID,
			common.BytesToHash(common.LeftPadBytes(from.Bytes(), 32)),
			common.BytesToHash(common.LeftPadBytes(from.Bytes(), 32)),
		},
		// Truncated data: the non-indexed uint256 cannot be unpacked. The raw
		// log is stored anyway, so decoding must degrade, not stop the source.
		Data: []byte{0x01, 0x02},
	}

	meta := s.GetLogMetadata(log)
	if meta.EventName != "Transfer" {
		t.Fatalf("event should still be matched, got %+v", meta)
	}
	if meta.Data["from"] != from.Hex() {
		t.Errorf("indexed args should still decode: %+v", meta.Data)
	}
}

func TestFormatArgValue(t *testing.T) {
	addr := common.HexToAddress("0x00000000000000000000000000000000000000aa")
	cases := []struct {
		in   any
		want string
	}{
		{"hello", "hello"},
		{true, "true"},
		{addr, addr.Hex()},
		{big.NewInt(-42), "-42"},
		{uint8(7), "7"},
		{uint32(7), "7"},
		{uint64(7), "7"},
		{[]byte{0xde, 0xad}, "dead"},
		{[4]byte{0xde, 0xad, 0xbe, 0xef}, "deadbeef"},
		{[32]byte{0xff}, "ff00000000000000000000000000000000000000000000000000000000000000"},
		{common.HexToHash("0xff00000000000000000000000000000000000000000000000000000000000000"), "ff00000000000000000000000000000000000000000000000000000000000000"},
		// No dedicated case: must degrade to a readable value, never panic.
		{uint16(9), "9"},
		{[]*big.Int{big.NewInt(1), big.NewInt(2)}, "[1 2]"},
	}
	for _, c := range cases {
		if got := formatArgValue(c.in); got != c.want {
			t.Errorf("formatArgValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
