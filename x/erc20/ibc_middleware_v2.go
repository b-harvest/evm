package erc20

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	callbacktypes "github.com/cosmos/ibc-go/v10/modules/apps/callbacks/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	ibcapi "github.com/cosmos/ibc-go/v10/modules/core/api"

	"github.com/cosmos/evm/x/erc20/keeper"
)

var _ ibcapi.IBCModule = &IBCMiddlewareV2{}

// IBCMiddlewareV2 implements the ICS26 callbacks for the transfer middleware given
// the erc20 keeper and the underlying application.
type IBCMiddlewareV2 struct {
	app             callbacktypes.CallbacksCompatibleModuleV2
	writeAckWrapper ibcapi.WriteAcknowledgementWrapper
	keeper          keeper.Keeper
	chanKeeperV2    callbacktypes.ChannelKeeperV2

	// maxCallbackGas defines the maximum amount of gas that a callback actor can ask the
	// relayer to pay for. If a callback fails due to insufficient gas, the entire tx
	// is reverted if the relayer hadn't provided the minimum(userDefinedGas, maxCallbackGas).
	// If the actor hasn't defined a gas limit, then it is assumed to be the maxCallbackGas.
	maxCallbackGas uint64
}

// NewIBCMiddlewareV2 creates a new IBCMiddlewareV2 given the keeper and underlying application
func NewIBCMiddlewareV2(
	app ibcapi.IBCModule,
	writeAckWrapper ibcapi.WriteAcknowledgementWrapper,
	k keeper.Keeper,
	chanKeeperV2 callbacktypes.ChannelKeeperV2,
	maxCallbackGas uint64,
) IBCMiddlewareV2 {
	packetDataUnmarshalerApp, ok := app.(callbacktypes.CallbacksCompatibleModuleV2)
	if !ok {
		panic(fmt.Errorf("underlying application does not implement %T", (*callbacktypes.CallbacksCompatibleModuleV2)(nil)))
	}
	return IBCMiddlewareV2{
		app:             packetDataUnmarshalerApp,
		writeAckWrapper: writeAckWrapper,
		keeper:          k,
		chanKeeperV2:    chanKeeperV2,
		maxCallbackGas:  maxCallbackGas,
	}
}

// WithWriteAckWrapper sets the WriteAcknowledgementWrapper for the middleware.
func (im *IBCMiddlewareV2) WithWriteAckWrapper(writeAckWrapper ibcapi.WriteAcknowledgementWrapper) {
	im.writeAckWrapper = writeAckWrapper
}

// GetWriteAckWrapper returns the WriteAckWrapper
func (im *IBCMiddlewareV2) GetWriteAckWrapper() ibcapi.WriteAcknowledgementWrapper {
	return im.writeAckWrapper
}

// OnSendPacket implements source callbacks for sending packets.
// It defers to the underlying application and then calls the contract callback.
// If the contract callback returns an error, panics, or runs out of gas, then
// the packet send is rejected.
func (im IBCMiddlewareV2) OnSendPacket(
	ctx sdk.Context,
	sourceClient string,
	destinationClient string,
	sequence uint64,
	payload channeltypesv2.Payload,
	signer sdk.AccAddress,
) error {
	err := im.OnSendPacket(ctx, sourceClient, destinationClient, sequence, payload, signer)
	if err != nil {
		return err
	}
	packetData, err := im.app.UnmarshalPacketData(payload)
	// OnSendPacket is not blocked if the packet does not opt-in to callbacks
	if err != nil {
		return nil
	}

	// TODO: Implement custom logics here using packetData.
	packetData = packetData
	return nil
}

// OnRecvPacket implements the ReceivePacket destination callbacks for the ibc-callbacks middleware during
// synchronous packet acknowledgement.
// It defers to the underlying application and then calls the contract callback.
// If the contract callback runs out of gas and may be retried with a higher gas limit then the state changes are
// reverted via a panic.
func (im IBCMiddlewareV2) OnRecvPacket(
	ctx sdk.Context,
	sourceClient string,
	destinationClient string,
	sequence uint64,
	payload channeltypesv2.Payload,
	relayer sdk.AccAddress,
) channeltypesv2.RecvPacketResult {
	recvResult := im.OnRecvPacket(ctx, sourceClient, destinationClient, sequence, payload, relayer)
	// if ack is nil (asynchronous acknowledgements), then the callback will be handled in WriteAcknowledgement
	// if ack is not successful, all state changes are reverted. If a packet cannot be received, then there is
	// no need to execute a callback on the receiving chain.
	if recvResult.Status == channeltypesv2.PacketStatus_Async || recvResult.Status == channeltypesv2.PacketStatus_Failure {
		return recvResult
	}
	packetData, err := im.app.UnmarshalPacketData(payload)
	// OnRecvPacket is not blocked if the packet does not opt-in to callbacks
	if err != nil {
		return recvResult
	}

	// TODO: implement logics here using packetData.
	packetData = packetData
	return recvResult
}

// OnAcknowledgementPacket implements source callbacks for acknowledgement packets.
// It defers to the underlying application and then calls the contract callback.
// If the contract callback runs out of gas and may be retried with a higher gas limit then the state changes are
// reverted via a panic.
func (im IBCMiddlewareV2) OnAcknowledgementPacket(
	ctx sdk.Context,
	sourceClient string,
	destinationClient string,
	sequence uint64,
	acknowledgement []byte,
	payload channeltypesv2.Payload,
	relayer sdk.AccAddress,
) error {
	// we first call the underlying app to handle the acknowledgement
	err := im.OnAcknowledgementPacket(ctx, sourceClient, destinationClient, sequence, acknowledgement, payload, relayer)
	if err != nil {
		return err
	}

	packetData, err := im.app.UnmarshalPacketData(payload)
	if err != nil {
		return err
	}

	// TODO: Implement custom logics here using packetData.
	packetData = packetData
	return nil
}

// OnTimeoutPacket implements timeout source callbacks for the ibc-callbacks middleware.
// It defers to the underlying application and then calls the contract callback.
// If the contract callback runs out of gas and may be retried with a higher gas limit then the state changes are
// reverted via a panic.
// OnTimeoutPacket is executed when a packet has timed out on the receiving chain.
func (im IBCMiddlewareV2) OnTimeoutPacket(
	ctx sdk.Context,
	sourceClient string,
	destinationClient string,
	sequence uint64,
	payload channeltypesv2.Payload,
	relayer sdk.AccAddress,
) error {
	err := im.app.OnTimeoutPacket(ctx, sourceClient, destinationClient, sequence, payload, relayer)
	if err != nil {
		return err
	}

	packetData, err := im.app.UnmarshalPacketData(payload)
	if err != nil {
		return err
	}

	// TODO: Implement custom logics here using packetData.
	packetData = packetData
	return nil
}

// WriteAcknowledgement implements the ReceivePacket destination callbacks for the ibc-callbacks middleware
// during asynchronous packet acknowledgement.
// It defers to the underlying application and then calls the contract callback.
// If the contract callback runs out of gas and may be retried with a higher gas limit then the state changes are
// reverted via a panic.
func (im IBCMiddlewareV2) WriteAcknowledgement(
	ctx sdk.Context,
	clientID string,
	sequence uint64,
	ack channeltypesv2.Acknowledgement,
) error {
	packet, found := im.chanKeeperV2.GetAsyncPacket(ctx, clientID, sequence)
	if !found {
		return errorsmod.Wrapf(channeltypesv2.ErrInvalidAcknowledgement, "async packet not found for clientID (%s) and sequence (%d)", clientID, sequence)
	}
	err := im.writeAckWrapper.WriteAcknowledgement(ctx, clientID, sequence, ack)
	if err != nil {
		return err
	}

	// NOTE: use first payload as the payload that is being handled by callbacks middleware
	// must reconsider if multipacket data gets supported with async packets
	// TRACKING ISSUE: https://github.com/cosmos/ibc-go/issues/7950
	if len(packet.Payloads) != 1 {
		return errorsmod.Wrapf(channeltypesv2.ErrInvalidAcknowledgement, "async packet has multiple payloads")
	}
	payload := packet.Payloads[0]

	packetData, err := im.app.UnmarshalPacketData(payload)
	if err != nil {
		return err
	}

	// TODO: Implement custom logics here using packetData.
	packetData = packetData
	return nil
}
