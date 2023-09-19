// Copyright (c) 2021-2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrwallet/v3/rpc/walletrpc"
	txrules "decred.org/dcrwallet/v3/wallet/txrules"
	"decred.org/dcrwallet/v3/wallet/txsizes"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v4"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v4"
	"github.com/decred/dcrd/txscript/v4/stdaddr"
	"github.com/decred/dcrd/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	errs "github.com/decred/dcrpool/errors"
)

const (
	// PPS represents the pay per share payment method.
	PPS = "pps"

	// PPLNS represents the pay per last n shares payment method.
	PPLNS = "pplns"

	// maxRoundingDiff is the maximum amount of atoms the total
	// output value of a transaction is allowed to be short of the
	// provided input due to rounding errors.
	maxRoundingDiff = dcrutil.Amount(500)

	// maxTxConfThreshold is the total number of coinbase confirmation
	// failures before a wallet rescan is requested.
	maxTxConfThreshold = uint32(3)

	// paymentBufferSize is the size of the buffer on the payment channel.
	paymentBufferSize = uint32(30)
)

// TxCreator defines the functionality needed by a transaction creator for the
// pool.
type TxCreator interface {
	// GetTxOut fetches the output referenced by the provided txHash and index.
	GetTxOut(context.Context, *chainhash.Hash, uint32, int8, bool) (*chainjson.GetTxOutResult, error)
	// CreateRawTransaction generates a transaction from the provided
	// inputs and payouts.
	CreateRawTransaction(context.Context, []chainjson.TransactionInput, map[stdaddr.Address]dcrutil.Amount, *int64, *int64) (*wire.MsgTx, error)
	// GetBlock fetches the block associated with the provided block hash.
	GetBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error)
}

// TxBroadcaster defines the functionality needed by a transaction broadcaster
// for the pool.
type TxBroadcaster interface {
	// SignTransaction signs transaction inputs, unlocking them for use.
	SignTransaction(context.Context, *walletrpc.SignTransactionRequest, ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error)
	// PublishTransaction broadcasts the transaction unto the network.
	PublishTransaction(context.Context, *walletrpc.PublishTransactionRequest, ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error)
	// GetTransaction fetches transaction details for the provided transaction.
	GetTransaction(context.Context, *walletrpc.GetTransactionRequest, ...grpc.CallOption) (*walletrpc.GetTransactionResponse, error)
	// Rescan requests a wallet utxo rescan.
	Rescan(ctx context.Context, in *walletrpc.RescanRequest, opts ...grpc.CallOption) (walletrpc.WalletService_RescanClient, error)
}

// rescanMsg represents a rescan response.
type rescanMsg struct {
	resp *walletrpc.RescanResponse
	err  error
}

// PaymentMgrConfig contains all of the configuration values which should be
// provided when creating a new instance of PaymentMgr.
type PaymentMgrConfig struct {
	// db represents the pool database.
	db Database
	// ActiveNet represents the network being mined on.
	ActiveNet *chaincfg.Params
	// PoolFee represents the fee charged to participating accounts of the pool.
	PoolFee float64
	// LastNPeriod represents the period to source shares from when using the
	// PPLNS payment scheme.
	LastNPeriod time.Duration
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// PaymentMethod represents the payment scheme of the pool.
	PaymentMethod string
	// PoolFeeAddrs represents the pool fee addresses of the pool.
	PoolFeeAddrs []stdaddr.Address
	// WalletAccount represents the wallet account to process payments from.
	WalletAccount uint32
	// WalletPass represents the passphrase to unlock the wallet with.
	WalletPass string
	// GetBlockConfirmations returns the number of block confirmations for the
	// provided block hash.
	GetBlockConfirmations func(context.Context, *chainhash.Hash) (int64, error)
	// FetchTxCreator returns a transaction creator that allows coinbase lookups
	// and payment transaction creation.
	FetchTxCreator func() TxCreator
	// FetchTxBroadcaster returns a transaction broadcaster that allows signing
	// and publishing of transactions.
	FetchTxBroadcaster func() TxBroadcaster
	// CoinbaseConfTimeout is the duration to wait for coinbase confirmations
	// when generating a payout transaction.
	CoinbaseConfTimeout time.Duration
	// SignalCache sends the provided cache update event to the gui cache.
	SignalCache func(event CacheUpdateEvent)
}

// paymentMsg represents a payment processing signal.
type paymentMsg struct {
	CurrentHeight  uint32
	TreasuryActive bool
	Done           chan struct{}
}

// PaymentMgr handles generating shares and paying out dividends to
// participating accounts.
type PaymentMgr struct {
	failedTxConfs uint32 // update atomically.
	txConfHashes  map[chainhash.Hash]uint32

	prng *rand.Rand

	processing bool
	paymentCh  chan *paymentMsg
	cfg        *PaymentMgrConfig
	mtx        sync.Mutex
}

// NewPaymentMgr creates a new payment manager.
func NewPaymentMgr(pCfg *PaymentMgrConfig) (*PaymentMgr, error) {
	pm := &PaymentMgr{
		cfg:          pCfg,
		txConfHashes: make(map[chainhash.Hash]uint32),
		prng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		paymentCh:    make(chan *paymentMsg, paymentBufferSize),
	}

	// Initialize last payment info (height and paid-on).
	_, _, err := pm.cfg.db.loadLastPaymentInfo()
	if err != nil {
		if errors.Is(err, errs.ValueNotFound) {
			// Initialize with zeros.
			err = pm.cfg.db.persistLastPaymentInfo(0, 0)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	// Initialize last payment created-on.
	_, err = pm.cfg.db.loadLastPaymentCreatedOn()
	if err != nil {
		if errors.Is(err, errs.ValueNotFound) {
			// Initialize with zero.
			err = pm.cfg.db.persistLastPaymentCreatedOn(0)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return pm, nil
}

// isProcessing returns whether the payment manager is in the process of
// paying out dividends.
func (pm *PaymentMgr) isProcessing() bool {
	pm.mtx.Lock()
	defer pm.mtx.Unlock()
	return pm.processing
}

// setProcessing sets the processing flag to the provided boolean.
func (pm *PaymentMgr) setProcessing(status bool) {
	pm.mtx.Lock()
	pm.processing = status
	pm.mtx.Unlock()
}

// sharePercentages calculates the percentages due each participating account
// according to their weighted shares.
func (pm *PaymentMgr) sharePercentages(shares []*Share) (map[string]*big.Rat, error) {
	totalShares := new(big.Rat)
	tally := make(map[string]*big.Rat)
	percentages := make(map[string]*big.Rat)

	// Tally all share weights for each participating account.
	for _, share := range shares {
		totalShares = totalShares.Add(totalShares, share.Weight)
		if _, ok := tally[share.Account]; ok {
			tally[share.Account] = tally[share.Account].
				Add(tally[share.Account], share.Weight)
			continue
		}
		tally[share.Account] = share.Weight
	}

	// Calculate each participating account percentage to be claimed.
	for account, shareCount := range tally {
		if tally[account].Cmp(ZeroRat) == 0 {
			return nil, errs.PoolError(errs.DivideByZero, "division by zero")
		}
		accPercent := new(big.Rat).Quo(shareCount, totalShares)
		percentages[account] = accPercent
	}
	return percentages, nil
}

// PPSSharePercentages calculates the current mining reward percentages
// due participating pool accounts based on work performed measured by
// the PPS payment scheme.
func (pm *PaymentMgr) PPSSharePercentages(workCreatedOn int64) (map[string]*big.Rat, error) {
	shares, err := pm.cfg.db.ppsEligibleShares(workCreatedOn)
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}
	percentages, err := pm.sharePercentages(shares)
	if err != nil {
		return nil, err
	}
	return percentages, nil
}

// PPLNSSharePercentages calculates the current mining reward percentages due pool
// accounts based on work performed measured by the PPLNS payment scheme.
func (pm *PaymentMgr) PPLNSSharePercentages() (map[string]*big.Rat, error) {
	now := time.Now()
	min := now.Add(-pm.cfg.LastNPeriod)
	shares, err := pm.cfg.db.pplnsEligibleShares(min.UnixNano())
	if err != nil {
		return nil, err
	}
	if len(shares) == 0 {
		return make(map[string]*big.Rat), nil
	}

	// Deduct pool fees and calculate the payment due each participating
	// account.
	percentages, err := pm.sharePercentages(shares)
	if err != nil {
		return nil, err
	}
	return percentages, nil
}

// calculatePayments creates the payments due participating accounts.
func (pm *PaymentMgr) calculatePayments(ratios map[string]*big.Rat, source *PaymentSource,
	total dcrutil.Amount, poolFee float64, height uint32, estMaturity uint32) ([]*Payment, int64, error) {
	const funcName = "calculatePayments"
	if len(ratios) == 0 {
		desc := fmt.Sprintf("%s: valid share ratios required to "+
			"generate payments", funcName)
		return nil, 0, errs.PoolError(errs.ShareRatio, desc)
	}

	// Deduct pool fee from the amount to be shared.
	fee := total.MulF64(poolFee)
	amtSansFees := total - fee
	sansFees := new(big.Rat).SetInt64(int64(amtSansFees))
	paymentTotal := dcrutil.Amount(0)
	dustAmts := make([]dcrutil.Amount, 0)

	// Calculate each participating account's portion of the amount after fees.
	payments := make([]*Payment, 0)
	for account, ratio := range ratios {
		amtRat := new(big.Rat).Mul(sansFees, ratio)
		amtI, accuracy := new(big.Float).SetRat(amtRat).Int64()
		amt := dcrutil.Amount(amtI)

		// Reduce the amount by an atom if float conversion accuracy was
		// above the actual value.
		if accuracy > 0 {
			amt -= dcrutil.Amount(1)
		}

		paymentTotal += amt

		// The script size of the output is assumed to be the worst possible,
		// which is txsizes.P2PKHOutputSize, to avoid a lower size estimation.
		if txrules.IsDustAmount(amt, txsizes.P2PKHOutputSize,
			txrules.DefaultRelayFeePerKb) {
			// Since dust payments will cause the payout transaction to error
			// and are also most likely to be generated by participating
			// accounts contributing sporadic work to pool they will be
			// forfeited by their corresponding accounts and be added to
			// the pool fee payout. This is intended to serve as a deterrent
			// for contributing intermittent, sporadic work to the pool.
			dustAmts = append(dustAmts, amt)
		} else {
			payments = append(payments, NewPayment(account, source, amt, height,
				estMaturity))
		}
	}

	if amtSansFees < paymentTotal {
		diff := paymentTotal - amtSansFees
		desc := fmt.Sprintf("%s: total payments (%s) is greater than "+
			"the remaining coinbase amount after fees (%s). Difference is %s",
			funcName, paymentTotal, amtSansFees, diff)
		return nil, 0, errs.PoolError(errs.PaymentSource, desc)
	}

	// Add a payout entry for pool fees, which includes any dust payments
	// collected.
	var dustTotal dcrutil.Amount
	for _, amt := range dustAmts {
		dustTotal += amt
	}

	feePayment := NewPayment(PoolFeesK, source, fee+dustTotal, height, estMaturity)
	payments = append(payments, feePayment)

	return payments, feePayment.CreatedOn, nil
}

// PayPerShare generates a payment bundle comprised of payments to all
// participating accounts. Payments are calculated based on work contributed
// to the pool since the last payment batch.
func (pm *PaymentMgr) payPerShare(source *PaymentSource, amt dcrutil.Amount, height uint32, workCreatedOn int64) error {
	percentages, err := pm.PPSSharePercentages(workCreatedOn)
	if err != nil {
		return err
	}
	estMaturity := height + uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
	payments, lastPmtCreatedOn, err := pm.calculatePayments(percentages,
		source, amt, pm.cfg.PoolFee, height, estMaturity)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		err := pm.cfg.db.PersistPayment(payment)
		if err != nil {
			return err
		}
	}
	// Update the last payment created on time and prune invalidated shares.
	err = pm.cfg.db.persistLastPaymentCreatedOn(lastPmtCreatedOn)
	if err != nil {
		return err
	}
	return pm.cfg.db.pruneShares(workCreatedOn)
}

// payPerLastNShares generates a payment bundle comprised of payments to all
// participating accounts within the lastNPeriod of the pool.
func (pm *PaymentMgr) payPerLastNShares(source *PaymentSource, amt dcrutil.Amount, height uint32) error {
	percentages, err := pm.PPLNSSharePercentages()
	if err != nil {
		return err
	}
	estMaturity := height + uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
	payments, lastPmtCreatedOn, err := pm.calculatePayments(percentages,
		source, amt, pm.cfg.PoolFee, height, estMaturity)
	if err != nil {
		return err
	}
	for _, payment := range payments {
		err := pm.cfg.db.PersistPayment(payment)
		if err != nil {
			return err
		}
	}
	// Update the last payment created on time and prune invalidated shares.
	err = pm.cfg.db.persistLastPaymentCreatedOn(lastPmtCreatedOn)
	if err != nil {
		return err
	}
	minNano := time.Now().Add(-pm.cfg.LastNPeriod).UnixNano()
	return pm.cfg.db.pruneShares(minNano)
}

// generatePayments creates payments for participating accounts. This should
// only be called when a block is confirmed mined, in pool mining mode.
func (pm *PaymentMgr) generatePayments(height uint32, source *PaymentSource, amt dcrutil.Amount, workCreatedOn int64) error {
	switch pm.cfg.PaymentMethod {
	case PPS:
		return pm.payPerShare(source, amt, height, workCreatedOn)

	case PPLNS:
		return pm.payPerLastNShares(source, amt, height)

	default:
		return fmt.Errorf("unknown payment method provided %v", pm.cfg.PaymentMethod)
	}
}

// pruneOrphanedPayments removes all orphaned payments from the provided payments.
func (pm *PaymentMgr) pruneOrphanedPayments(ctx context.Context, pmts map[string][]*Payment) (map[string][]*Payment, error) {
	toDelete := make([]string, 0, len(pmts))
	for key := range pmts {
		blockHash, err := chainhash.NewHashFromStr(key)
		if err != nil {
			desc := fmt.Sprintf("unable to generate hash: %v", err)
			return nil, errs.PoolError(errs.CreateHash, desc)
		}

		confs, err := pm.cfg.GetBlockConfirmations(ctx, blockHash)
		if err != nil {
			return nil, err
		}

		// If the block has no confirmations for the current chain
		// state it is an orphan. Remove payments associated with it.
		if confs <= 0 {
			toDelete = append(toDelete, key)
		}
	}

	// Delete payments sourced from orphaned blocks.
	for _, k := range toDelete {
		delete(pmts, k)
	}
	return pmts, nil
}

// applyTxFees determines the transaction fees needed for the payout transaction
// and deducts portions of the fee from outputs of participating accounts
// being paid to.
//
// The deducted portions are calculated as the percentage of fees based on
// the ratio of the amount being paid to the total transaction output minus
// pool fees.
func (pm *PaymentMgr) applyTxFees(inputs []chainjson.TransactionInput, outputs map[string]dcrutil.Amount,
	tOut dcrutil.Amount, feeAddr stdaddr.Address) (dcrutil.Amount, dcrutil.Amount, error) {
	const funcName = "applyTxFees"
	if len(inputs) == 0 {
		desc := fmt.Sprintf("%s: cannot create a payout transaction "+
			"without a tx input", funcName)
		return 0, 0, errs.PoolError(errs.TxIn, desc)
	}
	if len(outputs) == 0 {
		desc := fmt.Sprintf("%s: cannot create a payout transaction "+
			"without a tx output", funcName)
		return 0, 0, errs.PoolError(errs.TxOut, desc)
	}
	inSizes := make([]int, len(inputs))
	for range inputs {
		inSizes = append(inSizes, txsizes.RedeemP2PKHSigScriptSize)
	}
	outSizes := make([]int, len(outputs))
	for range outputs {
		outSizes = append(outSizes, txsizes.P2PKHOutputSize)
	}
	changeScriptSize := 0
	estSize := txsizes.EstimateSerializeSizeFromScriptSizes(inSizes, outSizes,
		changeScriptSize)
	estFee := txrules.FeeForSerializeSize(txrules.DefaultRelayFeePerKb, estSize)
	sansFees := tOut - estFee

	for addr, v := range outputs {
		// Pool fee payments are excluded from tx fee deductions.
		if addr == feeAddr.String() {
			continue
		}

		ratio := float64(int64(sansFees)) / float64(int64(v))
		outFee := estFee.MulF64(ratio)
		outputs[addr] -= outFee
	}

	return sansFees, estFee, nil
}

// confirmCoinbases ensures the coinbases referenced by the provided
// transaction hashes are spendable by querying the wallet about each
// transaction and asserting confirmations are equal to or greater
// than coinbase maturity.
//
// Confirming coinbases before creating, signing and publising
// transactions spending them mitigates possible errors in the
// transaction publishing process by ensuring the wallet is
// aware of the inputs it is expected to know about.
//
// The context passed to this function must have a corresponding
// cancellation to allow for a clean shutdown process.
func (pm *PaymentMgr) confirmCoinbases(ctx context.Context, txB TxBroadcaster, txHashes map[chainhash.Hash]uint32) error {
	const funcName = "confirmCoinbases"
	maxSpendableConfs := int32(pm.cfg.ActiveNet.CoinbaseMaturity) + 1

	keys := make([]chainhash.Hash, 0, len(txHashes))
	for hash := range txHashes {
		keys = append(keys, hash)
	}

	for idx := range keys {
		select {
		case <-ctx.Done():
			log.Tracef("%s: context cancelled fetching tx "+
				"confirmations", funcName)
			return errs.ContextCancelled

		default:
			req := &walletrpc.GetTransactionRequest{
				TransactionHash: keys[idx][:],
			}

			resp, err := txB.GetTransaction(ctx, req)
			if err != nil {
				state, ok := status.FromError(err)
				if ok {
					if state.Code() != codes.NotFound {
						return err
					}

					log.Errorf("%s: transaction %v not found by wallet",
						funcName, err)
					continue
				}
				return err
			}

			// Remove the confirmed transaction.
			if resp.Confirmations >= maxSpendableConfs {
				delete(txHashes, keys[idx])
			}
		}
	}

	if len(txHashes) != 0 {
		log.Tracef("%d coinbase(s) are not confirmed:", len(txHashes))
		for coinbase := range txHashes {
			log.Trace(coinbase)
		}
		return errs.TxConf
	}

	return nil
}

// fetchRecanReponse is a helper function for fetching rescan response.
// It will return when either a response or error is received from the
// provided rescan source, or when the provided context is cancelled.
func fetchRescanResponse(ctx context.Context, rescanSource func() (*walletrpc.RescanResponse, error)) (*walletrpc.RescanResponse, error) {
	const funcName = "fetchRescanResponse"
	respCh := make(chan *rescanMsg)
	go func(ch chan *rescanMsg) {
		resp, err := rescanSource()
		ch <- &rescanMsg{
			resp: resp,
			err:  err,
		}
	}(respCh)

	select {
	case <-ctx.Done():
		log.Tracef("%s: context cancelled fetching rescan response", funcName)
		return nil, errs.ContextCancelled
	case msg := <-respCh:
		close(respCh)
		if msg.err != nil {
			desc := fmt.Sprintf("%s: unable fetch wallet rescan response, %s",
				funcName, msg.err)
			return nil, errs.PoolError(errs.Rescan, desc)
		}
		return msg.resp, nil
	}
}

// monitorRescan ensures the wallet rescans up to the provided block height.
//
// The context passed to this function must have a corresponding
// cancellation to allow for a clean shutdown process.
func (pm *PaymentMgr) monitorRescan(ctx context.Context, rescanSource walletrpc.WalletService_RescanClient, height int32) error {
	const funcName = "monitorRescan"
	for {
		resp, err := fetchRescanResponse(ctx, rescanSource.Recv)
		if err != nil {
			if errors.Is(err, errs.ContextCancelled) {
				desc := fmt.Sprintf("%s: cancelled wallet rescan", funcName)
				return errs.PoolError(errs.ContextCancelled, desc)
			}
			return err
		}

		// Stop monitoring once the most recent block height has been rescanned.
		if resp.RescannedThrough >= height {
			log.Infof("wallet rescanned through height #%d", height)
			return nil
		}
	}
}

// generatePayoutTxDetails creates the payout transaction inputs and outputs
// from the provided payments
func (pm *PaymentMgr) generatePayoutTxDetails(ctx context.Context, txC TxCreator, feeAddr stdaddr.Address, payments map[string][]*Payment, treasuryActive bool) ([]chainjson.TransactionInput,
	map[chainhash.Hash]uint32, map[string]dcrutil.Amount, dcrutil.Amount, error) {
	const funcName = "generatePayoutTxDetails"

	// The coinbase output prior to
	// [DCP0006](https://github.com/decred/dcps/pull/17)
	// activation is at the third index position and at
	// the second index position once DCP0006 is activated.
	coinbaseIndex := uint32(1)
	if !treasuryActive {
		coinbaseIndex = 2
	}

	var tIn, tOut dcrutil.Amount
	inputs := make([]chainjson.TransactionInput, 0)
	inputTxHashes := make(map[chainhash.Hash]uint32)
	outputs := make(map[string]dcrutil.Amount)
	for _, pmtSet := range payments {
		coinbaseTx := pmtSet[0].Source.Coinbase
		txHash, err := chainhash.NewHashFromStr(coinbaseTx)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to create tx hash: %v",
				funcName, err)
			return nil, nil, nil, 0, errs.PoolError(errs.CreateHash, desc)
		}

		// Ensure the referenced prevout to be spent is spendable at
		// the current height.

		txOutResult, err := txC.GetTxOut(ctx, txHash, coinbaseIndex, wire.TxTreeRegular, false)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to find tx output: %v",
				funcName, err)
			return nil, nil, nil, 0, errs.PoolError(errs.TxOut, desc)
		}
		if txOutResult.Confirmations < int64(pm.cfg.ActiveNet.CoinbaseMaturity+1) {
			desc := fmt.Sprintf("%s: referenced coinbase at "+
				"index %d for tx %v is not spendable", funcName,
				coinbaseIndex, txHash.String())
			return nil, nil, nil, 0, errs.PoolError(errs.Coinbase, desc)
		}

		// Create the transaction input using the provided prevOut.
		in := chainjson.TransactionInput{
			Amount: txOutResult.Value,
			Txid:   txHash.String(),
			Vout:   coinbaseIndex,
			Tree:   wire.TxTreeRegular,
		}
		inputs = append(inputs, in)
		inputTxHashes[*txHash] = pmtSet[0].Height

		prevOutV, err := dcrutil.NewAmount(in.Amount)
		if err != nil {
			desc := fmt.Sprintf("%s: unable create the input amount: %v",
				funcName, err)
			return nil, nil, nil, 0, errs.PoolError(errs.CreateAmount, desc)
		}
		tIn += prevOutV

		// Generate the outputs paying dividends as well as pool fees.
		for _, pmt := range pmtSet {
			if pmt.Account == PoolFeesK {
				_, ok := outputs[feeAddr.String()]
				if !ok {
					outputs[feeAddr.String()] = pmt.Amount
					tOut += pmt.Amount
					continue
				}
				outputs[feeAddr.String()] += pmt.Amount
				tOut += pmt.Amount
				continue
			}

			acc, err := pm.cfg.db.fetchAccount(pmt.Account)
			if err != nil {
				return nil, nil, nil, 0, err
			}
			_, ok := outputs[acc.Address]
			if !ok {
				outputs[acc.Address] = pmt.Amount
				tOut += pmt.Amount
				continue
			}
			outputs[acc.Address] += pmt.Amount
			tOut += pmt.Amount
		}
	}

	// Ensure the transaction outputs do not source more value than possible
	// from the provided inputs and also are consuming all of the input
	// value after rounding errors.
	if tOut > tIn {
		desc := fmt.Sprintf("%s: total output values for the "+
			"transaction (%s) is greater than the provided inputs (%s)",
			funcName, tOut, tIn)
		return nil, nil, nil, 0, errs.PoolError(errs.CreateTx, desc)
	}

	diff := tIn - tOut
	if diff > maxRoundingDiff {
		desc := fmt.Sprintf("%s: difference between total output "+
			"values and the provided inputs (%s) exceeds the maximum "+
			"allowed for rounding errors (%s)", funcName, diff, maxRoundingDiff)
		return nil, nil, nil, 0, errs.PoolError(errs.CreateTx, desc)
	}

	return inputs, inputTxHashes, outputs, tOut, nil
}

// PayDividends pays mature mining rewards to participating accounts.
func (pm *PaymentMgr) payDividends(ctx context.Context, height uint32, treasuryActive bool) error {
	const funcName = "payDividends"
	mPmts, err := pm.cfg.db.maturePendingPayments(height)
	if err != nil {
		return err
	}

	// Nothing to do if there are no mature payments to process.
	if len(mPmts) == 0 {
		return nil
	}

	if pm.isProcessing() {
		log.Info("payment processing already in progress, terminating")
		return nil
	}

	pm.setProcessing(true)
	defer pm.setProcessing(false)

	txB := pm.cfg.FetchTxBroadcaster()
	if txB == nil {
		desc := fmt.Sprintf("%s: tx broadcaster cannot be nil", funcName)
		return errs.PoolError(errs.Disconnected, desc)
	}

	pCtx, pCancel := context.WithTimeout(ctx, pm.cfg.CoinbaseConfTimeout)
	defer pCancel()

	// Request a wallet rescan if tx confirmation failures are
	// at threshold.
	txConfCount := atomic.LoadUint32(&pm.failedTxConfs)
	if txConfCount >= maxTxConfThreshold {
		beginHeight := uint32(0)

		// Having no tx conf hashes at threshold indicates an
		// underlying error.
		pm.mtx.Lock()
		if len(pm.txConfHashes) == 0 {
			pm.mtx.Unlock()
			desc := fmt.Sprintf("%s: no tx conf hashes to rescan for",
				funcName)
			return errs.PoolError(errs.TxConf, desc)
		}

		// Find the lowest height to start the rescan from.
		for _, height := range pm.txConfHashes {
			if beginHeight == 0 {
				beginHeight = height
			}

			if beginHeight > height {
				beginHeight = height
			}
		}
		pm.mtx.Unlock()

		// Start the rescan a block height one coinbase maturity length of
		// blocks below the lowest reported block with a transaction
		// having inaccurate confirmation information.
		rescanBeginHeight := beginHeight - uint32(pm.cfg.ActiveNet.CoinbaseMaturity)
		log.Infof("wallet rescanning from height #%d", rescanBeginHeight)
		rescanReq := &walletrpc.RescanRequest{
			BeginHeight: int32(rescanBeginHeight),
		}
		rescanSource, err := txB.Rescan(pCtx, rescanReq)
		if err != nil {
			desc := fmt.Sprintf("%s: rescan source cannot be nil", funcName)
			return errs.PoolError(errs.Rescan, desc)
		}

		err = pm.monitorRescan(pCtx, rescanSource, int32(height))
		if err != nil {
			return err
		}
	}

	txC := pm.cfg.FetchTxCreator()
	if txC == nil {
		desc := fmt.Sprintf("%s: tx creator cannot be nil", funcName)
		return errs.PoolError(errs.Disconnected, desc)
	}

	// Remove all matured orphaned payments. Since the associated blocks
	// to these payments are not part of the main chain they will not be
	// paid out.
	pmts, err := pm.pruneOrphanedPayments(ctx, mPmts)
	if err != nil {
		return err
	}

	// The fee address is being picked at random from the set of pool fee
	// addresses to make it difficult for third-parties wanting to track
	// pool fees collected by the pool and ultimately determine the
	// cumulative value accrued by pool operators.
	feeAddr := pm.cfg.PoolFeeAddrs[pm.prng.Intn(len(pm.cfg.PoolFeeAddrs))]

	inputs, inputTxHashes, outputs, tOut, err :=
		pm.generatePayoutTxDetails(ctx, txC, feeAddr, pmts, treasuryActive)
	if err != nil {
		return err
	}

	_, estFee, err := pm.applyTxFees(inputs, outputs, tOut, feeAddr)
	if err != nil {
		return err
	}

	// Generate the transaction output set.
	outs := make(map[stdaddr.Address]dcrutil.Amount, len(outputs))
	for sAddr, amt := range outputs {
		addr, err := stdaddr.DecodeAddress(sAddr, pm.cfg.ActiveNet)
		if err != nil {
			desc := fmt.Sprintf("%s: unable to decode payout address: %v",
				funcName, err)
			return errs.PoolError(errs.Decode, desc)
		}
		outs[addr] = amt
	}

	// Ensure the wallet is aware of all the coinbase outputs being
	// spent by the payout transaction.
	err = pm.confirmCoinbases(pCtx, txB, inputTxHashes)
	if err != nil {
		// Increment the failed tx confirmation counter.
		atomic.AddUint32(&pm.failedTxConfs, 1)

		// Track the transactions with inaccurate confirmation data.
		pm.mtx.Lock()
		for k, v := range inputTxHashes {
			pm.txConfHashes[k] = v
		}
		pm.mtx.Unlock()

		// Do not error if coinbase spendable confirmation requests
		// are terminated by the context cancellation.
		if !errors.Is(err, errs.ContextCancelled) {
			return err
		}

		return nil
	}

	// Create, sign and publish the payout transaction.
	tx, err := txC.CreateRawTransaction(ctx, inputs, outs, nil, nil)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to create transaction: %v",
			funcName, err)
		return errs.PoolError(errs.CreateTx, desc)
	}
	txBytes, err := tx.Bytes()
	if err != nil {
		return err
	}

	signTxReq := &walletrpc.SignTransactionRequest{
		SerializedTransaction: txBytes,
		Passphrase:            []byte(pm.cfg.WalletPass),
	}
	signedTxResp, err := txB.SignTransaction(ctx, signTxReq)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to sign transaction: %v",
			funcName, err)
		return errs.PoolError(errs.SignTx, desc)
	}

	pubTxReq := &walletrpc.PublishTransactionRequest{
		SignedTransaction: signedTxResp.Transaction,
	}
	pubTxResp, err := txB.PublishTransaction(ctx, pubTxReq)
	if err != nil {
		desc := fmt.Sprintf("%s: unable to publish transaction: %v",
			funcName, err)
		return errs.PoolError(errs.PublishTx, desc)
	}

	txid, err := chainhash.NewHash(pubTxResp.TransactionHash)
	if err != nil {
		desc := fmt.Sprintf("unable to create transaction hash: %v", err)
		return errs.PoolError(errs.CreateHash, desc)
	}
	fees := outputs[feeAddr.String()]

	log.Infof("paid a total of %v in tx %s, including %v in pool fees. "+
		"Tx fee: %v", tOut, txid.String(), fees, estFee)

	// Update all associated payments as paid and archive them.
	for _, set := range pmts {
		for _, pmt := range set {
			pmt.PaidOnHeight = height
			pmt.TransactionID = txid.String()
			err := pm.cfg.db.updatePayment(pmt)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to update payment: %v",
					funcName, err)
				return errs.PoolError(errs.PersistEntry, desc)
			}
			err = pm.cfg.db.ArchivePayment(pmt)
			if err != nil {
				desc := fmt.Sprintf("%s: unable to archive payment: %v",
					funcName, err)
				return errs.PoolError(errs.PersistEntry, desc)
			}
		}
	}

	// Update payments metadata.
	err = pm.cfg.db.persistLastPaymentInfo(height, time.Now().UnixNano())
	if err != nil {
		return err
	}

	// Reset the failed tx conf counter and clear the hashes.
	atomic.StoreUint32(&pm.failedTxConfs, 0)

	pm.mtx.Lock()
	pm.txConfHashes = make(map[chainhash.Hash]uint32)
	pm.mtx.Unlock()

	return nil
}

// processPayments relays payment signals for processing.
func (pm *PaymentMgr) processPayments(msg *paymentMsg) {
	pm.paymentCh <- msg
}

// handlePayments processes dividend payment signals.
// This MUST be run as a goroutine.
func (pm *PaymentMgr) handlePayments(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case msg := <-pm.paymentCh:
			if !pm.cfg.SoloPool {
				err := pm.payDividends(ctx, msg.CurrentHeight, msg.TreasuryActive)
				if err != nil {
					log.Errorf("unable to process payments: %v", err)
					close(msg.Done)
					continue
				}

				// Signal the gui cache of paid dividends.
				pm.cfg.SignalCache(DividendsPaid)

				close(msg.Done)
			}
		}
	}
}
