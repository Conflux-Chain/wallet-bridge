package app

import (
	"errors"
	"maps"
	"strings"
	"testing"
)

func TestTransactionInput(t *testing.T) {
	session := Session{Account: "0x" + strings.Repeat("ab", 20), ChainID: "0x47"}
	to := "0x" + strings.Repeat("cD", 20)
	base := map[string]string{"from": "0x" + strings.Repeat("AB", 20), "to": to, "chainId": "71"}
	maximum := "0x" + strings.Repeat("f", 64)
	for _, tc := range []struct {
		name  string
		input map[string]string
		want  Transaction
	}{
		{"transfer defaults", nil, Transaction{Value: "0x0", Data: "0x"}},
		{"gas only", map[string]string{"gas": "21000"}, Transaction{Value: "0x0", Data: "0x", Gas: "0x5208"}},
		{"legacy", map[string]string{"gasPrice": "0", "value": "9007199254740993", "data": "0x00AB"},
			Transaction{Value: "0x20000000000001", Data: "0x00ab", GasPrice: "0x0"}},
		{"dynamic", map[string]string{"maxFeePerGas": "0x0100", "maxPriorityFeePerGas": "255"},
			Transaction{Value: "0x0", Data: "0x", MaxFeePerGas: "0x100", MaxPriorityFeePerGas: "0xff"}},
		{"equal caps", map[string]string{"maxFeePerGas": "10", "maxPriorityFeePerGas": "0x0a"},
			Transaction{Value: "0x0", Data: "0x", MaxFeePerGas: "0xa", MaxPriorityFeePerGas: "0xa"}},
		{"maximum amount", map[string]string{"value": maximum}, Transaction{Value: maximum, Data: "0x"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := maps.Clone(base)
			maps.Copy(input, tc.input)
			expected := tc.want
			expected.From, expected.To, expected.ChainID = session.Account, to, session.ChainID
			got, err := NormalizeTransaction(input, session)
			if err != nil || got != expected {
				t.Fatalf("got %+v, %v; want %+v", got, err, expected)
			}
		})
	}
	session.ChainID = "0x1" + strings.Repeat("0", 64)
	base["chainId"] = "0x000" + session.ChainID[2:]
	if tx, err := NormalizeTransaction(base, session); err != nil || tx.ChainID != session.ChainID {
		t.Fatalf("large Session chain ID = %q, %v", tx.ChainID, err)
	}
}

func TestTransactionInputErrors(t *testing.T) {
	session := Session{Account: "0x" + strings.Repeat("11", 20), ChainID: "0x47"}
	base := map[string]string{"from": session.Account, "to": "0x" + strings.Repeat("22", 20), "chainId": "71"}
	for _, field := range []string{"from", "to", "chainId"} {
		input := maps.Clone(base)
		delete(input, field)
		if _, err := NormalizeTransaction(input, session); !errors.Is(err, ErrInvalidTransaction) || !strings.Contains(err.Error(), field) {
			t.Errorf("missing %s: %v", field, err)
		}
	}
	for _, tc := range []struct {
		name     string
		fields   map[string]string
		fragment string
	}{
		{"deployment", map[string]string{"to": ""}, "deployment"},
		{"malformed to", map[string]string{"to": "0x1234"}, "to"},
		{"invalid address digits", map[string]string{"from": "0x" + strings.Repeat("GG", 20)}, "from"},
		{"different account", map[string]string{"from": base["to"]}, "Session account"},
		{"different network", map[string]string{"chainId": "1"}, "Session network"},
		{"missing data prefix", map[string]string{"data": "00"}, "data"},
		{"odd byte string", map[string]string{"data": "0xabc"}, "data"},
		{"invalid bytes", map[string]string{"data": "0x0g"}, "data"},
		{"explicit empty data", map[string]string{"data": ""}, "data"},
		{"empty vs omitted amount", map[string]string{"value": ""}, "value"},
		{"negative", map[string]string{"gasPrice": "-1"}, "gasPrice"},
		{"signed hex", map[string]string{"value": "0x+1"}, "value"},
		{"fraction", map[string]string{"gas": "1.5"}, "gas"},
		{"exponent", map[string]string{"value": "1e18"}, "value"},
		{"overflow", map[string]string{"value": "0x1" + strings.Repeat("0", 64)}, "value"},
		{"decimal overflow", map[string]string{"value": "115792089237316195423570985008687907853269984665640564039457584007913129639936"}, "value"},
		{"fee cap only", map[string]string{"maxFeePerGas": "5"}, "together"},
		{"tip only", map[string]string{"maxPriorityFeePerGas": "0"}, "together"},
		{"mixed fees", map[string]string{"gasPrice": "0", "maxFeePerGas": "1", "maxPriorityFeePerGas": "0"}, "gasPrice"},
		{"tip above cap", map[string]string{"maxFeePerGas": "15", "maxPriorityFeePerGas": "16"}, "exceeds"},
		{"larger tip same length", map[string]string{"maxFeePerGas": "16", "maxPriorityFeePerGas": "17"}, "exceeds"},
		{"explicit zero nonce", map[string]string{"nonce": "0"}, "nonce"},
		{"explicit type", map[string]string{"type": "0x2"}, "type"},
		{"additional field", map[string]string{"accessList": ""}, "accessList"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := maps.Clone(base)
			maps.Copy(input, tc.fields)
			got, err := NormalizeTransaction(input, session)
			if got != (Transaction{}) || !errors.Is(err, ErrInvalidTransaction) || !strings.Contains(err.Error(), tc.fragment) {
				t.Fatalf("got %+v, %v; want rejection mentioning %q", got, err, tc.fragment)
			}
		})
	}
}
