package types

import (
	"strings"
	"testing"

	"github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/suite"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type GenesisTestSuite struct {
	suite.Suite

	address string
	hash    common.Hash
	code    string

	validReceiptsRoot   string
	invalidReceiptsRoot string
}

func (suite *GenesisTestSuite) SetupTest() {
	priv, err := ethsecp256k1.GenerateKey()
	suite.Require().NoError(err)

	suite.address = common.BytesToAddress(priv.PubKey().Address().Bytes()).String()
	suite.hash = common.BytesToHash([]byte("hash"))
	suite.code = common.Bytes2Hex([]byte{1, 2, 3})

	suite.validReceiptsRoot = ethtypes.EmptyRootHash.String()
	suite.invalidReceiptsRoot = "0x" + strings.Repeat("0", 63)
}

func TestGenesisTestSuite(t *testing.T) {
	suite.Run(t, new(GenesisTestSuite))
}

func (suite *GenesisTestSuite) TestValidateGenesisAccount() {
	testCases := []struct {
		name           string
		genesisAccount GenesisAccount
		expPass        bool
	}{
		{
			"valid genesis account",
			GenesisAccount{
				Address: suite.address,
				Code:    suite.code,
				Storage: Storage{
					NewState(suite.hash, suite.hash),
				},
			},
			true,
		},
		{
			"empty account address bytes",
			GenesisAccount{
				Address: "",
				Code:    suite.code,
				Storage: Storage{
					NewState(suite.hash, suite.hash),
				},
			},
			false,
		},
		{
			"empty code bytes",
			GenesisAccount{
				Address: suite.address,
				Code:    "",
				Storage: Storage{
					NewState(suite.hash, suite.hash),
				},
			},
			true,
		},
	}

	for _, tc := range testCases {
		err := tc.genesisAccount.Validate()
		if tc.expPass {
			suite.Require().NoError(err, tc.name)
		} else {
			suite.Require().Error(err, tc.name)
		}
	}
}

func (suite *GenesisTestSuite) TestValidateGenesis() {
	defaultGenesis := DefaultGenesisState()

	testCases := []struct {
		name     string
		genState *GenesisState
		expPass  bool
	}{
		{
			name:     "default",
			genState: DefaultGenesisState(),
			expPass:  true,
		},
		{
			name: "valid genesis",
			genState: &GenesisState{
				Accounts: []GenesisAccount{
					{
						Address: suite.address,

						Code: suite.code,
						Storage: Storage{
							{Key: suite.hash.String()},
						},
					},
				},
				ReceiptsRoot: suite.validReceiptsRoot,
				Params:       DefaultParams(),
			},
			expPass: true,
		},
		{
			name:     "copied genesis",
			genState: NewGenesisState(defaultGenesis.Params, defaultGenesis.Accounts, defaultGenesis.ReceiptsRoot),
			expPass:  true,
		},
		{
			name: "invalid genesis account",
			genState: &GenesisState{
				Accounts: []GenesisAccount{
					{
						Address: "123456",

						Code: suite.code,
						Storage: Storage{
							{Key: suite.hash.String()},
						},
					},
				},
				ReceiptsRoot: suite.validReceiptsRoot,
				Params:       DefaultParams(),
			},
			expPass: false,
		},
		{
			name: "duplicated genesis account",
			genState: &GenesisState{
				Accounts: []GenesisAccount{
					{
						Address: suite.address,

						Code: suite.code,
						Storage: Storage{
							NewState(suite.hash, suite.hash),
						},
					},
					{
						Address: suite.address,

						Code: suite.code,
						Storage: Storage{
							NewState(suite.hash, suite.hash),
						},
					},
				},
			},
			expPass: false,
		},
		{
			name: "invalid receipts root",
			genState: &GenesisState{
				Accounts: []GenesisAccount{
					{
						Address: suite.address,

						Code: suite.code,
						Storage: Storage{
							{Key: suite.hash.String()},
						},
					},
				},
				ReceiptsRoot: suite.invalidReceiptsRoot,
				Params:       DefaultParams(),
			},
			expPass: false,
		},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			err := tc.genState.Validate()
			if tc.expPass {
				suite.Require().NoError(err, tc.name)
			} else {
				suite.Require().Error(err, tc.name)
			}
		})
	}
}
