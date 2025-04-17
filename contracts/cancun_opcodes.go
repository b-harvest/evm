package contracts

import (
	_ "embed"

	contractutils "github.com/cosmos/evm/contracts/utils"
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

var (
	// CancunOpcodesJSON are the compiled bytes of the CancunOpcodesContract
	//
	//go:embed solidity/CancunOpcodes.json
	CancunOpcodesJSON []byte

	// CancunOpcodesContract is the compiled cancun opcodes contract
	CancunOpcodesContract evmtypes.CompiledContract
)

func init() {
	var err error
	if CancunOpcodesContract, err = contractutils.ConvertHardhatBytesToCompiledContract(
		CancunOpcodesJSON,
	); err != nil {
		panic(err)
	}
}
