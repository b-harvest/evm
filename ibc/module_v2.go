package ibc

import (
	"bytes"

	sdk "github.com/cosmos/cosmos-sdk/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	ibcapi "github.com/cosmos/ibc-go/v10/modules/core/api"
	ibcmockv1 "github.com/cosmos/ibc-go/v10/testing/mock"
	ibcmockv2 "github.com/cosmos/ibc-go/v10/testing/mock/v2"
)

var _ ibcapi.IBCModule = &IBCModuleV2{}

// IBCModuleV2 is a mock implementation of the IBCModule interface.
// which delegates calls to the underlying IBCApp.
type IBCModuleV2 struct {
	IBCApp ibcmockv2.IBCApp
}

// NewIBCModuleV2 creates a new IBCModule with an underlying mock IBC application.
func NewIBCModuleV2() *IBCModuleV2 {
	return &IBCModuleV2{
		IBCApp: ibcmockv2.IBCApp{},
	}
}

func (im IBCModuleV2) OnSendPacket(ctx sdk.Context, sourceChannel string, destinationChannel string, sequence uint64, data channeltypesv2.Payload, signer sdk.AccAddress) error {
	if im.IBCApp.OnSendPacket != nil {
		return im.IBCApp.OnSendPacket(ctx, sourceChannel, destinationChannel, sequence, data, signer)
	}
	return nil
}

func (im IBCModuleV2) OnRecvPacket(ctx sdk.Context, sourceChannel string, destinationChannel string, sequence uint64, payload channeltypesv2.Payload, relayer sdk.AccAddress) channeltypesv2.RecvPacketResult {
	if im.IBCApp.OnRecvPacket != nil {
		return im.IBCApp.OnRecvPacket(ctx, sourceChannel, destinationChannel, sequence, payload, relayer)
	}
	if bytes.Equal(payload.Value, ibcmockv1.MockPacketData) {
		return ibcmockv2.MockRecvPacketResult
	}
	return channeltypesv2.RecvPacketResult{Status: channeltypesv2.PacketStatus_Failure}
}

func (im IBCModuleV2) OnAcknowledgementPacket(ctx sdk.Context, sourceChannel string, destinationChannel string, sequence uint64, acknowledgement []byte, payload channeltypesv2.Payload, relayer sdk.AccAddress) error {
	if im.IBCApp.OnAcknowledgementPacket != nil {
		return im.IBCApp.OnAcknowledgementPacket(ctx, sourceChannel, destinationChannel, sequence, payload, acknowledgement, relayer)
	}
	return nil
}

func (im IBCModuleV2) OnTimeoutPacket(ctx sdk.Context, sourceChannel string, destinationChannel string, sequence uint64, payload channeltypesv2.Payload, relayer sdk.AccAddress) error {
	if im.IBCApp.OnTimeoutPacket != nil {
		return im.IBCApp.OnTimeoutPacket(ctx, sourceChannel, destinationChannel, sequence, payload, relayer)
	}
	return nil
}

func (im IBCModuleV2) UnmarshalPacketData(payload channeltypesv2.Payload) (interface{}, error) {
	if bytes.Equal(payload.Value, ibcmockv1.MockPacketData) {
		return ibcmockv1.MockPacketData, nil
	}
	return nil, ibcmockv1.MockApplicationCallbackError
}
