package testutil

import (
	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
)

var (
	UosmoDenom    = transfertypes.NewDenom("uosmo", transfertypes.NewHop(transfertypes.PortID, "channel-0"))
	UosmoIbcDenom = UosmoDenom.IBCDenom()

	UatomDenom    = transfertypes.NewDenom("uatom", transfertypes.NewHop(transfertypes.PortID, "channel-1"))
	UatomIbcdDnom = UatomDenom.IBCDenom()

	UAtomDenom    = transfertypes.NewDenom("aatom", transfertypes.NewHop(transfertypes.PortID, "channel-0"))
	UAtomIbcDenom = UAtomDenom.IBCDenom()

	UatomOsmoDenom = transfertypes.NewDenom(
		"uatom",
		transfertypes.NewHop(transfertypes.PortID, "channel-0"),
		transfertypes.NewHop(transfertypes.PortID, "channel-1"),
	)
	UatomOsmoIbcDenom = UatomOsmoDenom.IBCDenom()

	AAtomDenom    = transfertypes.NewDenom("aatom", transfertypes.NewHop(transfertypes.PortID, "channel-0"))
	AAtomIbcDenom = AAtomDenom.IBCDenom()
)
