package types

import (
	"encoding/hex"
	"fmt"
	"strings"

	ethtypes "github.com/ethereum/go-ethereum/core/types"

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
		Accounts:     []GenesisAccount{},
		Params:       DefaultParams(),
		ReceiptsRoot: ethtypes.EmptyRootHash.String(),
	}
}

// NewGenesisState creates a new genesis state.
func NewGenesisState(params Params, accounts []GenesisAccount, receiptsRoot string) *GenesisState {
	return &GenesisState{
		Accounts:     accounts,
		Params:       params,
		ReceiptsRoot: receiptsRoot,
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

	if err := isReceiptsRootValid(gs.ReceiptsRoot); err != nil {
		return err
	}

	return gs.Params.Validate()
}

func isReceiptsRootValid(receiptsRoot string) error {
	if !strings.HasPrefix(receiptsRoot, "0x") {
		return fmt.Errorf("invalid receipts root: %s, should start with 0x", receiptsRoot)
	}

	hexString := receiptsRoot[2:]
	if len(hexString) != 64 {
		return fmt.Errorf("invalid receipts root: %s, should be 32 bytes (64 hex characters)", receiptsRoot)
	}

	if _, err := hex.DecodeString(hexString); err != nil {
		return fmt.Errorf("invalid receipts root: %s, invalid hex characters", receiptsRoot)
	}

	return nil
}
