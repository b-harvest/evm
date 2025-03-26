package ibctesting

import (
	"encoding/json"

	"cosmossdk.io/log"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	ibctesting "github.com/cosmos/ibc-go/v10/testing"

	exampleapp "github.com/cosmos/evm/example_chain"
	"github.com/cosmos/evm/example_chain/testutil"
)

func SetupExampleApp() (ibctesting.TestingApp, map[string]json.RawMessage) {
	app := exampleapp.NewExampleApp(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		true,
		simtestutil.EmptyAppOptions{},
		testutil.NoOpEvmAppOptions,
	)
	return app, app.DefaultGenesis()
}
