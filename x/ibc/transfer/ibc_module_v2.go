package transfer

import (
	v2 "github.com/cosmos/ibc-go/v10/modules/apps/transfer/v2"
	ibcapi "github.com/cosmos/ibc-go/v10/modules/core/api"

	"github.com/cosmos/evm/x/ibc/transfer/keeper"
)

var _ ibcapi.IBCModule = IBCModuleV2{}

// IBCModuleV2 implements the ICS26 interface for transfer given the transfer keeper.
type IBCModuleV2 struct {
	*v2.IBCModule
}

// NewIBCModule creates a new IBCModule given the keeper
func NewIBCModuleV2(k keeper.Keeper) IBCModuleV2 {
	transferModule := v2.NewIBCModule(*k.Keeper)
	return IBCModuleV2{
		IBCModule: transferModule,
	}
}
