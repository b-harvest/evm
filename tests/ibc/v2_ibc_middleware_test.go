package ibc

import (
	"errors"
	"testing"

	ibctesting "github.com/cosmos/ibc-go/v10/testing"
	ibcmockv2 "github.com/cosmos/ibc-go/v10/testing/mock/v2"
	testifysuite "github.com/stretchr/testify/suite"

	evmibctesting "github.com/cosmos/evm/ibc/testing"
	erc20Keeper "github.com/cosmos/evm/x/erc20/keeper"
	"github.com/cosmos/evm/x/erc20/v2"
)

// MiddlewareTestSuite tests the v2 IBC middleware for the ERC20 module.
type MiddlewareV2TestSuite struct {
	testifysuite.Suite

	coordinator *ibctesting.Coordinator

	// testing chains used for convenience and readability
	evmChainA *ibctesting.TestChain
	chainB    *ibctesting.TestChain

	// chainB to evmChainA for testing OnRecvPacket, OnAckPacket, and OnTimeoutPacket
	pathBToA *ibctesting.Path
}

func (suite *MiddlewareV2TestSuite) SetupTest() {
	suite.coordinator = evmibctesting.NewCoordinator(suite.T(), 1, 2)
	suite.evmChainA = suite.coordinator.GetChain(ibctesting.GetChainID(1))
	suite.chainB = suite.coordinator.GetChain(ibctesting.GetChainID(2))

	// setup between chainB and evmChainA
	// pathBToA.EndpointA = endpoint on chainB
	// pathBToA.EndpointB = endpoint on evmChainA
	suite.pathBToA = ibctesting.NewPath(suite.chainB, suite.evmChainA)

	// setup IBC v2 paths between the chains
	suite.pathBToA.SetupV2()
}

func TestMiddlewareV2TestSuite(t *testing.T) {
	testifysuite.Run(t, new(MiddlewareV2TestSuite))
}

func (s *MiddlewareV2TestSuite) TestNewIBCMiddleware() {
	testCases := []struct {
		name          string
		instantiateFn func()
		expError      error
	}{
		{
			"success",
			func() {
				_ = v2.NewIBCMiddleware(ibcmockv2.IBCModule{}, erc20Keeper.Keeper{})
			},
			nil,
		},
		{
			"panics with nil underlying app",
			func() {
				_ = v2.NewIBCMiddleware(nil, erc20Keeper.Keeper{})
			},
			errors.New("underlying application cannot be nil"),
		},
		{
			"panics with nil erc20 keeper",
			func() {
				_ = v2.NewIBCMiddleware(ibcmockv2.IBCModule{}, nil)
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
