package network

import (
	"context"
	"math/big"
	"math/rand"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coreos/bbolt"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrjson"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrwallet/rpc/walletrpc"
	"github.com/robfig/cron"
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
	cron           *cron.Cron
	ConnCount      uint64
	Ticker         *time.Ticker
}

// NewHub initializes a websocket hub.
func NewHub(db *bolt.DB, httpc *http.Client, hcfg *HubConfig, limiter *RateLimiter) (*Hub, error) {
	h := &Hub{
		db:          db,
		httpc:       httpc,
		limiter:     limiter,
		cfg:         hcfg,
		hashRate:    dividend.ZeroInt,
		poolTargets: make(map[string]uint32),
		cron:        cron.New(),
		Broadcast:   make(chan Message),
		ConnCount:   0,
		Ticker:      time.NewTicker(time.Second * 5),
	}

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

	log.Debugf("Pool targets are: %v", spew.Sdump(h.poolTargets))

	// Create handlers for chain notifications being subscribed for.
	ntfnHandlers := &rpcclient.NotificationHandlers{
		OnBlockConnected: func(blkHeader []byte, transactions [][]byte) {
			log.Debugf("Block connected: %x", blkHeader)

			// Check if the accepted block was mined by the pool.
			decoded, err := DecodeHeader(blkHeader)
			if err != nil {
				log.Error(err)
				return
			}

			nonce := FetchNonce(decoded)
			work, err := GetAcceptedWork(h.db, nonce)
			if err != nil {
				log.Error(err)
				return
			}

			header, err := ParseBlockHeader(decoded)
			if err != nil {
				log.Error(err)
				return
			}

			// If the connected block is an accepted work from the pool
			// record payouts to participating accounts.
			if work != nil {
				// Fetch the coinbase amount.
				blockHash := header.BlockHash()
				blockHeight := header.Height
				coinbase, err := h.FetchCoinbaseValue(&blockHash)
				if err != nil {
					log.Error(err)
					return
				}

				// Pay dividends per the configured payment scheme.
				switch h.cfg.PaymentMethod {
				case dividend.PPS:
					err := dividend.PayPerShare(h.db, *coinbase, h.cfg.PoolFee,
						blockHeight, h.cfg.ActiveNet.CoinbaseMaturity)
					if err != nil {
						log.Error(err)
						return
					}
				case dividend.PPLNS:
					err := dividend.PayPerLastNShares(h.db, *coinbase,
						h.cfg.PoolFee, blockHeight,
						h.cfg.ActiveNet.CoinbaseMaturity, h.cfg.LastNPeriod)
					if err != nil {
						log.Error(err)
						return
					}
				}

				// Update the accepted work record for the connected block.
				work.Connected = true
				work.ConnectedAtHeight = int64(header.Height)
				work.Update(h.db)

				// Prune accepted work that's recorded as connected to the
				// chain and is below the estimated reorg limit.
				err = PruneAcceptedWork(h.db,
					int64(header.Height-MaxReorgLimit))
				if err != nil {
					log.Error(err)
					return
				}
			}

			h.Broadcast <- ConnectedBlockNotification(header.Height)
		},
		OnBlockDisconnected: func(blkHeader []byte) {
			log.Debugf("Block disconnected: %x", blkHeader)

			// Check if the accepted block was mined by the pool.
			decoded, err := DecodeHeader(blkHeader)
			if err != nil {
				log.Error(err)
				return
			}

			nonce := FetchNonce(decoded)
			work, err := GetAcceptedWork(h.db, nonce)
			if err != nil {
				log.Error(err)
				return
			}

			// If the disconnected block is an accepted work from the pool
			// update the state of the accepted work.
			if work != nil {
				work.Connected = false
				work.ConnectedAtHeight = -1
			}

			if !h.HasConnectedClients() {
				return
			}

			height, err := FetchBlockHeight(blkHeader)
			if err != nil {
				log.Error(err)
				return
			}

			h.Broadcast <- DisconnectedBlockNotification(height)
		},
		OnWork: func(blkHeader string, target string) {
			log.Debugf("New Work (header: %v , target: %v)", blkHeader,
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
		return nil, err
	}

	h.rpcc = rpcc

	// Subscribe for chain notifications.
	if err := h.rpcc.NotifyWork(); err != nil {
		h.rpccMtx.Lock()
		h.rpcc.Shutdown()
		h.rpccMtx.Unlock()
		return nil, err
	}
	if err := h.rpcc.NotifyBlocks(); err != nil {
		h.rpccMtx.Lock()
		h.rpcc.Shutdown()
		h.rpccMtx.Unlock()
		return nil, err
	}

	log.Debugf("RPC connection established with dcrd.")

	// Establish GRPC connection with the wallet.
	creds, err := credentials.NewClientTLSFromFile(hcfg.WalletRPCCertFile,
		"localhost")
	if err != nil {
		return nil, err
	}

	h.gConn, err = grpc.Dial(hcfg.WalletGRPCHost,
		grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, err
	}

	log.Debugf("GRPC connection established with dcrwallet.")

	h.grpc = walletrpc.NewWalletServiceClient(h.gConn)

	h.StartPaymentCron()

	return h, nil
}

// Shutdown terminates all connected clients to the hub and releases all
// resources used.
func (h *Hub) Shutdown() {
	h.cron.Stop()
	log.Debugf("Dividend payment cron stopped.")

	h.Broadcast <- nil
	h.gConn.Close()
	h.rpcc.Shutdown()
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

// FetchCoinbaseValue returns the coinbase value of the provided block hash.
func (h *Hub) FetchCoinbaseValue(blockHash *chainhash.Hash) (*dcrutil.Amount, error) {
	h.rpccMtx.Lock()
	block, err := h.rpcc.GetBlock(blockHash)
	if err != nil {
		return nil, err
	}
	h.rpccMtx.Unlock()

	coinbaseAmt := dcrutil.Amount(block.Transactions[0].TxIn[0].ValueIn)
	return &coinbaseAmt, nil
}

// AddHashRate adds the hash power provided by the newly connected client
//to the total estimated hash power of pool.
func (h *Hub) AddHashRate(miner string) {
	hash := dividend.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Add(h.hashRate, hash)
	log.Debugf("Client connected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// RemoveHashRate removes the hash power previously provided by the
// disconnected client from the total estimated hash power of the pool.
func (h *Hub) RemoveHashRate(miner string) {
	hash := dividend.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Sub(h.hashRate, hash)
	log.Debugf("Client disconnected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// PublishTransaction creates a transaction paying pool accounts for work done.
func (h *Hub) PublishTransaction(payouts map[dcrutil.Address]dcrutil.Amount, targetAmt dcrutil.Amount) error {
	// Fund dividend payouts.
	fundTxReq := &walletrpc.FundTransactionRequest{
		RequiredConfirmations: int32(h.cfg.ActiveNet.CoinbaseMaturity),
		TargetAmount:          int64(targetAmt),
		IncludeChangeScript:   false,
	}

	fundTxResp, err := h.grpc.FundTransaction(context.TODO(), fundTxReq)
	if err != nil {
		return err
	}

	// Create the transaction.
	txInputs := make([]dcrjson.TransactionInput, 0)
	for _, utxo := range fundTxResp.SelectedOutputs {
		in := dcrjson.TransactionInput{
			Amount: float64(utxo.Amount),
			Txid:   string(utxo.TransactionHash),
			Vout:   utxo.OutputIndex,
			Tree:   int8(utxo.Tree),
		}
		txInputs = append(txInputs, in)
	}

	h.rpccMtx.Lock()
	msgTx, err := h.rpcc.CreateRawTransaction(txInputs, payouts, nil, nil)
	h.rpccMtx.Unlock()
	if err != nil {
		return err
	}

	msgTxBytes, err := msgTx.Bytes()
	if err != nil {
		return err
	}

	// Sign the transaction.
	signTxReq := &walletrpc.SignTransactionRequest{
		SerializedTransaction: msgTxBytes,
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

	log.Debugf("Published tx hash is: %v", pubTxResp.TransactionHash)

	return nil
}

// GeneratePaymentDetails generates kv pair of addresses and payment amounts
// from the provided eligible payments.
func (h *Hub) GeneratePaymentDetails(eligiblePmts []*dividend.PaymentBundle) (map[dcrutil.Address]dcrutil.Amount, *dcrutil.Amount, error) {
	// Generate the address and payment amount kv pairs.
	var targetAmt dcrutil.Amount
	pmts := make(map[dcrutil.Address]dcrutil.Amount, 0)
	for _, p := range eligiblePmts {
		var addr dcrutil.Address

		// For pool fee payments, fetch a pool fee address at random.
		if p.Account == dividend.PoolFeesK {
			rand.Seed(time.Now().UnixNano())
			addr = h.cfg.PoolFeeAddrs[rand.Intn(len(h.cfg.PoolFeeAddrs))]

			bundleAmt := p.Total()
			pmts[addr] = bundleAmt
			targetAmt += bundleAmt
			continue
		}

		// For a dividend payment, fetch the corresponding account address.
		acc, err := dividend.GetAccount(h.db, []byte(p.Account))
		if err != nil {
			return nil, nil, err
		}

		addr, err = dcrutil.DecodeAddress(acc.Address)
		if err != nil {
			return nil, nil, err
		}

		bundleAmt := p.Total()
		pmts[addr] = bundleAmt
		targetAmt += bundleAmt
	}

	return pmts, &targetAmt, nil
}

// ProcessPayments fetches all eligible payments and publishes a
// transaction to the network paying dividends to participating accounts.
func (h *Hub) ProcessPayments() error {
	// Fetch all eligible payments.
	eligiblePmts, err := dividend.FetchEligiblePayments(h.db, h.cfg.MinPayment)
	if err != nil {
		return err
	}

	// Generate the payment details from the eligible payments fetched.
	pmts, targetAmt, err := h.GeneratePaymentDetails(eligiblePmts)
	if err != nil {
		return err
	}

	// Publish the transaction.
	err = h.PublishTransaction(pmts, *targetAmt)
	if err != nil {
		return err
	}

	// Update all eligible payments published by the tx as paid.
	nowNano := time.Now().UnixNano()
	for _, bundle := range eligiblePmts {
		err = bundle.UpdateAsPaid(h.db, nowNano)
		if err != nil {
			return err
		}
	}

	// Update the last payment paid on time.
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

// StartPaymentCron starts the dividend payment cron.
func (h *Hub) StartPaymentCron() {
	h.cron.AddFunc("@every 6h",
		func() {
			err := h.ProcessPayments()
			if err != nil {
				log.Error("Failed to process payments: %v", err)
				return
			}

			log.Debugf("Sucessfully processed payments.")
		})
	h.cron.Start()
	log.Debugf("Started dividend payment cron.")
}
