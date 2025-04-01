package ibc

import (
	"errors"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"
	testifysuite "github.com/stretchr/testify/suite"

	"github.com/cosmos/evm/evmd"
	"github.com/cosmos/evm/ibc"
	evmibctesting "github.com/cosmos/evm/ibc/testing"
	"github.com/cosmos/evm/testutil"
	"github.com/cosmos/evm/x/erc20"
	erc20Keeper "github.com/cosmos/evm/x/erc20/keeper"
	"github.com/cosmos/evm/x/erc20/types"
)

// MiddlewareTestSuite tests the IBC middleware for the ERC20 module.
type MiddlewareTestSuite struct {
	testifysuite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	evmChainA *ibctesting.TestChain
	chainB    *ibctesting.TestChain

	pathAToB *ibctesting.Path
	pathBToA *ibctesting.Path
}

func (s *MiddlewareTestSuite) SetupTest() {
	s.coordinator = evmibctesting.NewCoordinator(s.T(), 1, 2)
	s.evmChainA = s.coordinator.GetChain(ibctesting.GetChainID(1))
	s.chainB = s.coordinator.GetChain(ibctesting.GetChainID(2))

	s.pathAToB = ibctesting.NewPath(s.evmChainA, s.chainB)
	s.pathAToB.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathAToB.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathAToB.EndpointA.ChannelConfig.Version = transfertypes.V1
	s.pathAToB.EndpointB.ChannelConfig.Version = transfertypes.V1
	s.pathAToB.Setup()

	s.pathBToA = ibctesting.NewPath(s.chainB, s.evmChainA)
	s.pathBToA.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathBToA.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathBToA.EndpointA.ChannelConfig.Version = transfertypes.V1
	s.pathBToA.EndpointB.ChannelConfig.Version = transfertypes.V1
	s.pathBToA.Setup()
}

func TestMiddlewareTestSuite(t *testing.T) {
	testifysuite.Run(t, new(MiddlewareTestSuite))
}

func (s *MiddlewareTestSuite) TestNewIBCMiddleware() {
	testCases := []struct {
		name          string
		instantiateFn func()
		expError      error
	}{
		{
			"success",
			func() {
				_ = erc20.NewIBCMiddleware(erc20Keeper.Keeper{}, ibc.Module{})
			},
			nil,
		},
		{
			"panics with nil underlying app",
			func() {
				_ = erc20.NewIBCMiddleware(erc20Keeper.Keeper{}, nil)
			},
			errors.New("underlying application cannot be nil"),
		},
		{
			"panics with nil erc20 keeper",
			func() {
				_ = erc20.NewIBCMiddleware(nil, ibc.Module{})
			},
			errors.New("erc20 keeper cannot be nil"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			if tc.expError == nil {
				s.Require().NotPanics(
					tc.instantiateFn,
					"unexpected panic: NewIBCMiddleware",
				)
			} else {
				s.Require().PanicsWithError(
					tc.expError.Error(),
					tc.instantiateFn,
					"expected panic with error: ", tc.expError.Error(),
				)
			}
		})
	}
}

func (s *MiddlewareTestSuite) TestOnRecvPacket() {
	var (
		ctx    sdk.Context
		packet channeltypes.Packet
	)

	testCases := []struct {
		name     string
		malleate func()
		expError string
	}{
		{
			name:     "pass",
			malleate: nil,
			expError: "",
		},
		{
			name: "fail: malformed packet data",
			malleate: func() {
				packet.Data = []byte("malformed data")
			},
			expError: "handling packet",
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()

			ctx = s.chainB.GetContext()
			bondDenom, err := s.chainB.GetSimApp().StakingKeeper.BondDenom(ctx)
			s.Require().NoError(err)
			sendAmt := ibctesting.DefaultCoinAmount
			receiver := s.evmChainA.SenderAccount.GetAddress()
			packetData := transfertypes.NewFungibleTokenPacketData(
				bondDenom,
				sendAmt.String(),
				s.chainB.SenderAccount.GetAddress().String(),
				receiver.String(),
				"",
			)
			packet = channeltypes.Packet{
				Sequence:           1,
				SourcePort:         s.pathBToA.EndpointB.ChannelConfig.PortID,
				SourceChannel:      s.pathBToA.EndpointB.ChannelID,
				DestinationPort:    s.pathBToA.EndpointA.ChannelConfig.PortID,
				DestinationChannel: s.pathBToA.EndpointA.ChannelID,
				Data:               packetData.GetBytes(),
				TimeoutHeight:      s.evmChainA.GetTimeoutHeight(),
				TimeoutTimestamp:   0,
			}

			if tc.malleate != nil {
				tc.malleate()
			}

			transferStack, ok := s.evmChainA.App.GetIBCKeeper().PortKeeper.Route(transfertypes.ModuleName)
			s.Require().True(ok)

			ctx = s.evmChainA.GetContext()
			sourceChan := s.pathBToA.EndpointB.GetChannel()
			onRecvPacket := func() ibcexported.Acknowledgement {
				return transferStack.OnRecvPacket(
					ctx,
					sourceChan.Version,
					packet,
					s.evmChainA.SenderAccount.GetAddress())
			}

			ack := onRecvPacket()
			if tc.expError == "" {
				s.Require().True(ack.Success())
				// make sure voucher coins are sent to the receiver
				data, ackErr := transfertypes.UnmarshalPacketData(packetData.GetBytes(), sourceChan.Version, "")
				s.Require().Nil(ackErr)
				voucherDenom := testutil.GetVoucherDenomFromPacketData(data, packet.GetDestPort(), packet.GetDestChannel())
				evmApp := s.evmChainA.App.(*evmd.EVMD)
				voucherCoin := evmApp.BankKeeper.GetBalance(ctx, receiver, voucherDenom)
				s.Require().Equal(sendAmt.String(), voucherCoin.Amount.String())
				// make sure token pair is registered
				tp, err := types.NewTokenPairSTRv2(voucherDenom)
				s.Require().NoError(err)
				tokenPair, found := evmApp.Erc20Keeper.GetTokenPair(ctx, tp.GetID())
				s.Require().True(found)
				s.Require().Equal(voucherDenom, tokenPair.Denom)
			} else {
				s.Require().False(ack.Success())
				acknowledgement, ok := ack.(channeltypes.Acknowledgement)
				s.Require().True(ok)
				ackErr, ok := acknowledgement.Response.(*channeltypes.Acknowledgement_Error)
				s.Require().True(ok)
				s.Require().Contains(ackErr.Error, tc.expError)
			}
		})
	}
}
