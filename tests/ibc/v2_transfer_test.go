// Copied from https://github.com/cosmos/ibc-go/blob/e5baeaea3e549f64055f0a60f2617a3404ea777d/modules/apps/transfer/v2/ibc_module_test.go
// Why was this copied?
// This test suite was copied to verify that ExampleChain (EVM-based chain)
// correctly implements IBC v2 packet handling logic, including send, receive,
// acknowledgement, and timeout flows.
package ibc

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	testifysuite "github.com/stretchr/testify/suite"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	"github.com/cosmos/evm/example_chain"
	evmibctesting "github.com/cosmos/evm/ibc/testing"
)

const testclientid = "testclientid"

type TransferTestSuiteV2 struct {
	testifysuite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	evmChainA *ibctesting.TestChain
	chainB    *ibctesting.TestChain
	chainC    *ibctesting.TestChain

	pathAToB *ibctesting.Path
	pathBToC *ibctesting.Path
}

const invalidPortID = "invalidportid"

func (suite *TransferTestSuiteV2) SetupTest() {
	suite.coordinator = evmibctesting.NewCoordinator(suite.T(), 1, 2)
	suite.evmChainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))
	suite.chainC = suite.coordinator.GetChain(ibctesting.GetChainID(3))

	// setup between evmChainA and chainB
	// NOTE:
	// pathAToB.EndpointA = endpoint on evmChainA
	// pathAToB.EndpointB = endpoint on chainB
	suite.pathAToB = ibctesting.NewPath(suite.evmChainA, suite.chainB)

	// setup between chainB and chainC
	// pathBToC.EndpointA = endpoint on chainB
	// pathBToC.EndpointB = endpoint on chainC
	suite.pathBToC = ibctesting.NewPath(suite.chainB, suite.chainC)

	// setup IBC v2 paths between the chains
	suite.pathAToB.SetupV2()
	suite.pathBToC.SetupV2()
}

func TestTransferTestSuiteV2(t *testing.T) {
	testifysuite.Run(t, new(TransferTestSuiteV2))
}

func (suite *TransferTestSuiteV2) TestOnSendPacket() {
	var payload channeltypesv2.Payload
	testCases := []struct {
		name                  string
		sourceDenomToTransfer string
		malleate              func()
		expError              error
	}{
		{
			"transfer single denom",
			sdk.DefaultBondDenom,
			func() {},
			nil,
		},
		{
			"transfer with invalid source port",
			sdk.DefaultBondDenom,
			func() {
				payload.SourcePort = invalidPortID
			},
			channeltypesv2.ErrInvalidPacket,
		},
		{
			"transfer with invalid destination port",
			sdk.DefaultBondDenom,
			func() {
				payload.DestinationPort = invalidPortID
			},
			channeltypesv2.ErrInvalidPacket,
		},
		{
			"transfer with invalid source client",
			sdk.DefaultBondDenom,
			func() {
				suite.pathAToB.EndpointA.ClientID = testclientid
			},
			channeltypesv2.ErrInvalidPacket,
		},
		{
			"transfer with invalid destination client",
			sdk.DefaultBondDenom,
			func() {
				suite.pathAToB.EndpointB.ClientID = testclientid
			},
			channeltypesv2.ErrInvalidPacket,
		},
		{
			"transfer with slashes in base denom",
			"base/coin",
			func() {},
			types.ErrInvalidDenomForTransfer,
		},
		{
			"transfer with slashes in ibc denom",
			fmt.Sprintf("ibc/%x", sha256.Sum256([]byte("coin"))),
			func() {},
			types.ErrInvalidDenomForTransfer,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			evmApp := suite.evmChainA.App.(*example_chain.ExampleChain)
			originalBalance := evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				tc.sourceDenomToTransfer,
			)

			amount, ok := sdkmath.NewIntFromString("9223372036854775808") // 2^63 (one above int64)
			suite.Require().True(ok)
			originalCoin := sdk.NewCoin(tc.sourceDenomToTransfer, amount)

			token := types.Token{
				Denom:  types.Denom{Base: originalCoin.Denom},
				Amount: originalCoin.Amount.String(),
			}

			transferData := types.NewFungibleTokenPacketData(
				token.Denom.Path(),
				token.Amount,
				suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(),
				"",
			)
			bz := suite.evmChainA.Codec.MustMarshal(&transferData)
			payload = channeltypesv2.NewPayload(
				types.PortID, types.PortID, types.V1,
				types.EncodingProtobuf, bz,
			)

			// malleate payload
			tc.malleate()

			ctx := suite.evmChainA.GetContext()
			cbs := suite.evmChainA.App.GetIBCKeeper().ChannelKeeperV2.Router.Route(ibctesting.TransferPort)

			err := cbs.OnSendPacket(
				ctx,
				suite.pathAToB.EndpointA.ClientID,
				suite.pathAToB.EndpointB.ClientID,
				1,
				payload,
				suite.evmChainA.SenderAccount.GetAddress(),
			)

			if tc.expError != nil {
				suite.Require().Contains(err.Error(), tc.expError.Error())
			} else {
				suite.Require().NoError(err)

				escrowAddress := types.GetEscrowAddress(types.PortID, suite.pathAToB.EndpointA.ClientID)
				// check that the balance for evmChainA is updated
				chainABalance := evmApp.BankKeeper.GetBalance(
					suite.evmChainA.GetContext(),
					suite.evmChainA.SenderAccount.GetAddress(),
					originalCoin.Denom,
				)
				suite.Require().Equal(originalBalance.Amount.Sub(amount).Int64(), chainABalance.Amount.Int64())

				// check that module account escrow address has locked the tokens
				chainAEscrowBalance := evmApp.BankKeeper.GetBalance(
					suite.evmChainA.GetContext(),
					escrowAddress,
					originalCoin.Denom,
				)
				suite.Require().Equal(originalCoin, chainAEscrowBalance)
			}
		})
	}
}

func (suite *TransferTestSuiteV2) TestOnRecvPacket() {
	var payload channeltypesv2.Payload
	testCases := []struct {
		name                  string
		sourceDenomToTransfer string
		malleate              func()
		expErr                bool
	}{
		{
			"transfer single denom",
			sdk.DefaultBondDenom,
			func() {},
			false,
		},
		{
			"transfer with invalid source port",
			sdk.DefaultBondDenom,
			func() {
				payload.SourcePort = invalidPortID
			},
			true,
		},
		{
			"transfer with invalid dest port",
			sdk.DefaultBondDenom,
			func() {
				payload.DestinationPort = invalidPortID
			},
			true,
		},
		{
			"transfer with invalid source client",
			sdk.DefaultBondDenom,
			func() {
				suite.pathAToB.EndpointA.ClientID = testclientid
			},
			true,
		},
		{
			"transfer with invalid destination client",
			sdk.DefaultBondDenom,
			func() {
				suite.pathAToB.EndpointB.ClientID = testclientid
			},
			true,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			evmApp := suite.evmChainA.App.(*example_chain.ExampleChain)
			originalBalance := evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				tc.sourceDenomToTransfer,
			)

			timeoutTimestamp := uint64(suite.chainB.GetContext().BlockTime().Add(time.Hour).Unix())

			amount, ok := sdkmath.NewIntFromString("9223372036854775808") // 2^63 (one above int64)
			suite.Require().True(ok)
			originalCoin := sdk.NewCoin(tc.sourceDenomToTransfer, amount)

			msg := types.NewMsgTransferWithEncoding(
				types.PortID, suite.pathAToB.EndpointA.ClientID,
				originalCoin, suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(), clienttypes.Height{},
				timeoutTimestamp, "", types.EncodingProtobuf,
			)
			_, err := suite.evmChainA.SendMsgs(msg)
			suite.Require().NoError(err) // message committed

			token, err := evmApp.TransferKeeper.TokenFromCoin(suite.evmChainA.GetContext(), originalCoin)
			suite.Require().NoError(err)

			transferData := types.NewFungibleTokenPacketData(
				token.Denom.Path(),
				token.Amount,
				suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(),
				"",
			)
			bz := suite.evmChainA.Codec.MustMarshal(&transferData)
			payload = channeltypesv2.NewPayload(
				types.PortID, types.PortID, types.V1,
				types.EncodingProtobuf, bz,
			)

			ctx := suite.chainB.GetContext()
			cbs := suite.chainB.App.GetIBCKeeper().ChannelKeeperV2.Router.Route(ibctesting.TransferPort)

			// malleate payload after it has been sent but before OnRecvPacket callback is called
			tc.malleate()

			recvResult := cbs.OnRecvPacket(
				ctx, suite.pathAToB.EndpointA.ClientID, suite.pathAToB.EndpointB.ClientID,
				1, payload, suite.chainB.SenderAccount.GetAddress(),
			)

			if !tc.expErr {

				suite.Require().Equal(channeltypesv2.PacketStatus_Success, recvResult.Status)
				suite.Require().Equal(channeltypes.NewResultAcknowledgement([]byte{byte(1)}).Acknowledgement(), recvResult.Acknowledgement)

				escrowAddress := types.GetEscrowAddress(types.PortID, suite.pathAToB.EndpointA.ClientID)
				// check that the balance for evmChainA is updated
				chainABalance := evmApp.BankKeeper.GetBalance(
					suite.evmChainA.GetContext(),
					suite.evmChainA.SenderAccount.GetAddress(),
					originalCoin.Denom,
				)
				suite.Require().Equal(originalBalance.Amount.Sub(amount).Int64(), chainABalance.Amount.Int64())

				// check that module account escrow address has locked the tokens
				chainAEscrowBalance := evmApp.BankKeeper.GetBalance(
					suite.evmChainA.GetContext(),
					escrowAddress,
					originalCoin.Denom,
				)
				suite.Require().Equal(originalCoin, chainAEscrowBalance)

				traceAToB := types.NewHop(types.PortID, suite.pathAToB.EndpointB.ClientID)

				// check that voucher exists on chain B
				chainBDenom := types.NewDenom(originalCoin.Denom, traceAToB)
				chainBBalance := suite.chainB.GetSimApp().BankKeeper.GetBalance(
					suite.chainB.GetContext(),
					suite.chainB.SenderAccount.GetAddress(),
					chainBDenom.IBCDenom(),
				)
				coinSentFromAToB := sdk.NewCoin(chainBDenom.IBCDenom(), amount)
				suite.Require().Equal(coinSentFromAToB, chainBBalance)
			} else {
				suite.Require().Equal(channeltypesv2.PacketStatus_Failure, recvResult.Status)
			}
		})
	}
}

func (suite *TransferTestSuiteV2) TestOnAckPacket() {
	testCases := []struct {
		name                  string
		sourceDenomToTransfer string
	}{
		{
			"transfer single denom",
			sdk.DefaultBondDenom,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			evmApp := suite.evmChainA.App.(*example_chain.ExampleChain)
			originalBalance := evmApp.BankKeeper.GetBalance(suite.evmChainA.GetContext(), suite.evmChainA.SenderAccount.GetAddress(), tc.sourceDenomToTransfer)

			timeoutTimestamp := uint64(suite.chainB.GetContext().BlockTime().Add(time.Hour).Unix())

			amount, ok := sdkmath.NewIntFromString("9223372036854775808") // 2^63 (one above int64)
			suite.Require().True(ok)
			originalCoin := sdk.NewCoin(tc.sourceDenomToTransfer, amount)

			msg := types.NewMsgTransferWithEncoding(
				types.PortID, suite.pathAToB.EndpointA.ClientID,
				originalCoin, suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(), clienttypes.Height{},
				timeoutTimestamp, "", types.EncodingProtobuf,
			)
			_, err := suite.evmChainA.SendMsgs(msg)
			suite.Require().NoError(err) // message committed

			token, err := evmApp.TransferKeeper.TokenFromCoin(suite.evmChainA.GetContext(), originalCoin)
			suite.Require().NoError(err)

			transferData := types.NewFungibleTokenPacketData(
				token.Denom.Path(),
				token.Amount,
				suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(),
				"",
			)
			bz := suite.evmChainA.Codec.MustMarshal(&transferData)
			payload := channeltypesv2.NewPayload(
				types.PortID, types.PortID, types.V1,
				types.EncodingProtobuf, bz,
			)

			ctx := suite.evmChainA.GetContext()
			cbs := evmApp.GetIBCKeeper().ChannelKeeperV2.Router.Route(ibctesting.TransferPort)

			ack := channeltypes.NewResultAcknowledgement([]byte{byte(1)})

			err = cbs.OnAcknowledgementPacket(
				ctx, suite.pathAToB.EndpointA.ClientID, suite.pathAToB.EndpointB.ClientID,
				1, ack.Acknowledgement(), payload, suite.evmChainA.SenderAccount.GetAddress(),
			)
			suite.Require().NoError(err)

			// on successful ack, the tokens sent in packets should still be in escrow
			escrowAddress := types.GetEscrowAddress(types.PortID, suite.pathAToB.EndpointA.ClientID)
			// check that the balance for evmChainA is updated
			chainABalance := evmApp.BankKeeper.GetBalance(suite.evmChainA.GetContext(), suite.evmChainA.SenderAccount.GetAddress(), originalCoin.Denom)
			suite.Require().Equal(originalBalance.Amount.Sub(amount).Int64(), chainABalance.Amount.Int64())

			// check that module account escrow address has locked the tokens
			chainAEscrowBalance := evmApp.BankKeeper.GetBalance(suite.evmChainA.GetContext(), escrowAddress, originalCoin.Denom)
			suite.Require().Equal(originalCoin, chainAEscrowBalance)

			// create a custom error ack and replay the callback to ensure it fails with IBC v2 callbacks
			errAck := channeltypes.NewErrorAcknowledgement(types.ErrInvalidAmount)
			err = cbs.OnAcknowledgementPacket(
				ctx, suite.pathAToB.EndpointA.ClientID, suite.pathAToB.EndpointB.ClientID,
				1, errAck.Acknowledgement(), payload, suite.evmChainA.SenderAccount.GetAddress(),
			)
			suite.Require().Error(err)

			// create the sentinel error ack and replay the callback to ensure the tokens are correctly refunded
			// we can replay the callback here because the replay protection is handled in the IBC handler
			err = cbs.OnAcknowledgementPacket(
				ctx, suite.pathAToB.EndpointA.ClientID, suite.pathAToB.EndpointB.ClientID,
				1, channeltypesv2.ErrorAcknowledgement[:], payload, suite.evmChainA.SenderAccount.GetAddress(),
			)
			suite.Require().NoError(err)

			// on error ack, the tokens sent in packets should be returned to sender
			// check that the balance for evmChainA is refunded
			chainABalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				originalCoin.Denom,
			)
			suite.Require().Equal(originalBalance.Amount, chainABalance.Amount)

			// check that module account escrow address has no tokens
			chainAEscrowBalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				escrowAddress,
				originalCoin.Denom,
			)
			suite.Require().Equal(sdk.NewCoin(originalCoin.Denom, sdkmath.ZeroInt()), chainAEscrowBalance)
		})
	}
}

func (suite *TransferTestSuiteV2) TestOnTimeoutPacket() {
	testCases := []struct {
		name                  string
		sourceDenomToTransfer string
	}{
		{
			"transfer single denom",
			sdk.DefaultBondDenom,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset

			evmApp := suite.evmChainA.App.(*example_chain.ExampleChain)
			originalBalance := evmApp.BankKeeper.GetBalance(suite.evmChainA.GetContext(), suite.evmChainA.SenderAccount.GetAddress(), tc.sourceDenomToTransfer)

			timeoutTimestamp := uint64(suite.chainB.GetContext().BlockTime().Add(time.Hour).Unix())

			amount, ok := sdkmath.NewIntFromString("9223372036854775808") // 2^63 (one above int64)
			suite.Require().True(ok)
			originalCoin := sdk.NewCoin(tc.sourceDenomToTransfer, amount)

			msg := types.NewMsgTransferWithEncoding(
				types.PortID, suite.pathAToB.EndpointA.ClientID,
				originalCoin, suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(), clienttypes.Height{},
				timeoutTimestamp, "", types.EncodingProtobuf,
			)
			_, err := suite.evmChainA.SendMsgs(msg)
			suite.Require().NoError(err) // message committed

			token, err := evmApp.TransferKeeper.TokenFromCoin(suite.evmChainA.GetContext(), originalCoin)
			suite.Require().NoError(err)

			transferData := types.NewFungibleTokenPacketData(
				token.Denom.Path(),
				token.Amount,
				suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(),
				"",
			)
			bz := suite.evmChainA.Codec.MustMarshal(&transferData)
			payload := channeltypesv2.NewPayload(
				types.PortID, types.PortID, types.V1,
				types.EncodingProtobuf, bz,
			)

			// on successful send, the tokens sent in packets should be in escrow
			escrowAddress := types.GetEscrowAddress(types.PortID, suite.pathAToB.EndpointA.ClientID)
			// check that the balance for evmChainA is updated
			chainABalance := evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				originalCoin.Denom,
			)
			suite.Require().Equal(originalBalance.Amount.Sub(amount).Int64(), chainABalance.Amount.Int64())

			// check that module account escrow address has locked the tokens
			chainAEscrowBalance := evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				escrowAddress,
				originalCoin.Denom,
			)
			suite.Require().Equal(originalCoin, chainAEscrowBalance)

			ctx := suite.evmChainA.GetContext()
			cbs := suite.evmChainA.App.GetIBCKeeper().ChannelKeeperV2.Router.Route(ibctesting.TransferPort)

			err = cbs.OnTimeoutPacket(
				ctx, suite.pathAToB.EndpointA.ClientID, suite.pathAToB.EndpointB.ClientID,
				1, payload, suite.evmChainA.SenderAccount.GetAddress(),
			)
			suite.Require().NoError(err)

			// on timeout, the tokens sent in packets should be returned to sender
			// check that the balance for evmChainA is refunded
			chainABalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				originalCoin.Denom,
			)
			suite.Require().Equal(originalBalance.Amount, chainABalance.Amount)

			// check that module account escrow address has no tokens
			chainAEscrowBalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				escrowAddress,
				originalCoin.Denom,
			)
			suite.Require().Equal(sdk.NewCoin(originalCoin.Denom, sdkmath.ZeroInt()), chainAEscrowBalance)
		})
	}
}
