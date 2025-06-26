package evm

import (
	"math/big"
	"time"

	errorsmod "cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
	evmkeeper "github.com/cosmos/evm/x/vm/keeper"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// MonoDecorator is a single decorator that handles all the prechecks for
// ethereum transactions.
type MonoDecorator struct {
	accountKeeper   anteinterfaces.AccountKeeper
	feeMarketKeeper anteinterfaces.FeeMarketKeeper
	evmKeeper       anteinterfaces.EVMKeeper
	maxGasWanted    uint64
}

// NewEVMMonoDecorator creates the 'mono' decorator, that is used to run the ante handle logic
// for EVM transactions on the chain.
//
// This runs all the default checks for EVM transactions enable through Cosmos EVM.
// Any partner chains can use this in their ante handler logic and build additional EVM
// decorators using the returned DecoratorUtils
func NewEVMMonoDecorator(
	accountKeeper anteinterfaces.AccountKeeper,
	feeMarketKeeper anteinterfaces.FeeMarketKeeper,
	evmKeeper anteinterfaces.EVMKeeper,
	maxGasWanted uint64,
) MonoDecorator {
	return MonoDecorator{
		accountKeeper:   accountKeeper,
		feeMarketKeeper: feeMarketKeeper,
		evmKeeper:       evmKeeper,
		maxGasWanted:    maxGasWanted,
	}
}

// AnteHandle handles the entire decorator chain using a mono decorator.
func (md MonoDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {

	ctx.Logger().Info("starting MonoDecorator", "timestamp(unixnano)", time.Now().UnixNano())
	defer ctx.Logger().Info("finished MonoDecorator", "timestamp(unixnano)", time.Now().UnixNano())
	// 0. Basic validation of the transaction
	var txFeeInfo *txtypes.Fee
	if !ctx.IsReCheckTx() {
		// NOTE: txFeeInfo is associated with the Cosmos stack, not the EVM. For
		// this reason, the fee is represented in the original decimals and
		// should be converted later when used.
		txFeeInfo, err = ValidateTx(tx)
		if err != nil {
			return ctx, err
		}
	}

	ethCfg := evmtypes.GetEthChainConfig()
	evmDenom := evmtypes.GetEVMCoinDenom()

	// 1. setup ctx
	ctx, err = SetupContextAndResetTransientGas(ctx, tx, md.evmKeeper)
	if err != nil {
		return ctx, err
	}

	// 2. get utils
	decUtils, err := NewMonoDecoratorUtils(ctx, md.evmKeeper)
	if err != nil {
		return ctx, err
	}

	// NOTE: the protocol does not support multiple EVM messages currently so
	// this loop will complete after the first message.
	for i, msg := range tx.GetMsgs() {
		ethMsg, txData, err := evmtypes.UnpackEthMsg(msg)
		if err != nil {
			return ctx, err
		}

		feeAmt := txData.Fee()
		gas := txData.GetGas()
		fee := sdkmath.LegacyNewDecFromBigInt(feeAmt)
		gasLimit := sdkmath.LegacyNewDecFromBigInt(new(big.Int).SetUint64(gas))

		// TODO: computation for mempool and global fee can be made using only
		// the price instead of the fee. This would save some computation.
		//
		// 2. mempool inclusion fee
		ctx.Logger().Info("start MonoDecorator/CheckMempoolFee", "timestamp(unixnano)", time.Now().UnixNano())
		if ctx.IsCheckTx() && !simulate {
			// FIX: Mempool dec should be converted
			if err := CheckMempoolFee(fee, decUtils.MempoolMinGasPrice, gasLimit, decUtils.Rules.IsLondon); err != nil {
				return ctx, err
			}
		}
		ctx.Logger().Info("end MonoDecorator/CheckMempoolFee", "timestamp(unixnano)", time.Now().UnixNano())

		if txData.TxType() == ethtypes.DynamicFeeTxType && decUtils.BaseFee != nil {
			// If the base fee is not empty, we compute the effective gas price
			// according to current base fee price. The gas limit is specified
			// by the user, while the price is given by the minimum between the
			// max price paid for the entire tx, and the sum between the price
			// for the tip and the base fee.
			feeAmt = txData.EffectiveFee(decUtils.BaseFee)
			fee = sdkmath.LegacyNewDecFromBigInt(feeAmt)
		}

		// 3. min gas price (global min fee)
		ctx.Logger().Info("start MonoDecorator/CheckGlobalFee", "timestamp(unixnano)", time.Now().UnixNano())
		if err := CheckGlobalFee(fee, decUtils.GlobalMinGasPrice, gasLimit); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/CheckGlobalFee", "timestamp(unixnano)", time.Now().UnixNano())

		// 4. validate msg contents
		ctx.Logger().Info("start MonoDecorator/ValidateMsg", "timestamp(unixnano)", time.Now().UnixNano())

		if err := ValidateMsg(
			decUtils.EvmParams,
			txData,
			ethMsg.GetFrom(),
		); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/ValidateMsg", "timestamp(unixnano)", time.Now().UnixNano())

		// 5. signature verification
		ctx.Logger().Info("start MonoDecorator/SignatureVerification", "timestamp(unixnano)", time.Now().UnixNano())

		if err := SignatureVerification(
			ethMsg,
			decUtils.Signer,
			decUtils.EvmParams.AllowUnprotectedTxs,
		); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/SignatureVerification", "timestamp(unixnano)", time.Now().UnixNano())

		from := ethMsg.GetFrom()
		fromAddr := common.BytesToAddress(from)

		// 6. account balance verification
		// We get the account with the balance from the EVM keeper because it is
		// using a wrapper of the bank keeper as a dependency to scale all
		// balances to 18 decimals.
		ctx.Logger().Info("start MonoDecorator/VerifyAccountBalance", "timestamp(unixnano)", time.Now().UnixNano())

		account := md.evmKeeper.GetAccount(ctx, fromAddr)
		if err := VerifyAccountBalance(
			ctx,
			md.accountKeeper,
			account,
			fromAddr,
			txData,
		); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/VerifyAccountBalance", "timestamp(unixnano)", time.Now().UnixNano())

		// 7. can transfer
		ctx.Logger().Info("start MonoDecorator/CanTransfer", "timestamp(unixnano)", time.Now().UnixNano())

		coreMsg, err := ethMsg.AsMessage(decUtils.Signer, decUtils.BaseFee)
		if err != nil {
			return ctx, errorsmod.Wrapf(
				err,
				"failed to create an ethereum core.Message from signer %T", decUtils.Signer,
			)
		}

		if err := CanTransfer(
			ctx,
			md.evmKeeper,
			coreMsg,
			decUtils.BaseFee,
			ethCfg,
			decUtils.EvmParams,
			decUtils.Rules.IsLondon,
		); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/CanTransfer", "timestamp(unixnano)", time.Now().UnixNano())

		// 8. gas consumption
		ctx.Logger().Info("start MonoDecorator/GasConsumption", "timestamp(unixnano)", time.Now().UnixNano())

		msgFees, err := evmkeeper.VerifyFee(
			txData,
			evmDenom,
			decUtils.BaseFee,
			decUtils.Rules.IsHomestead,
			decUtils.Rules.IsIstanbul,
			ctx.IsCheckTx(),
		)
		if err != nil {
			return ctx, err
		}

		err = ConsumeFeesAndEmitEvent(
			ctx,
			md.evmKeeper,
			msgFees,
			from,
		)
		if err != nil {
			return ctx, err
		}

		gasWanted := UpdateCumulativeGasWanted(
			ctx,
			gas,
			md.maxGasWanted,
			decUtils.GasWanted,
		)
		decUtils.GasWanted = gasWanted

		minPriority := GetMsgPriority(
			txData,
			decUtils.MinPriority,
			decUtils.BaseFee,
		)
		decUtils.MinPriority = minPriority

		// Update the fee to be paid for the tx adding the fee specified for the
		// current message.
		decUtils.TxFee.Add(decUtils.TxFee, txData.Fee())

		// Update the transaction gas limit adding the gas specified in the
		// current message.
		decUtils.TxGasLimit += gas
		ctx.Logger().Info("end MonoDecorator/GasConsumption", "timestamp(unixnano)", time.Now().UnixNano())

		// 9. increment sequence
		ctx.Logger().Info("start MonoDecorator/IncreaseSequence", "timestamp(unixnano)", time.Now().UnixNano())

		acc := md.accountKeeper.GetAccount(ctx, from)
		if acc == nil {
			// safety check: shouldn't happen
			return ctx, errorsmod.Wrapf(
				errortypes.ErrUnknownAddress,
				"account %s does not exist",
				from,
			)
		}

		if err := IncrementNonce(ctx, md.accountKeeper, acc, txData.GetNonce()); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/IncreaseSequence", "timestamp(unixnano)", time.Now().UnixNano())

		// 10. gas wanted
		ctx.Logger().Info("start MonoDecorator/CheckGasWanted", "timestamp(unixnano)", time.Now().UnixNano())

		if err := CheckGasWanted(ctx, md.feeMarketKeeper, tx, decUtils.Rules.IsLondon); err != nil {
			return ctx, err
		}
		ctx.Logger().Info("end MonoDecorator/CheckGasWanted", "timestamp(unixnano)", time.Now().UnixNano())

		// 11. emit events
		txIdx := uint64(i) //nolint:gosec // G115
		EmitTxHashEvent(ctx, ethMsg, decUtils.BlockTxIndex, txIdx)
	}
	ctx.Logger().Info("start MonoDecorator/CheckTxFee", "timestamp(unixnano)", time.Now().UnixNano())

	if err := CheckTxFee(txFeeInfo, decUtils.TxFee, decUtils.TxGasLimit); err != nil {
		return ctx, err
	}
	ctx.Logger().Info("end MonoDecorator/CheckTxFee", "timestamp(unixnano)", time.Now().UnixNano())

	ctx.Logger().Info("start MonoDecorator/CheckBlockGasLimit", "timestamp(unixnano)", time.Now().UnixNano())

	ctx, err = CheckBlockGasLimit(ctx, decUtils.GasWanted, decUtils.MinPriority)
	if err != nil {
		return ctx, err
	}
	ctx.Logger().Info("end MonoDecorator/CheckBlockGasLimit", "timestamp(unixnano)", time.Now().UnixNano())

	return next(ctx, tx, simulate)
}
