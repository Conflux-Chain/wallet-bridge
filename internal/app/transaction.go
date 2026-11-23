package app

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// Transaction holds the fields shared by review and wallet submission.
// Optional quantities are empty when the wallet should estimate them.
type Transaction struct {
	From                 string
	To                   string
	ChainID              string
	Value                string
	Data                 string
	Gas                  string
	GasPrice             string
	MaxFeePerGas         string
	MaxPriorityFeePerGas string
}

var ErrInvalidTransaction = errors.New("invalid transaction")

// NormalizeTransaction checks the payload against a stored Session binding.
// The caller checks Session liveness when it commits the request.
func NormalizeTransaction(fields map[string]string, session Session) (Transaction, error) {
	tx := Transaction{From: fields["from"], To: fields["to"], Value: "0x0", Data: "0x"}
	if tx.To == "" {
		return Transaction{}, fmt.Errorf("%w: to is required (contract deployment is unsupported)", ErrInvalidTransaction)
	}
	for _, address := range []struct{ name, value string }{{"from", tx.From}, {"to", tx.To}} {
		if len(address.value) != 42 || !hexBytes(address.value) {
			return Transaction{}, fmt.Errorf("%w: %s must be a 20-byte hex address", ErrInvalidTransaction, address.name)
		}
	}
	if !strings.EqualFold(tx.From, session.Account) {
		return Transaction{}, fmt.Errorf("%w: from differs from the Session account", ErrInvalidTransaction)
	}
	tx.From = session.Account

	quantities := map[string]*string{
		"chainId": &tx.ChainID, "value": &tx.Value, "gas": &tx.Gas, "gasPrice": &tx.GasPrice,
		"maxFeePerGas": &tx.MaxFeePerGas, "maxPriorityFeePerGas": &tx.MaxPriorityFeePerGas,
	}
	for field, raw := range fields {
		switch field {
		case "from", "to":
			continue
		case "data":
			if !hexBytes(raw) {
				return Transaction{}, fmt.Errorf("%w: data must contain 0x-prefixed whole bytes", ErrInvalidTransaction)
			}
			tx.Data = strings.ToLower(raw)
		default:
			target, supported := quantities[field]
			if !supported {
				return Transaction{}, fmt.Errorf("%w: unsupported field %q", ErrInvalidTransaction, field)
			}
			value, ok := transactionQuantity(raw, field)
			if !ok {
				return Transaction{}, fmt.Errorf("%w: invalid %s quantity", ErrInvalidTransaction, field)
			}
			*target = value
		}
	}
	if tx.ChainID == "" || tx.ChainID != session.ChainID {
		return Transaction{}, fmt.Errorf("%w: chainId is required and must match the Session network", ErrInvalidTransaction)
	}
	if (tx.MaxFeePerGas == "") != (tx.MaxPriorityFeePerGas == "") {
		return Transaction{}, fmt.Errorf("%w: maxFeePerGas and maxPriorityFeePerGas must be supplied together", ErrInvalidTransaction)
	}
	if tx.GasPrice != "" && tx.MaxFeePerGas != "" {
		return Transaction{}, fmt.Errorf("%w: gasPrice conflicts with EIP-1559 fees", ErrInvalidTransaction)
	}
	// Canonical hex quantities of equal length sort in numeric order.
	tip, cap := tx.MaxPriorityFeePerGas, tx.MaxFeePerGas
	if len(tip) > len(cap) || len(tip) == len(cap) && tip > cap {
		return Transaction{}, fmt.Errorf("%w: maxPriorityFeePerGas exceeds maxFeePerGas", ErrInvalidTransaction)
	}
	return tx, nil
}

func hexBytes(value string) bool {
	digits, ok := strings.CutPrefix(value, "0x")
	return ok && len(digits)%2 == 0 && strings.Trim(digits, "0123456789abcdefABCDEF") == ""
}

func transactionQuantity(raw, field string) (string, bool) {
	if strings.HasPrefix(raw, "0x") {
		value, ok := canonicalHexQuantity(raw)
		return value, ok && (field == "chainId" || len(value)-2 <= 64)
	}
	if raw == "" || strings.Trim(raw, "0123456789") != "" {
		return "", false
	}
	digits := strings.TrimLeft(raw, "0")
	if digits == "" {
		return "0x0", true
	}
	if field != "chainId" && len(digits) > 78 {
		return "", false
	}
	number, ok := new(big.Int).SetString(digits, 10)
	if !ok || field != "chainId" && number.BitLen() > 256 {
		return "", false
	}
	return "0x" + number.Text(16), true
}
