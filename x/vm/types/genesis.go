package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cosmos/evm/types"
)

// Validate performs a basic validation of a GenesisAccount fields.
func (ga GenesisAccount) Validate() error {
	if err := types.ValidateAddress(ga.Address); err != nil {
		return err
	}
	return ga.Storage.Validate()
}

// DefaultGenesisState sets default evm genesis state with empty accounts and default params and
// chain config values.
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Accounts:      []GenesisAccount{},
		Params:        DefaultParams(),
		ReceiptsRoots: []ReceiptsRoot{},
	}
}

// NewGenesisState creates a new genesis state.
func NewGenesisState(params Params, accounts []GenesisAccount, receiptsRoots []ReceiptsRoot) *GenesisState {
	return &GenesisState{
		Accounts:      accounts,
		Params:        params,
		ReceiptsRoots: receiptsRoots,
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	seenAccounts := make(map[string]bool)
	for _, acc := range gs.Accounts {
		if seenAccounts[acc.Address] {
			return fmt.Errorf("duplicated genesis account %s", acc.Address)
		}
		if err := acc.Validate(); err != nil {
			return fmt.Errorf("invalid genesis account %s: %w", acc.Address, err)
		}
		seenAccounts[acc.Address] = true
	}

	for _, receiptsRoot := range gs.ReceiptsRoots {
		if receiptsRoot.BlockHeight < 1 {
			return fmt.Errorf("invalid receipts root: block height cannot be 0")
		}

		if !strings.HasPrefix(receiptsRoot.ReceiptRoot, "0x") {
			return fmt.Errorf("invalid receipts root: %s, should start with 0x", receiptsRoot.ReceiptRoot)
		}

		hexString := receiptsRoot.ReceiptRoot[2:]
		if len(hexString) != 64 {
			return fmt.Errorf("invalid receipts root: %s, should be 32 bytes (64 hex characters)", receiptsRoot.ReceiptRoot)
		}

		if _, err := hex.DecodeString(hexString); err != nil {
			return fmt.Errorf("invalid receipts root: %s, invalid hex characters", receiptsRoot.ReceiptRoot)
		}
	}

	return gs.Params.Validate()
}
