// Copied from https://github.com/cosmos/ibc-go/blob/e5baeaea3e549f64055f0a60f2617a3404ea777d/modules/apps/transfer/v2/ibc_module_test.go
//
// Why was this copied?
// This test suite was copied to verify that ExampleChain (EVM-based chain)
// correctly implements IBC v2 packet handling logic, including send, receive,
// acknowledgement, and timeout flows.
// Additionally, we made slight modifications to confirm ExampleChain's behavior
// as a receiving chain (see TestOnRecvPacket), ensuring that the EVM-based chain
// correctly mints/burns/escrows tokens according to ICS-20 standards.
//
//nolint:gosec // Reason: G115 warnings are safe in test context
package ibc

import (
	"crypto/sha256"
	"fmt"
	"testing"
	//"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	"github.com/ethereum/go-ethereum/common"
	testifysuite "github.com/stretchr/testify/suite"

	"github.com/cosmos/ibc-go/v10/modules/apps/transfer/types"

	"github.com/cosmos/evm/evmd"
	evmibctesting "github.com/cosmos/evm/ibc/testing"
	"github.com/cosmos/evm/precompiles/ics20"
	"github.com/cosmos/evm/testutil/integration/os/factory"
	"github.com/cosmos/evm/testutil/integration/os/grpc"
	testkeyring "github.com/cosmos/evm/testutil/integration/os/keyring"
	"github.com/cosmos/evm/testutil/integration/os/network"
	erc20types "github.com/cosmos/evm/x/erc20/types"
	evmante "github.com/cosmos/evm/x/vm/ante"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	//clienttypes "github.com/cosmos/ibc-go/v10/modules/core/02-client/types"
	//channeltypes "github.com/cosmos/ibc-go/v10/modules/core/04-channel/types"
	channeltypesv2 "github.com/cosmos/ibc-go/v10/modules/core/04-channel/v2/types"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type ICS20TransferTestSuiteV2 struct {
	testifysuite.Suite

	coordinator *evmibctesting.Coordinator

	// testing chains used for convenience and readability
	evmChainA   *evmibctesting.TestChain
	factory     factory.TxFactory
	grpcHandler grpc.Handler
	keyring     testkeyring.Keyring
	bondDenom   string
	precompile  *ics20.Precompile

	chainB *evmibctesting.TestChain
	chainC *evmibctesting.TestChain

	pathAToB *evmibctesting.Path
	pathBToC *evmibctesting.Path

	// chainB to evmChainA for testing OnRecvPacket, OnAckPacket, and OnTimeoutPacket
	pathBToA *evmibctesting.Path
}

func (suite *ICS20TransferTestSuiteV2) SetupTest() {
	suite.coordinator = evmibctesting.NewCoordinator(suite.T(), 1, 2)

	// Set EVM chain with precompile
	suite.evmChainA = suite.coordinator.GetChain(evmibctesting.GetChainID(1))

	//erc20types.ModuleAddress
	//suite.evmChainA.SenderAccount =
	fmt.Println(evmtypes.GetEVMCoinDenom())

	keyring := testkeyring.New(2)
	nw := network.NewUnitTestNetwork(
		network.WithPreFundedAccounts(keyring.GetAllAccAddrs()...),
	)

	grpcHandler := grpc.NewIntegrationHandler(nw)
	txFactory := factory.New(nw, grpcHandler)

	ctx := nw.GetContext()
	sk := nw.App.StakingKeeper
	bondDenom, err := sk.BondDenom(ctx)
	if err != nil {
		panic(err)
	}

	suite.bondDenom = bondDenom
	suite.factory = txFactory
	suite.grpcHandler = grpcHandler
	suite.keyring = keyring
	//suite.network = nw

	evmApp := suite.evmChainA.App.(*evmd.EVMD)

	// TODO: consider use app's precompiles[ibcTransferPrecompile.Address()]
	if suite.precompile, err = ics20.NewPrecompile(
		*evmApp.StakingKeeper,
		evmApp.TransferKeeper,
		evmApp.IBCKeeper.ChannelKeeper,
		evmApp.AuthzKeeper, // TODO: To be deprecated,
		evmApp.EVMKeeper,
	); err != nil {
		panic(err)
	}

	suite.chainB = suite.coordinator.GetChain(evmibctesting.GetChainID(2))
	suite.chainC = suite.coordinator.GetChain(evmibctesting.GetChainID(3))

	// setup between evmChainA and chainB
	// NOTE:
	// pathAToB.EndpointA = endpoint on evmChainA
	// pathAToB.EndpointB = endpoint on chainB
	suite.pathAToB = evmibctesting.NewPath(suite.evmChainA, suite.chainB)

	// setup between chainB and chainC
	// pathBToC.EndpointA = endpoint on chainB
	// pathBToC.EndpointB = endpoint on chainC
	suite.pathBToC = evmibctesting.NewPath(suite.chainB, suite.chainC)

	// setup between chainB and evmChainA
	// pathBToA.EndpointA = endpoint on chainB
	// pathBToA.EndpointB = endpoint on evmChainA
	suite.pathBToA = evmibctesting.NewPath(suite.chainB, suite.evmChainA)

	// setup IBC v2 paths between the chains

	// v1
	suite.pathAToB = evmibctesting.NewTransferPath(suite.evmChainA, suite.chainB)
	suite.pathAToB.Setup()
	traceAToB := types.NewHop(suite.pathAToB.EndpointB.ChannelConfig.PortID, suite.pathAToB.EndpointB.ChannelID)

	//suite.pathAToB.Setup()
	//suite.pathBToC.Setup()
	//suite.pathBToA.Setup()
	//suite.pathAToB.SetupV2()
	//suite.pathBToC.SetupV2()
	//suite.pathBToA.SetupV2()
	//pathAToB := evmibctesting.NewTransferPath(suite.evmChainA, suite.chainB)
	//pathAToB.Setup()
	//traceAToB := types.NewHop(suite.pathAToB.EndpointB.ChannelConfig.PortID, suite.pathAToB.EndpointB.ChannelID)
	fmt.Println(traceAToB)

}

func TestICS20TransferTestSuiteV2(t *testing.T) {
	testifysuite.Run(t, new(ICS20TransferTestSuiteV2))
}

// TestOnSendPacket tests ExampleChain's implementation of the OnSendPacket callback.
func (suite *ICS20TransferTestSuiteV2) TestOnSendPacket() {
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

		// TODO: add deploy token, call
		// TODO: add EVM dneom case   contract.CallerAddress != origin && msg.Token.Denom == evmDenom
		// TODO: add normal transfer case
		// TODO: add normal transfer bug pair exist case
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest() // reset
			nativeErc20 := SetupNativeErc20(suite.T(), suite.evmChainA)
			fmt.Println(nativeErc20)
			fmt.Println(nativeErc20.Denom)
			fmt.Println(nativeErc20.ContractAddr)

			evmApp := suite.evmChainA.App.(*evmd.EVMD)
			//contract := vm.NewPrecompile(vm.AccountRef(delegator.Addr), s.precompile, big.NewInt(0), tc.gas)

			ctx := suite.evmChainA.GetContext()
			evmParams := evmApp.EVMKeeper.GetParams(ctx)
			addr := common.HexToAddress(evmtypes.ICS20PrecompileAddress)
			icsPrecompile, found, err := evmApp.EVMKeeper.GetStaticPrecompileInstance(&evmParams, addr)
			suite.Require().NoError(err, "failed to instantiate precompile")
			suite.Require().True(found, "not found precompile")
			fmt.Println(icsPrecompile, found, err)
			fmt.Println(addr)

			sourceDenomToTransfer, err := evmApp.StakingKeeper.BondDenom(suite.evmChainA.GetContext())
			suite.Require().NoError(err)

			msgAmount := evmibctesting.DefaultCoinAmount
			originalBalance := evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				sourceDenomToTransfer,
			)

			timeoutHeight := clienttypes.NewHeight(1, 110)

			originalCoin := sdk.NewCoin(sourceDenomToTransfer, msgAmount)

			sourceAddr := common.BytesToAddress(suite.evmChainA.SenderAccount.GetAddress().Bytes())
			receiverAddr := common.BytesToAddress(suite.chainB.SenderAccount.GetAddress().Bytes())
			// send from evmChainA to chainB
			msg := types.NewMsgTransfer(
				suite.pathAToB.EndpointA.ChannelConfig.PortID,
				suite.pathAToB.EndpointA.ChannelID,
				originalCoin,
				//sourceAddr,
				//receiverAddr,
				suite.evmChainA.SenderAccount.GetAddress().String(),
				suite.chainB.SenderAccount.GetAddress().String(),
				timeoutHeight, 0, "",
			)

			suite.evmChainA.SenderAccount.GetAddress().Bytes()

			// TODO: fix/review format
			//input, err := suite.precompile.Pack(
			//	ics20.TransferMethod,
			//	suite.keyring.GetAddr(0),
			//	msg.SourcePort,
			//	msg.SourceChannel,
			//	msg.Token.Amount,
			//	msg.Sender,
			//	msg.Receiver,
			//	msg.TimeoutHeight,
			//	msg.TimeoutTimestamp,
			//	msg.Memo,
			//)

			// TODO: revert to this
			evmRes, err := evmApp.EVMKeeper.CallEVM(
				ctx,
				suite.precompile.ABI,
				erc20types.ModuleAddress,
				suite.precompile.Address(),
				false,
				"denoms",
				query.PageRequest{
					[]byte{},
					0,
					0,
					false,
					false,
				},
			)
			fmt.Println(evmRes, err)

			originalBalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				tc.sourceDenomToTransfer,
			)
			//originalBalanceRecv := evmApp.BankKeeper.GetBalance(
			//	suite.chainB.GetContext(),
			//	suite.chainB.SenderAccount.GetAddress(),
			//	tc.sourceDenomToTransfer,
			//)
			fmt.Println("balance before", originalBalance)

			ctx = evmante.BuildEvmExecutionCtx(ctx).
				WithGasMeter(storetypes.NewInfiniteGasMeter())
			evmRes, err = evmApp.EVMKeeper.CallEVM(
				ctx,
				suite.precompile.ABI,
				//erc20types.ModuleAddress,
				sourceAddr,
				suite.precompile.Address(),
				true,
				"transfer",
				//suite.keyring.GetAddr(0).String(),
				msg.SourcePort,
				msg.SourceChannel,
				msg.Token.Denom,
				msg.Token.Amount.BigInt(),
				//big.NewInt(msg.Token.Amount.Int64()).String(),
				sourceAddr,
				receiverAddr.String(),
				//msg.Sender,
				//msg.Receiver,
				msg.TimeoutHeight,
				msg.TimeoutTimestamp,
				msg.Memo,
			)
			// <nil> contract call failed: method 'transfer', contract '0x0000000000000000000000000000000000000802': invalid source channel ID : identifier cannot be blank: invalid identifier: evm transaction execution failed [/Users/dongsamb/git-local/evm/x/vm/keeper/call_evm.go:99]
			//contract call failed: method 'transfer', contract '0x0000000000000000000000000000000000000802': origin address 0x47EeB2eac350E1923b8CBDfA4396A077b36E62a0 is not the same as sender address 0x03266a3fd6ba682C443dcB43fDe8242F64afe8A2: evm transaction execution failed [/Users/dongsamb/git-local/evm/x/vm/keeper/call_evm.go:99]
			// <nil> contract call failed: method 'transfer', contract '0x0000000000000000000000000000000000000802': out of gas: evm transaction execution failed [/Users/dongsamb/git-local/evm/x/vm/keeper/call_evm.go:99]

			fmt.Println(evmRes, err)

			originalBalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				tc.sourceDenomToTransfer,
			)
			//originalBalanceRecv := evmApp.BankKeeper.GetBalance(
			//	suite.chainB.GetContext(),
			//	suite.chainB.SenderAccount.GetAddress(),
			//	tc.sourceDenomToTransfer,
			//)
			fmt.Println("balance after", originalBalance)

			//contract := vm.NewPrecompile(vm.AccountRef(caller), s.precompile, big.NewInt(0), uint64(1e6))
			//contract.Input = input
			//
			//contractAddr := contract.Address()
			//// Build and sign Ethereum transaction
			//txArgs := evmtypes.EvmTxArgs{
			//	ChainID:   evmtypes.GetEthChainConfig().ChainID,
			//	Nonce:     0,
			//	To:        &contractAddr,
			//	Amount:    nil,
			//	GasLimit:  100000,
			//	GasPrice:  chainutil.ExampleMinGasPrices.BigInt(),
			//	GasFeeCap: baseFee,
			//	GasTipCap: big.NewInt(1),
			//	Accesses:  &ethtypes.AccessList{},
			//}
			//msg, err := s.factory.GenerateGethCoreMsg(s.keyring.GetPrivKey(0), txArgs)
			//s.Require().NoError(err)
			//
			//// Instantiate config
			//proposerAddress := ctx.BlockHeader().ProposerAddress
			//cfg, err := s.network.App.EVMKeeper.EVMConfig(ctx, proposerAddress)
			//s.Require().NoError(err, "failed to instantiate EVM config")
			//
			//// Instantiate EVM
			//headerHash := ctx.HeaderHash()
			//stDB := statedb.New(
			//	ctx,
			//	s.network.App.EVMKeeper,
			//	statedb.NewEmptyTxConfig(common.BytesToHash(headerHash)),
			//)
			//evm := s.network.App.EVMKeeper.NewEVM(
			//	ctx, msg, cfg, nil, stDB,
			//)
			//
			//precompiles, found, err := s.network.App.EVMKeeper.GetPrecompileInstance(ctx, contractAddr)
			//s.Require().NoError(err, "failed to instantiate precompile")
			//s.Require().True(found, "not found precompile")
			//evm.WithPrecompiles(precompiles.Map, precompiles.Addresses)
			//
			//// Run precompiled contract
			//bz, err := s.precompile.Run(evm, contract, tc.readOnly)

			///// @dev Transfer defines a method for performing an IBC transfer.
			///// @param sourcePort the port on which the packet will be sent
			///// @param sourceChannel the channel by which the packet will be sent
			///// @param denom the denomination of the Coin to be transferred to the receiver
			///// @param amount the amount of the Coin to be transferred to the receiver
			///// @param sender the hex address of the sender
			///// @param receiver the bech32 address of the receiver
			///// @param timeoutHeight the timeout height relative to the current block height.
			///// The timeout is disabled when set to 0
			///// @param timeoutTimestamp the timeout timestamp in absolute nanoseconds since unix epoch.
			///// The timeout is disabled when set to 0
			///// @param memo optional memo
			///// @return nextSequence sequence number of the transfer packet sent
			//function transfer(
			//	string memory sourcePort,
			//	string memory sourceChannel,
			//	string memory denom,
			//	uint256 amount,
			//	address sender,
			//	string memory receiver,
			//	Height memory timeoutHeight,
			//	uint64 timeoutTimestamp,
			//	string memory memo
			//) external returns (uint64 nextSequence);

			//evmApp.EVMKeeper.CallEVMWithData(suite.network.GetContext(), tc.from, &wcosmosEVMContract, data, false)
			//
			//suite.precompile.Precompile.
			//
			originalBalance = evmApp.BankKeeper.GetBalance(
				suite.evmChainA.GetContext(),
				suite.evmChainA.SenderAccount.GetAddress(),
				tc.sourceDenomToTransfer,
			)
			//originalBalanceRecv := evmApp.BankKeeper.GetBalance(
			//	suite.chainB.GetContext(),
			//	suite.chainB.SenderAccount.GetAddress(),
			//	tc.sourceDenomToTransfer,
			//)
			//fmt.Println("balance before", originalBalance)

			amount, ok := sdkmath.NewIntFromString("9223372036854775808") // 2^63 (one above int64)
			suite.Require().True(ok)
			originalCoin = sdk.NewCoin(tc.sourceDenomToTransfer, amount)

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

			//ctx := suite.evmChainA.GetContext()
			cbs := suite.evmChainA.App.GetIBCKeeper().ChannelKeeperV2.Router.Route(evmibctesting.TransferPort)

			err = cbs.OnSendPacket(
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
