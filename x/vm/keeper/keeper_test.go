package keeper_test

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/cosmos/evm/contracts"
	"github.com/cosmos/evm/testutil/integration/os/factory"
	"github.com/cosmos/evm/utils"
	"github.com/cosmos/evm/x/vm/statedb"
	"github.com/cosmos/evm/x/vm/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

func (suite *KeeperTestSuite) TestBaseFee() {
	testCases := []struct {
		name            string
		enableLondonHF  bool
		enableFeemarket bool
		expectBaseFee   *big.Int
	}{
		{"not enable london HF, not enable feemarket", false, false, nil},
		{"enable london HF, not enable feemarket", true, false, big.NewInt(0)},
		{"enable london HF, enable feemarket", true, true, big.NewInt(1000000000)},
		{"not enable london HF, enable feemarket", false, true, nil},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.enableFeemarket = tc.enableFeemarket
			suite.enableLondonHF = tc.enableLondonHF
			suite.SetupTest()

			baseFee := suite.network.App.EVMKeeper.GetBaseFee(suite.network.GetContext())
			suite.Require().Equal(tc.expectBaseFee, baseFee)
		})
	}
	suite.enableFeemarket = false
	suite.enableLondonHF = true
}

func (suite *KeeperTestSuite) TestGetAccountStorage() {
	var ctx sdk.Context
	testCases := []struct {
		name     string
		malleate func() common.Address
	}{
		{
			name:     "Only accounts that are not a contract (no storage)",
			malleate: nil,
		},
		{
			name: "One contract (with storage) and other EOAs",
			malleate: func() common.Address {
				supply := big.NewInt(100)
				contractAddr := suite.DeployTestContract(suite.T(), ctx, suite.keyring.GetAddr(0), supply)
				return contractAddr
			},
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			suite.SetupTest()
			ctx = suite.network.GetContext()

			var contractAddr common.Address
			if tc.malleate != nil {
				contractAddr = tc.malleate()
			}

			i := 0
			suite.network.App.AccountKeeper.IterateAccounts(ctx, func(account sdk.AccountI) bool {
				acc, ok := account.(*authtypes.BaseAccount)
				if !ok {
					// Ignore e.g. module accounts
					return false
				}

				address, err := utils.Bech32ToHexAddr(acc.Address)
				if err != nil {
					// NOTE: we panic in the test to see any potential problems
					// instead of skipping to the next account
					panic(fmt.Sprintf("failed to convert %s to hex address", err))
				}

				storage := suite.network.App.EVMKeeper.GetAccountStorage(ctx, address)

				if address == contractAddr {
					suite.Require().NotEqual(0, len(storage),
						"expected account %d to have non-zero amount of storage slots, got %d",
						i, len(storage),
					)
				} else {
					suite.Require().Len(storage, 0,
						"expected account %d to have %d storage slots, got %d",
						i, 0, len(storage),
					)
				}

				i++
				return false
			})
		})
	}
}

func (suite *KeeperTestSuite) TestGetAccountOrEmpty() {
	ctx := suite.network.GetContext()
	empty := statedb.Account{
		Balance:  new(big.Int),
		CodeHash: evmtypes.EmptyCodeHash,
	}

	supply := big.NewInt(100)
	contractAddr := suite.DeployTestContract(suite.T(), ctx, suite.keyring.GetAddr(0), supply)

	testCases := []struct {
		name     string
		addr     common.Address
		expEmpty bool
	}{
		{
			"unexisting account - get empty",
			common.Address{},
			true,
		},
		{
			"existing contract account",
			contractAddr,
			false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			res := suite.network.App.EVMKeeper.GetAccountOrEmpty(ctx, tc.addr)
			if tc.expEmpty {
				suite.Require().Equal(empty, res)
			} else {
				suite.Require().NotEqual(empty, res)
			}
		})
	}
}

func (suite *KeeperTestSuite) TestGetReceiptTransient() {
	// 1. deploy erc20 contract
	user1Key := suite.keyring.GetKey(0)
	constructorArgs := []interface{}{"coin", "token", uint8(18)}
	compiledContract := contracts.ERC20MinterBurnerDecimalsContract
	txArgs := evmtypes.EvmTxArgs{
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(1),
	}

	contractAddr, err := suite.factory.DeployContract(
		user1Key.Priv,
		txArgs,
		factory.ContractDeploymentData{
			Contract:        compiledContract,
			ConstructorArgs: constructorArgs,
		},
	)
	suite.Require().NoError(err)
	suite.Require().NotEqual(common.Address{}, contractAddr)

	err = suite.network.NextBlock()
	suite.Require().NoError(err)

	// 2. Mint erc20 token
	transferAmount := int64(1000)
	result, err := suite.MintERC20Token(user1Key.Priv, contractAddr, user1Key.Addr, big.NewInt(transferAmount))
	suite.Require().NoError(err)
	suite.Require().True(result.IsOK(), "transaction should have succeeded", result.GetLog())

	res, err := suite.factory.GetEvmTransactionResponseFromTxResult(result)
	suite.Require().NoError(err)

	logs := types.LogsToEthereum(res.Logs)
	bloom := ethtypes.BytesToBloom(ethtypes.LogsBloom(logs))

	// 3. Get receipt from transient store and compare
	receiptTransient, err := suite.network.App.EVMKeeper.GetReceiptTransient(suite.network.GetContext(), common.HexToHash(res.Hash))
	suite.Require().NoError(err)
	suite.Require().Equal(uint8(ethtypes.DynamicFeeTxType), receiptTransient.Type)
	suite.Require().Equal(ethtypes.ReceiptStatusSuccessful, receiptTransient.Status)
	suite.Require().Equal(res.GasUsed, receiptTransient.CumulativeGasUsed)
	suite.Require().Equal(bloom, receiptTransient.Bloom)
	for i, log := range logs {
		// Set log.TxHash, log.lockHash, log.BlockNumber to empty values
		// It is because the method used for storing receipt, rlp.EncodeToBytes does not encode these fields
		// rlp.EncodeToBytes only encode Address, Topics, Data for ethereum log
		log.TxHash = common.Hash{}
		log.BlockHash = common.Hash{}
		log.BlockNumber = 0
		suite.Require().Equal(log, receiptTransient.Logs[i])
	}
	// Set TxHash, ContractAddress, GasUsed to empty values
	// It is also because the method used for storing receipt, rlp.EncodeToBytes does not encode these fields
	// rlp.EncodeToBytes only encode PostStateOrStatus, CumulativeGasUsed, Bloom, Logs for ethereum receipt
	suite.Require().Equal(common.Hash{}, receiptTransient.TxHash)
	suite.Require().Equal(common.Address{}, receiptTransient.ContractAddress)
	suite.Require().Equal(uint64(0), receiptTransient.GasUsed)

	// 4. MarshalBinary and compare
	bz1, err := receiptTransient.MarshalBinary()
	suite.Require().NoError(err)

	receipt := &ethtypes.Receipt{
		Type:              uint8(ethtypes.DynamicFeeTxType),
		Status:            ethtypes.ReceiptStatusSuccessful,
		CumulativeGasUsed: res.GasUsed,
		Bloom:             bloom,
		Logs:              logs,
	}
	bz2, err := receipt.MarshalBinary()
	suite.Require().NoError(err)
	suite.Require().Equal(bz1, bz2)
}

func (suite *KeeperTestSuite) TestGetReceiptsTransientWithERC20Mint() {

	// 1. deploy erc20 contract
	user1Key := suite.keyring.GetKey(0)
	constructorArgs := []interface{}{"coin", "token", uint8(18)}
	compiledContract := contracts.ERC20MinterBurnerDecimalsContract
	txArgs := evmtypes.EvmTxArgs{
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(1),
	}

	contractAddr, err := suite.factory.DeployContract(
		user1Key.Priv,
		txArgs,
		factory.ContractDeploymentData{
			Contract:        compiledContract,
			ConstructorArgs: constructorArgs,
		},
	)
	suite.Require().NoError(err)
	suite.Require().NotEqual(common.Address{}, contractAddr)

	err = suite.network.NextBlock()
	suite.Require().NoError(err)

	// 2. Mint erc20 token
	transferAmount := int64(1000)
	result, err := suite.MintERC20Token(user1Key.Priv, contractAddr, user1Key.Addr, big.NewInt(transferAmount))
	suite.Require().NoError(err)
	suite.Require().True(result.IsOK(), "transaction should have succeeded", result.GetLog())

	res, err := suite.factory.GetEvmTransactionResponseFromTxResult(result)
	logs := types.LogsToEthereum(res.Logs)
	bloom := ethtypes.BytesToBloom(ethtypes.LogsBloom(logs))
	receipt := &ethtypes.Receipt{
		Type:              uint8(ethtypes.DynamicFeeTxType),
		Status:            1,
		CumulativeGasUsed: uint64(res.GasUsed),
		Logs:              logs,
		Bloom:             bloom,
	}
	err = suite.network.NextBlock()
	suite.Require().NoError(err)

	// 3. Get receiptsRoot
	receiptsRoot, err := s.network.App.EVMKeeper.GetReceiptsRoot(suite.network.GetContext(), suite.network.GetContext().BlockHeight())
	suite.Require().NoError(err)

	// 4. Check receiptsRoot == ethtype.DepriveSha(Receipts)
	receipts := []*ethtypes.Receipt{receipt}
	expReceiptsRoot := ethtypes.DeriveSha(ethtypes.Receipts(receipts), trie.NewStackTrie(nil))

	suite.Require().Equal(expReceiptsRoot, receiptsRoot)
}

func (suite *KeeperTestSuite) TestGetReceiptsTransientWithNativeSend() {

	// 1. Transfer token 2 times in 1 block
	user1Key := suite.keyring.GetKey(0)
	user2Key := suite.keyring.GetKey(1)
	transferAmount := int64(1000)

	// Taking custom args from the table entry
	txArgs := evmtypes.EvmTxArgs{
		GasTipCap: big.NewInt(0),
		GasFeeCap: big.NewInt(1),
		Amount:    big.NewInt(transferAmount),
		To:        &user2Key.Addr,
	}
	result1, err := suite.factory.ExecuteEthTx(user1Key.Priv, txArgs)
	suite.Require().NoError(err)
	suite.Require().True(result1.IsOK(), "transaction should have succeeded", result1.GetLog())

	txArgs.To = &user1Key.Addr
	result2, err := suite.factory.ExecuteEthTx(user2Key.Priv, txArgs)
	suite.Require().NoError(err)
	suite.Require().True(result2.IsOK(), "transaction should have succeeded", result2.GetLog())

	err = suite.network.NextBlock()
	suite.Require().NoError(err)

	// 2. Get receiptsRoot
	receiptsRoot, err := s.network.App.EVMKeeper.GetReceiptsRoot(suite.network.GetContext(), suite.network.GetContext().BlockHeight())
	suite.Require().NoError(err)

	// 3. Check receiptsRoot == ethtype.DepriveSha(Receipts)
	res1, err := suite.factory.GetEvmTransactionResponseFromTxResult(result1)
	suite.Require().NoError(err)
	logs := types.LogsToEthereum(res1.Logs)
	bloom := ethtypes.BytesToBloom(ethtypes.LogsBloom(logs))
	receipt1 := &ethtypes.Receipt{
		Type:              uint8(ethtypes.DynamicFeeTxType),
		Status:            1,
		CumulativeGasUsed: uint64(res1.GasUsed),
		Logs:              logs,
		Bloom:             bloom,
	}

	res2, err := suite.factory.GetEvmTransactionResponseFromTxResult(result2)
	suite.Require().NoError(err)
	logs = types.LogsToEthereum(res2.Logs)
	bloom = ethtypes.BytesToBloom(ethtypes.LogsBloom(logs))
	receipt2 := &ethtypes.Receipt{
		Type:              ethtypes.DynamicFeeTxType,
		Status:            1,
		CumulativeGasUsed: uint64(res2.GasUsed),
		Logs:              logs,
		Bloom:             bloom,
	}

	receipts := []*ethtypes.Receipt{receipt1, receipt2}
	expReceiptsRoot := ethtypes.DeriveSha(ethtypes.Receipts(receipts), trie.NewStackTrie(nil))

	suite.Require().Equal(expReceiptsRoot, receiptsRoot)
}
