package ibctesting

import (
	"testing"
	"time"

	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	"github.com/cosmos/evm/example_chain"
)

var globalStartTime = time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)

// NewCoordinator initializes Coordinator with N EVM TestChain's (Cosmos EVM apps) and M Cosmos chains (Simulation Apps)
func NewCoordinator(t *testing.T, nEVMChains, mCosmosChains int) *ibctesting.Coordinator {
	t.Helper()
	chains := make(map[string]*ibctesting.TestChain)
	coord := &ibctesting.Coordinator{
		T:           t,
		CurrentTime: globalStartTime,
	}

	example_chain.EvmAppOptions("cosmos_9001-1")
	for i := 1; i <= nEVMChains; i++ {
		chainID := ibctesting.GetChainID(i)
		// setup EVM chains
		ibctesting.DefaultTestingAppInit = SetupExampleApp
		chains[chainID] = NewTestChain(t, coord, chainID)
	}

	// setup Cosmos chains
	ibctesting.DefaultTestingAppInit = ibctesting.SetupTestingApp
	for j := 1 + nEVMChains; j <= nEVMChains+mCosmosChains; j++ {
		chainID := ibctesting.GetChainID(j)
		chains[chainID] = ibctesting.NewTestChain(t, coord, chainID)
	}

	coord.Chains = chains

	return coord
}
