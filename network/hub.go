package network

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coreos/bbolt"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrwallet/rpc/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"dnldd/dcrpool/database"
	"dnldd/dcrpool/dividend"
)

const (
	// MaxReorgLimit is an estimated maximum chain reorganization limit.
	// That is, it is highly improbable for the the chain to reorg beyond six
	// blocks from the chain tip.
	MaxReorgLimit = 6
)

// HubConfig represents configuration details for the hub.
type HubConfig struct {
	ActiveNet         *chaincfg.Params
	DcrdRPCCfg        *rpcclient.ConnConfig
	PoolFee           float64
	MaxTxFeeReserve   dcrutil.Amount
	MaxGenTime        *big.Int
	WalletRPCCertFile string
	WalletGRPCHost    string
	PaymentMethod     string
	LastNPeriod       uint32
	WalletPass        string
	MinPayment        dcrutil.Amount
	PoolFeeAddrs      []dcrutil.Address
}

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	db             *bolt.DB
	httpc          *http.Client
	cfg            *HubConfig
	limiter        *RateLimiter
	Broadcast      chan Message
	rpcc           *rpcclient.Client
	rpccMtx        sync.Mutex
	gConn          *grpc.ClientConn
	grpc           walletrpc.WalletServiceClient
	grpcMtx        sync.Mutex
	hashRate       *big.Int
	hashRateMtx    sync.Mutex
	poolTargets    map[string]uint32
	poolTargetsMtx sync.RWMutex
	ConnCount      uint64
	Ticker         *time.Ticker
	connCh         chan []byte
	discCh         chan []byte
	ctx            context.Context
	cancel         context.CancelFunc
	txFeeReserve   dcrutil.Amount
}

// NewHub initializes a websocket hub.
func NewHub(ctx context.Context, cancel context.CancelFunc, db *bolt.DB, httpc *http.Client, hcfg *HubConfig, limiter *RateLimiter) (*Hub, error) {
	h := &Hub{
		db:          db,
		httpc:       httpc,
		limiter:     limiter,
		cfg:         hcfg,
		hashRate:    dividend.ZeroInt,
		poolTargets: make(map[string]uint32),
		Broadcast:   make(chan Message),
		ConnCount:   0,
		Ticker:      time.NewTicker(time.Second * 5),
		connCh:      make(chan []byte),
		discCh:      make(chan []byte),
		ctx:         ctx,
		cancel:      cancel,
	}

	log.Infof("Payment method is %v.", hcfg.PaymentMethod)

	// Load the tx fee reserve.
	err := db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)

		v := pbkt.Get(dividend.TxFeeReserve)
		if v == nil {
			log.Info("Tx fee reserve value not found in db, initializing.")
			h.txFeeReserve = dcrutil.Amount(0)
		}

		if v != nil {
			h.txFeeReserve = dcrutil.Amount(binary.LittleEndian.Uint32(v))
		}

		return nil
	})
	if err != nil {
		log.Error(err)
		return nil, err
	}

	log.Tracef("Tx fee reserve is currently %v, with a max of %v",
		h.txFeeReserve, h.cfg.MaxTxFeeReserve)

	// Calculate pool targets for all known miners.
	h.poolTargetsMtx.Lock()
	for miner, hashrate := range dividend.MinerHashes {
		target, err := dividend.CalculatePoolTarget(h.cfg.ActiveNet, hashrate,
			h.cfg.MaxGenTime)
		if err != nil {
			log.Error(err)
			return nil, err
		}

		compactTarget := blockchain.BigToCompact(target)
		h.poolTargets[miner] = compactTarget
	}
	h.poolTargetsMtx.Unlock()

	log.Tracef("Pool targets are: %v", spew.Sdump(h.poolTargets))

	// Create handlers for chain notifications being subscribed for.
	ntfnHandlers := &rpcclient.NotificationHandlers{
		OnBlockConnected: func(blkHeader []byte, transactions [][]byte) {
			h.connCh <- blkHeader
		},
		OnBlockDisconnected: func(blkHeader []byte) {
			h.discCh <- blkHeader
		},
		OnWork: func(blkHeader string, target string) {
			log.Tracef("New Work (header: %v , target: %v)", blkHeader,
				target)

			if !h.HasConnectedClients() {
				return
			}

			// Broadcast a work notification.
			h.Broadcast <- WorkNotification(blkHeader, "")
		},
	}

	// Establish RPC connection with dcrd.
	rpcc, err := rpcclient.New(hcfg.DcrdRPCCfg, ntfnHandlers)
	if err != nil {
		return nil, fmt.Errorf("rpc error (dcrd): %v", err)
	}

	h.rpcc = rpcc

	// Subscribe for chain notifications.
	if err := h.rpcc.NotifyWork(); err != nil {
		h.rpccMtx.Lock()
		h.rpcc.Shutdown()
		h.rpccMtx.Unlock()
		return nil, fmt.Errorf("notify work rpc error (dcrd): %v", err)
	}
	if err := h.rpcc.NotifyBlocks(); err != nil {
		h.rpccMtx.Lock()
		h.rpcc.Shutdown()
		h.rpccMtx.Unlock()
		return nil, fmt.Errorf("notify blocks rpc error (dcrd): %v", err)
	}

	log.Debugf("RPC connection established with dcrd.")

	// Establish GRPC connection with the wallet.
	creds, err := credentials.NewClientTLSFromFile(hcfg.WalletRPCCertFile,
		"localhost")
	if err != nil {
		return nil, fmt.Errorf("grpc tls error (dcrwallet): %v", err)
	}

	h.gConn, err = grpc.Dial(hcfg.WalletGRPCHost,
		grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("grpc dial error (dcrwallet): %v", err)
	}

	if h.gConn == nil {
		return nil, fmt.Errorf("Failed to establish grpc with the wallet")
	}

	h.grpc = walletrpc.NewWalletServiceClient(h.gConn)

	go h.process()
	// h.dividendPayments()

	return h, nil
}

// HasConnectedClients asserts the mining pool has connected miners.
func (h *Hub) HasConnectedClients() bool {
	connCount := atomic.LoadUint64(&h.ConnCount)
	if connCount == 0 {
		return false
	}
	return true
}

// SubmitWork sends solved block data for evaluation.
func (h *Hub) SubmitWork(data *string) (bool, error) {
	h.rpccMtx.Lock()
	status, err := h.rpcc.GetWorkSubmit(*data)
	h.rpccMtx.Unlock()
	return status, err
}

// AddHashRate adds the hash power provided by the newly connected client
//to the total estimated hash power of pool.
func (h *Hub) AddHashRate(miner string) {
	hash := dividend.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Add(h.hashRate, hash)
	log.Infof("Client connected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// RemoveHashRate removes the hash power previously provided by the
// disconnected client from the total estimated hash power of the pool.
func (h *Hub) RemoveHashRate(miner string) {
	hash := dividend.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Sub(h.hashRate, hash)
	log.Infof("Client disconnected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// PersistTxFeeReserve saves the tx fee reserve to the database.
func (h *Hub) PersistTxFeeReserve() error {
	err := h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		vbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(vbytes, uint32(h.txFeeReserve))
		return pbkt.Put(dividend.TxFeeReserve, vbytes)
	})
	return err
}

// PublishTransaction creates a transaction paying pool accounts for work done.
func (h *Hub) PublishTransaction(payouts map[dcrutil.Address]dcrutil.Amount, targetAmt dcrutil.Amount) error {
	outs := make([]*walletrpc.ConstructTransactionRequest_Output, 0, len(payouts))
	for addr, amt := range payouts {
		out := &walletrpc.ConstructTransactionRequest_Output{
			Destination: &walletrpc.ConstructTransactionRequest_OutputDestination{
				Address: addr.String(),
			},
			Amount: int64(amt),
		}

		outs = append(outs, out)
	}

	// Construct the transaction.
	constructTxReq := &walletrpc.ConstructTransactionRequest{
		SourceAccount:            0,
		RequiredConfirmations:    6,
		OutputSelectionAlgorithm: walletrpc.ConstructTransactionRequest_ALL,
		NonChangeOutputs:         outs,
	}

	h.grpcMtx.Lock()
	constructTxResp, err := h.grpc.ConstructTransaction(context.TODO(), constructTxReq)
	h.grpcMtx.Unlock()
	if err != nil {
		return err
	}

	// Sign the transaction.
	signTxReq := &walletrpc.SignTransactionRequest{
		SerializedTransaction: constructTxResp.UnsignedTransaction,
		Passphrase:            []byte(h.cfg.WalletPass),
	}

	h.grpcMtx.Lock()
	signedTxResp, err := h.grpc.SignTransaction(context.TODO(), signTxReq)
	h.grpcMtx.Unlock()
	if err != nil {
		return err
	}

	// Publish the transaction.
	pubTxReq := &walletrpc.PublishTransactionRequest{
		SignedTransaction: signedTxResp.Transaction,
	}

	h.grpcMtx.Lock()
	pubTxResp, err := h.grpc.PublishTransaction(context.TODO(), pubTxReq)
	h.grpcMtx.Unlock()
	if err != nil {
		return err
	}

	log.Tracef("Published tx hash is: %x", pubTxResp.TransactionHash)

	return nil
}

func (h *Hub) process() {
	log.Info("Started hub process handler.")
out:
	for {
		select {
		case <-h.ctx.Done():
			h.Broadcast <- nil
			h.gConn.Close()
			h.rpcc.Shutdown()
			break out

		case blkHeader := <-h.connCh:
			header, err := ParseBlockHeader(blkHeader)
			if err != nil {
				log.Errorf("failed to parse conected block header: %v", err)
				h.cancel()
				continue
			}

			blockHash := header.BlockHash()
			log.Tracef("Block connected (hash: %v, height: %v)",
				blockHash, header.Height)

			nonce := FetchNonce(blkHeader)
			encoded := make([]byte, hex.EncodedLen(len(nonce)))
			_ = hex.Encode(encoded, nonce)
			work, err := GetAcceptedWork(h.db, encoded)
			if err != nil {
				log.Errorf("failed to fetch accepted work: %v", err)
				h.cancel()
				continue
			}

			// If the connected block is an accepted work from the pool,
			// record payouts to participating accounts.
			if work != nil {
				// Fetch the associated coinbase amount for the block.
				h.rpccMtx.Lock()
				block, err := h.rpcc.GetBlock(&blockHash)
				h.rpccMtx.Unlock()
				if err != nil {
					log.Error(err)
					h.cancel()
					continue
				}

				coinbase :=
					dcrutil.Amount(block.Transactions[0].TxIn[0].ValueIn)

				// Pay dividends per the configured payment scheme and process
				// mature payments.
				switch h.cfg.PaymentMethod {
				case dividend.PPS:
					err := dividend.PayPerShare(h.db, coinbase, h.cfg.PoolFee,
						header.Height, h.cfg.ActiveNet.CoinbaseMaturity)
					if err != nil {
						log.Error(err)
						h.cancel()
						continue
					}

				case dividend.PPLNS:
					err := dividend.PayPerLastNShares(h.db, coinbase,
						h.cfg.PoolFee, header.Height,
						h.cfg.ActiveNet.CoinbaseMaturity, h.cfg.LastNPeriod)
					if err != nil {
						log.Error(err)
						h.cancel()
						continue
					}
				}
			}

			// Process matured payments.
			err = h.ProcessPayments(uint32(header.Height))
			if err != nil {
				log.Error(err)
			}

			// Update the accepted work record for the connected block.
			work.Connected = true
			work.ConnectedAtHeight = int64(header.Height)
			work.Update(h.db)

			// Prune accepted work that's recorded as connected to the
			// chain and is below the estimated reorg limit.
			err = PruneAcceptedWork(h.db, int64(header.Height-MaxReorgLimit))
			if err != nil {
				log.Error(err)
				h.cancel()
				continue
			}

			// Prune old processed payments.
			var lastPaymentPruneHeight uint32
			err = h.db.View(func(tx *bolt.Tx) error {
				pbkt := tx.Bucket(database.PoolBkt)
				if pbkt == nil {
					return database.ErrBucketNotFound(database.PoolBkt)
				}

				v := pbkt.Get(dividend.LastPaymentPruneHeight)
				if v != nil {
					lastPaymentPruneHeight = binary.LittleEndian.Uint32(v)
				}

				return nil
			})
			if err != nil {
				log.Error(err)
				h.cancel()
				continue
			}

			log.Tracef("last payments prune height is %v", lastPaymentPruneHeight)

			heightDiff := header.Height - lastPaymentPruneHeight
			if heightDiff >= dividend.DeleteThreshold {
				err := dividend.PruneProcessedPayments(h.db, header.Height)
				if err != nil {
					log.Error(err)
					h.cancel()
					continue
				}
			}

		case blkHeader := <-h.discCh:
			header, err := ParseBlockHeader(blkHeader)
			if err != nil {
				log.Errorf("failed to parse disconnected block header: %v", err)
				h.cancel()
			}

			log.Tracef("Block disconnected (hash: %v, height: %v)",
				header.BlockHash(), header.Height)

			nonce := FetchNonce(blkHeader)
			work, err := GetAcceptedWork(h.db, nonce)
			if err != nil {
				log.Error(err)
				h.cancel()
			}

			// If the disconnected block is an accepted work from the pool,
			// delete all associated payments and update the state of the
			// accepted work.
			if work != nil {
				work.Connected = false

				payments, err := dividend.FetchPendingPaymentsAtHeight(h.db,
					uint32(work.ConnectedAtHeight))
				if err != nil {
					log.Error(err)
					h.cancel()
				}

				for _, pmt := range payments {
					err = pmt.Delete(h.db)
					if err != nil {
						log.Error(err)
						h.cancel()
					}
				}

				work.ConnectedAtHeight = -1
				work.Update(h.db)
			}
		}
	}

	log.Info("Hub process handler done.")
}

// ProcessPayments fetches all eligible payments and publishes a
// transaction to the network paying dividends to participating accounts.
func (h *Hub) ProcessPayments(height uint32) error {
	// Fetch all eligible payments.
	eligiblePmts, err := dividend.FetchEligiblePaymentBundles(h.db, height,
		h.cfg.MinPayment)
	if err != nil {
		return err
	}

	if len(eligiblePmts) == 0 {
		return fmt.Errorf("no eligible payments to process")
	}

	// Generate the payment details from the eligible payments fetched.
	details, targetAmt, err := dividend.GeneratePaymentDetails(h.db,
		h.cfg.PoolFeeAddrs, eligiblePmts, h.cfg.MaxTxFeeReserve, &h.txFeeReserve)
	if err != nil {
		return err
	}

	log.Tracef("Target amount is : %v", targetAmt)

	// Create address-amount kv pairs for the transaction, using the payment
	// details.
	pmts := make(map[dcrutil.Address]dcrutil.Amount, len(details))
	for addrStr, amt := range details {
		addr, err := dcrutil.DecodeAddress(addrStr)
		if err != nil {
			return err
		}

		pmts[addr] = amt
	}

	// Publish the transaction.
	err = h.PublishTransaction(pmts, *targetAmt)
	if err != nil {
		return err
	}

	// Update all eligible payments published by the tx as paid.
	for _, bundle := range eligiblePmts {
		err = bundle.UpdateAsPaid(h.db, height)
		if err != nil {
			return err
		}
	}

	// Update the last payment paid on time.
	nowNano := time.Now().UnixNano()
	err = h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		return pbkt.Put(dividend.LastPaymentPaidOn,
			dividend.NanoToBigEndianBytes(nowNano))
	})
	if err != nil {
		return err
	}

	return nil
}
