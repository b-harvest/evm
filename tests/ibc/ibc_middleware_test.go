package ibc

import (
	"errors"
	"math/big"
	"testing"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	transfertypes "github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	ibcexported "github.com/cosmos/ibc-go/v10/modules/core/exported"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	testifysuite "github.com/stretchr/testify/suite"

	"github.com/cosmos/evm/contracts"
	"github.com/cosmos/evm/evmd"
	"github.com/cosmos/evm/ibc"
	evmibctesting "github.com/cosmos/evm/ibc/testing"
	"github.com/cosmos/evm/testutil"
	"github.com/cosmos/evm/x/erc20"
	erc20Keeper "github.com/cosmos/evm/x/erc20/keeper"
	"github.com/cosmos/evm/x/erc20/types"
	erc20types "github.com/cosmos/evm/x/erc20/types"
)

// MiddlewareTestSuite tests the IBC middleware for the ERC20 module.
type MiddlewareTestSuite struct {
	testifysuite.Suite

	coordinator *evmibctesting.Coordinator

	// testing chains used for convenience and readability
	evmChainA *evmibctesting.TestChain
	chainB    *evmibctesting.TestChain

	pathAToB *evmibctesting.Path
	pathBToA *evmibctesting.Path
}

type NativeErc20Info struct {
	denom        string
	contractAbi  abi.ABI
	contractAddr common.Address
	account      common.Address
	initialBal   *big.Int
}

func (s *MiddlewareTestSuite) SetupTest() {
	s.coordinator = evmibctesting.NewCoordinator(s.T(), 1, 2)
	s.evmChainA = s.coordinator.GetChain(ibctesting.GetChainID(1))
	s.chainB = s.coordinator.GetChain(ibctesting.GetChainID(2))

	s.pathAToB = evmibctesting.NewPath(s.evmChainA, s.chainB)
	s.pathAToB.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathAToB.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathAToB.EndpointA.ChannelConfig.Version = transfertypes.V1
	s.pathAToB.EndpointB.ChannelConfig.Version = transfertypes.V1
	s.pathAToB.Setup()

	s.pathBToA = evmibctesting.NewPath(s.chainB, s.evmChainA)
	s.pathBToA.EndpointA.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathBToA.EndpointB.ChannelConfig.PortID = ibctesting.TransferPort
	s.pathBToA.EndpointA.ChannelConfig.Version = transfertypes.V1
	s.pathBToA.EndpointB.ChannelConfig.Version = transfertypes.V1
	s.pathBToA.Setup()
}

// SetupNativeErc20 sets up a native ERC20 token on evmChainA and registers it with the ERC20 module.
func (s *MiddlewareTestSuite) SetupNativeErc20() *NativeErc20Info {
	evmCtx := s.evmChainA.GetContext()
	evmApp := s.evmChainA.App.(*evmd.EVMD)

	// Scenario: Native ERC20 token transfer from evmChainA to chainB
	deployedContractAddr, err := evmApp.Erc20Keeper.DeployERC20Contract(evmCtx, banktypes.Metadata{
		DenomUnits: []*banktypes.DenomUnit{
			{
				Denom:    "example",
				Exponent: 18,
			},
		},
		Name:   "Example",
		Symbol: "Ex",
	})
	s.Require().NoError(err)
	s.evmChainA.NextBlock()
	// Create a new token pair for the deployed contract.
	_, err = evmApp.Erc20Keeper.RegisterERC20(
		evmCtx,
		&erc20types.MsgRegisterERC20{
			Authority:      authtypes.NewModuleAddress(govtypes.ModuleName).String(),
			Erc20Addresses: []string{deployedContractAddr.Hex()},
		},
	)
	s.Require().NoError(err)

	// evmChainA sender must have `sendAmt` erc20 tokens before sending the token to chainB via IBC.
	contractAbi := contracts.ERC20MinterBurnerDecimalsContract.ABI
	nativeErc20Denom := types.CreateDenom(deployedContractAddr.String())
	sendAmt := evmibctesting.DefaultCoinAmount
	evmChainAAccount := s.evmChainA.SenderAccount.GetAddress()
	// mint native erc20
	_, err = evmApp.EVMKeeper.CallEVM(
		evmCtx, contractAbi, erc20types.ModuleAddress, deployedContractAddr,
		true, "mint", common.BytesToAddress(evmChainAAccount), big.NewInt(sendAmt.Int64()),
	)
	s.Require().NoError(err)
	erc20Bal := evmApp.Erc20Keeper.BalanceOf(evmCtx, contractAbi, deployedContractAddr, common.BytesToAddress(evmChainAAccount))
	s.Require().Equal(sendAmt.String(), erc20Bal.String(), "erc20 balance should be equal to minted sendAmt")

	return &NativeErc20Info{
		denom:        nativeErc20Denom,
		contractAbi:  contractAbi,
		contractAddr: deployedContractAddr,
		account:      common.BytesToAddress(evmChainAAccount),
		initialBal:   sendAmt.BigInt(),
	}
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
			path := s.pathAToB
			packet = channeltypes.Packet{
				Sequence:           1,
				SourcePort:         path.EndpointB.ChannelConfig.PortID,
				SourceChannel:      path.EndpointB.ChannelID,
				DestinationPort:    path.EndpointA.ChannelConfig.PortID,
				DestinationChannel: path.EndpointA.ChannelID,
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
			sourceChan := path.EndpointB.GetChannel()
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

// TestOnRecvPacketNativeErc20 tests the OnRecvPacket method for native ERC20 tokens.
func (s *MiddlewareTestSuite) TestOnRecvPacketNativeErc20() {
	s.SetupTest()
	nativeErc20 := s.SetupNativeErc20()

	evmCtx := s.evmChainA.GetContext()
	evmApp := s.evmChainA.App.(*evmd.EVMD)

	// Scenario: Native ERC20 token transfer from evmChainA to chainB
	// evmChainA sender must have `sendAmt` erc20 tokens before sending the token to chainB via IBC.

	timeoutHeight := clienttypes.NewHeight(1, 110)
	path := s.pathAToB
	chainBAccount := s.chainB.SenderAccount.GetAddress()
	// IBC send from evmChainA to chainB.
	sendAmt := math.NewIntFromBigInt(nativeErc20.initialBal)
	senderEthAddr := nativeErc20.account
	sender := sdk.AccAddress(senderEthAddr.Bytes())
	msg := transfertypes.NewMsgTransfer(
		path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
		sdk.NewCoin(nativeErc20.denom, sendAmt), sender.String(), chainBAccount.String(),
		timeoutHeight, 0, "",
	)
	_, err := s.evmChainA.SendMsgs(msg)
	s.Require().NoError(err) // message committed
	erc20BalAfterIbcTransfer := evmApp.Erc20Keeper.BalanceOf(evmCtx, nativeErc20.contractAbi, nativeErc20.contractAddr, senderEthAddr)
	s.Require().Equal(erc20BalAfterIbcTransfer.String(), new(big.Int).Sub(nativeErc20.initialBal, sendAmt.BigInt()).String(), "erc20 balance should be equal to minted sendAmt - sendAmt after IBC transfer")
	// Check native erc20 token is escrowed on evmChainA for sending to chainB.
	escrowAddr := transfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
	escrowedBal := evmApp.BankKeeper.GetBalance(evmCtx, escrowAddr, nativeErc20.denom)
	s.Require().Equal(escrowedBal.Amount.String(), sendAmt.String(), "escrowed balance should be equal to sendAmt after IBC transfer")

	// chainBNativeErc20Denom is the native erc20 token denom on chainB from evmChainA through IBC.
	chainBNativeErc20Denom := transfertypes.NewDenom(
		nativeErc20.denom,
		transfertypes.NewHop(
			s.pathAToB.EndpointB.ChannelConfig.PortID,
			s.pathAToB.EndpointB.ChannelID,
		),
	)
	receiver := sender // the receiver is the sender on evmChainA
	// Mock the transfer of received native erc20 token by evmChainA to evmChainA.
	// Note that ChainB didn't receive the native erc20 token. We just assume that.
	packetData := transfertypes.NewFungibleTokenPacketData(
		chainBNativeErc20Denom.Path(),
		sendAmt.String(),
		chainBAccount.String(),
		receiver.String(),
		"",
	)
	packet := channeltypes.Packet{
		Sequence:           1,
		SourcePort:         path.EndpointB.ChannelConfig.PortID,
		SourceChannel:      path.EndpointB.ChannelID,
		DestinationPort:    path.EndpointA.ChannelConfig.PortID,
		DestinationChannel: path.EndpointA.ChannelID,
		Data:               packetData.GetBytes(),
		TimeoutHeight:      s.evmChainA.GetTimeoutHeight(),
		TimeoutTimestamp:   0,
	}

	transferStack, ok := s.evmChainA.App.GetIBCKeeper().PortKeeper.Route(transfertypes.ModuleName)
	s.Require().True(ok)

	sourceChan := path.EndpointB.GetChannel()
	onRecvPacket := func() ibcexported.Acknowledgement {
		return transferStack.OnRecvPacket(
			evmCtx,
			sourceChan.Version,
			packet,
			s.evmChainA.SenderAccount.GetAddress())
	}

	ack := onRecvPacket()
	s.Require().True(ack.Success())
	// make sure voucher coins are sent to the sender
	_, ackErr := transfertypes.UnmarshalPacketData(packetData.GetBytes(), sourceChan.Version, "")
	s.Require().Nil(ackErr)
	escrowedBal = evmApp.BankKeeper.GetBalance(evmCtx, escrowAddr, nativeErc20.denom)
	s.Require().True(escrowedBal.IsZero(), "escrowed balance should be un-escrowed after receiving the packet")
}

func (s *MiddlewareTestSuite) TestOnAcknowledgementPacket() {
	var (
		ctx    sdk.Context
		packet channeltypes.Packet
		ack    []byte
	)

	testCases := []struct {
		name           string
		malleate       func()
		onSendRequired bool
		expError       string
	}{
		{
			name:           "pass",
			malleate:       nil,
			onSendRequired: false,
			expError:       "",
		},
		{
			name: "pass: refund escrowed token",
			malleate: func() {
				ackErr := channeltypes.NewErrorAcknowledgement(errors.New("error"))
				ack = ackErr.Acknowledgement()
			},
			onSendRequired: true,
			expError:       "",
		},
		{
			name: "fail: malformed packet data",
			malleate: func() {
				packet.Data = []byte("malformed data")
			},
			onSendRequired: false,
			expError:       "cannot unmarshal ICS-20 transfer packet data",
		},
		{
			name: "fail: empty ack",
			malleate: func() {
				ack = []byte{}
			},
			onSendRequired: false,
			expError:       "cannot unmarshal ICS-20 transfer packet acknowledgement",
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()

			ctx = s.evmChainA.GetContext()
			evmApp := s.evmChainA.App.(*evmd.EVMD)
			bondDenom, err := evmApp.StakingKeeper.BondDenom(ctx)
			s.Require().NoError(err)
			sendAmt := ibctesting.DefaultCoinAmount
			sender := s.evmChainA.SenderAccount.GetAddress()
			receiver := s.chainB.SenderAccount.GetAddress()
			packetData := transfertypes.NewFungibleTokenPacketData(
				bondDenom,
				sendAmt.String(),
				sender.String(),
				receiver.String(),
				"",
			)
			path := s.pathAToB
			packet = channeltypes.Packet{
				Sequence:           1,
				SourcePort:         path.EndpointA.ChannelConfig.PortID,
				SourceChannel:      path.EndpointA.ChannelID,
				DestinationPort:    path.EndpointB.ChannelConfig.PortID,
				DestinationChannel: path.EndpointB.ChannelID,
				Data:               packetData.GetBytes(),
				TimeoutHeight:      s.chainB.GetTimeoutHeight(),
				TimeoutTimestamp:   0,
			}

			ack = channeltypes.NewResultAcknowledgement([]byte{1}).Acknowledgement()

			if tc.malleate != nil {
				tc.malleate()
			}

			transferStack, ok := evmApp.GetIBCKeeper().PortKeeper.Route(transfertypes.ModuleName)
			s.Require().True(ok)

			sourceChan := s.pathAToB.EndpointA.GetChannel()
			onAcknowledgementPacket := func() error {
				return transferStack.OnAcknowledgementPacket(
					ctx,
					sourceChan.Version,
					packet,
					ack,
					receiver,
				)
			}
			if tc.onSendRequired {
				timeoutHeight := clienttypes.NewHeight(1, 110)
				msg := transfertypes.NewMsgTransfer(
					path.EndpointA.ChannelConfig.PortID,
					path.EndpointA.ChannelID,
					sdk.NewCoin(bondDenom, sendAmt),
					sender.String(),
					receiver.String(),
					timeoutHeight, 0, "",
				)
				res, err := s.evmChainA.SendMsgs(msg)
				s.Require().NoError(err) // message committed
				packet, err := ibctesting.ParsePacketFromEvents(res.Events)
				s.Require().NoError(err)

				// relay send
				err = path.RelayPacket(packet)
				s.Require().NoError(err) // relay committed
			}

			err = onAcknowledgementPacket()
			if tc.expError == "" {
				s.Require().NoError(err)
			} else {
				s.Require().Error(err)
				s.Require().Contains(err.Error(), tc.expError)
			}
		})
	}
}

// TestOnAcknowledgementPacketNativeErc20 tests the OnAcknowledgementPacket method for native ERC20 tokens.
func (s *MiddlewareTestSuite) TestOnAcknowledgementPacketNativeErc20() {
	var (
		packet channeltypes.Packet
		ack    []byte
	)

	testCases := []struct {
		name      string
		malleate  func()
		expError  string
		expRefund bool
	}{
		{
			name:      "pass",
			malleate:  nil,
			expError:  "",
			expRefund: false,
		},
		{
			name: "pass: refund escrowed token",
			malleate: func() {
				ackErr := channeltypes.NewErrorAcknowledgement(errors.New("error"))
				ack = ackErr.Acknowledgement()
			},
			expError:  "",
			expRefund: true,
		},
		{
			name: "fail: malformed packet data",
			malleate: func() {
				packet.Data = []byte("malformed data")
			},
			expError:  "cannot unmarshal ICS-20 transfer packet data",
			expRefund: false,
		},
		{
			name: "fail: empty ack",
			malleate: func() {
				ack = []byte{}
			},
			expError:  "cannot unmarshal ICS-20 transfer packet acknowledgement",
			expRefund: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()
			nativeErc20 := s.SetupNativeErc20()

			evmCtx := s.evmChainA.GetContext()
			evmApp := s.evmChainA.App.(*evmd.EVMD)

			// Scenario: Native ERC20 token transfer from evmChainA to chainB
			// evmChainA sender must have `sendAmt` erc20 tokens before sending the token to chainB via IBC.

			timeoutHeight := clienttypes.NewHeight(1, 110)
			path := s.pathAToB
			chainBAccount := s.chainB.SenderAccount.GetAddress()
			// IBC send from evmChainA to chainB.
			sendAmt := math.NewIntFromBigInt(nativeErc20.initialBal)
			senderEthAddr := nativeErc20.account
			sender := sdk.AccAddress(senderEthAddr.Bytes())
			receiver := s.chainB.SenderAccount.GetAddress()
			msg := transfertypes.NewMsgTransfer(
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				sdk.NewCoin(nativeErc20.denom, sendAmt), sender.String(), receiver.String(),
				timeoutHeight, 0, "",
			)
			// escrowCheck is a function that checks the state of the evmChainA after the IBC transfer.
			// sendAmt should be escrowed on evmChainA for sending to chainB.
			// eerc20 balance should be decreased by sendAmt.
			escrowCheck := func() {
				erc20BalAfterIbcTransfer := evmApp.Erc20Keeper.BalanceOf(evmCtx, nativeErc20.contractAbi, nativeErc20.contractAddr, senderEthAddr)
				s.Require().Equal(erc20BalAfterIbcTransfer.String(), new(big.Int).Sub(nativeErc20.initialBal, sendAmt.BigInt()).String(), "erc20 balance should be equal to minted sendAmt - sendAmt after IBC transfer")
				// Check native erc20 token is escrowed on evmChainA for sending to chainB.
				escrowAddr := transfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				escrowedBal := evmApp.BankKeeper.GetBalance(evmCtx, escrowAddr, nativeErc20.denom)
				s.Require().Equal(escrowedBal.Amount.String(), sendAmt.String(), "escrowed balance should be equal to sendAmt after IBC transfer")
			}
			refundCheck := func() {
				// Check native erc20 token is un-escrowed on evmChainA after refund.
				escrowAddr := transfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				escrowedBal := evmApp.BankKeeper.GetBalance(evmCtx, escrowAddr, nativeErc20.denom)
				s.Require().True(escrowedBal.IsZero(), "escrowed balance should be un-escrowed after receiving the packet")

				// Check erc20 balance is same as initial balance after refund.
				erc20BalAfterIbcTransfer := evmApp.Erc20Keeper.BalanceOf(evmCtx, nativeErc20.contractAbi, nativeErc20.contractAddr, senderEthAddr)
				s.Require().Equal(erc20BalAfterIbcTransfer.String(), nativeErc20.initialBal.String(), "erc20 balance should be equal to initial balance after refund")
			}
			_, err := s.evmChainA.SendMsgs(msg)
			s.Require().NoError(err) // message committed
			escrowCheck()

			transferStack, ok := s.evmChainA.App.GetIBCKeeper().PortKeeper.Route(transfertypes.ModuleName)
			s.Require().True(ok)

			packetData := transfertypes.NewFungibleTokenPacketData(
				nativeErc20.denom,
				sendAmt.String(),
				sender.String(),
				chainBAccount.String(),
				"",
			)
			packet = channeltypes.Packet{
				Sequence:           1,
				SourcePort:         path.EndpointA.ChannelConfig.PortID,
				SourceChannel:      path.EndpointA.ChannelID,
				DestinationPort:    path.EndpointB.ChannelConfig.PortID,
				DestinationChannel: path.EndpointB.ChannelID,
				Data:               packetData.GetBytes(),
				TimeoutHeight:      s.chainB.GetTimeoutHeight(),
				TimeoutTimestamp:   0,
			}
			ack = channeltypes.NewResultAcknowledgement([]byte{1}).Acknowledgement()

			if tc.malleate != nil {
				tc.malleate()
			}

			sourceChan := path.EndpointA.GetChannel()
			onAcknowledgementPacket := func() error {
				return transferStack.OnAcknowledgementPacket(
					evmCtx,
					sourceChan.Version,
					packet,
					ack,
					receiver,
				)
			}

			err = onAcknowledgementPacket()
			if tc.expError == "" {
				s.Require().NoError(err)
			} else {
				s.Require().Error(err)
				s.Require().Contains(err.Error(), tc.expError)
			}
			if tc.expRefund {
				refundCheck()
			} else {
				escrowCheck()
			}
		})
	}
}

func (s *MiddlewareTestSuite) TestOnTimeoutPacket() {
	var (
		ctx    sdk.Context
		packet channeltypes.Packet
	)

	testCases := []struct {
		name           string
		malleate       func()
		onSendRequired bool
		expError       string
	}{
		{
			name:           "pass",
			malleate:       nil,
			onSendRequired: true,
			expError:       "",
		},
		{
			name: "fail: malformed packet data",
			malleate: func() {
				packet.Data = []byte("malformed data")
			},
			onSendRequired: false,
			expError:       "cannot unmarshal ICS-20 transfer packet data",
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()

			ctx = s.evmChainA.GetContext()
			evmApp := s.evmChainA.App.(*evmd.EVMD)
			bondDenom, err := evmApp.StakingKeeper.BondDenom(ctx)
			s.Require().NoError(err)
			sendAmt := ibctesting.DefaultCoinAmount
			sender := s.evmChainA.SenderAccount.GetAddress()
			receiver := s.chainB.SenderAccount.GetAddress()
			packetData := transfertypes.NewFungibleTokenPacketData(
				bondDenom,
				sendAmt.String(),
				sender.String(),
				receiver.String(),
				"",
			)
			path := s.pathAToB
			packet = channeltypes.Packet{
				Sequence:           1,
				SourcePort:         path.EndpointA.ChannelConfig.PortID,
				SourceChannel:      path.EndpointA.ChannelID,
				DestinationPort:    path.EndpointB.ChannelConfig.PortID,
				DestinationChannel: path.EndpointB.ChannelID,
				Data:               packetData.GetBytes(),
				TimeoutHeight:      s.chainB.GetTimeoutHeight(),
				TimeoutTimestamp:   0,
			}

			if tc.malleate != nil {
				tc.malleate()
			}

			transferStack, ok := evmApp.GetIBCKeeper().PortKeeper.Route(transfertypes.ModuleName)
			s.Require().True(ok)

			sourceChan := s.pathAToB.EndpointA.GetChannel()
			onTimeoutPacket := func() error {
				return transferStack.OnTimeoutPacket(
					ctx,
					sourceChan.Version,
					packet,
					sender,
				)
			}

			if tc.onSendRequired {
				timeoutHeight := clienttypes.NewHeight(1, 110)
				msg := transfertypes.NewMsgTransfer(
					path.EndpointA.ChannelConfig.PortID,
					path.EndpointA.ChannelID,
					sdk.NewCoin(bondDenom, sendAmt),
					sender.String(),
					receiver.String(),
					timeoutHeight, 0, "",
				)
				res, err := s.evmChainA.SendMsgs(msg)
				s.Require().NoError(err) // message committed
				packet, err := ibctesting.ParsePacketFromEvents(res.Events)
				s.Require().NoError(err)

				// relay send
				err = path.RelayPacket(packet)
				s.Require().NoError(err) // relay committed
			}

			err = onTimeoutPacket()
			if tc.expError == "" {
				s.Require().NoError(err)
			} else {
				s.Require().Error(err)
				s.Require().Contains(err.Error(), tc.expError)
			}
		})
	}
}

// TestOnTimeoutPacketNativeErc20 tests the OnTimeoutPacket method for native ERC20 tokens.
func (s *MiddlewareTestSuite) TestOnTimeoutPacketNativeErc20() {
	var (
		packet channeltypes.Packet
	)

	testCases := []struct {
		name      string
		malleate  func()
		expError  string
		expRefund bool
	}{
		{
			name:      "pass: refund escrowed native erc20 coin",
			malleate:  nil,
			expError:  "",
			expRefund: true,
		},
		{
			name: "fail: malformed packet data",
			malleate: func() {
				packet.Data = []byte("malformed data")
			},
			expError:  "cannot unmarshal ICS-20 transfer packet data",
			expRefund: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Run(tc.name, func() {
			s.SetupTest()
			nativeErc20 := s.SetupNativeErc20()

			evmCtx := s.evmChainA.GetContext()
			evmApp := s.evmChainA.App.(*evmd.EVMD)

			// Scenario: Native ERC20 token transfer from evmChainA to chainB
			// evmChainA sender must have `sendAmt` erc20 tokens before sending the token to chainB via IBC.

			timeoutHeight := clienttypes.NewHeight(1, 110)
			path := s.pathAToB
			chainBAccount := s.chainB.SenderAccount.GetAddress()
			// IBC send from evmChainA to chainB.
			sendAmt := math.NewIntFromBigInt(nativeErc20.initialBal)
			senderEthAddr := nativeErc20.account
			sender := sdk.AccAddress(senderEthAddr.Bytes())
			receiver := s.chainB.SenderAccount.GetAddress()
			msg := transfertypes.NewMsgTransfer(
				path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID,
				sdk.NewCoin(nativeErc20.denom, sendAmt), sender.String(), receiver.String(),
				timeoutHeight, 0, "",
			)
			// escrowCheck is a function that checks the state of the evmChainA after the IBC transfer.
			// sendAmt should be escrowed on evmChainA for sending to chainB.
			// eerc20 balance should be decreased by sendAmt.
			escrowCheck := func() {
				erc20BalAfterIbcTransfer := evmApp.Erc20Keeper.BalanceOf(evmCtx, nativeErc20.contractAbi, nativeErc20.contractAddr, senderEthAddr)
				s.Require().Equal(erc20BalAfterIbcTransfer.String(), new(big.Int).Sub(nativeErc20.initialBal, sendAmt.BigInt()).String(), "erc20 balance should be equal to minted sendAmt - sendAmt after IBC transfer")
				// Check native erc20 token is escrowed on evmChainA for sending to chainB.
				escrowAddr := transfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				escrowedBal := evmApp.BankKeeper.GetBalance(evmCtx, escrowAddr, nativeErc20.denom)
				s.Require().Equal(escrowedBal.Amount.String(), sendAmt.String(), "escrowed balance should be equal to sendAmt after IBC transfer")
			}
			refundCheck := func() {
				// Check native erc20 token is un-escrowed on evmChainA after refund.
				escrowAddr := transfertypes.GetEscrowAddress(path.EndpointA.ChannelConfig.PortID, path.EndpointA.ChannelID)
				escrowedBal := evmApp.BankKeeper.GetBalance(evmCtx, escrowAddr, nativeErc20.denom)
				s.Require().True(escrowedBal.IsZero(), "escrowed balance should be un-escrowed after receiving the packet")

				// Check erc20 balance is same as initial balance after refund.
				erc20BalAfterIbcTransfer := evmApp.Erc20Keeper.BalanceOf(evmCtx, nativeErc20.contractAbi, nativeErc20.contractAddr, senderEthAddr)
				s.Require().Equal(erc20BalAfterIbcTransfer.String(), nativeErc20.initialBal.String(), "erc20 balance should be equal to initial balance after refund")
			}
			_, err := s.evmChainA.SendMsgs(msg)
			s.Require().NoError(err) // message committed
			escrowCheck()

			transferStack, ok := s.evmChainA.App.GetIBCKeeper().PortKeeper.Route(transfertypes.ModuleName)
			s.Require().True(ok)

			packetData := transfertypes.NewFungibleTokenPacketData(
				nativeErc20.denom,
				sendAmt.String(),
				sender.String(),
				chainBAccount.String(),
				"",
			)
			packet = channeltypes.Packet{
				Sequence:           1,
				SourcePort:         path.EndpointA.ChannelConfig.PortID,
				SourceChannel:      path.EndpointA.ChannelID,
				DestinationPort:    path.EndpointB.ChannelConfig.PortID,
				DestinationChannel: path.EndpointB.ChannelID,
				Data:               packetData.GetBytes(),
				TimeoutHeight:      s.chainB.GetTimeoutHeight(),
				TimeoutTimestamp:   0,
			}

			if tc.malleate != nil {
				tc.malleate()
			}

			sourceChan := path.EndpointA.GetChannel()
			onTimeoutPacket := func() error {
				return transferStack.OnTimeoutPacket(
					evmCtx,
					sourceChan.Version,
					packet,
					receiver,
				)
			}

			err = onTimeoutPacket()
			if tc.expError == "" {
				s.Require().NoError(err)
			} else {
				s.Require().Error(err)
				s.Require().Contains(err.Error(), tc.expError)
			}
			if tc.expRefund {
				refundCheck()
			} else {
				escrowCheck()
			}
		})
	}
}
