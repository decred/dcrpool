// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

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

	bolt "github.com/coreos/bbolt"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrpool/database"
	"github.com/decred/dcrpool/dividend"
	"github.com/decred/dcrpool/util"
	"github.com/decred/dcrwallet/rpc/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	// MaxReorgLimit is an estimated maximum chain reorganization limit.
	// That is, it is highly improbable for the the chain to reorg beyond six
	// blocks from the chain tip.
	MaxReorgLimit = 6

	// getworkDataLen is the length of the data field of the getwork RPC.
	// It consists of the serialized block header plus the internal blake256
	// padding.  The internal blake256 padding consists of a single 1 bit
	// followed by zeros and a final 1 bit in order to pad the message out
	// to 56 bytes followed by length of the message in bits encoded as a
	// big-endian uint64 (8 bytes).  Thus, the resulting length is a
	// multiple of the blake256 block size (64 bytes).  Given the padding
	// requires at least a 1 bit and 64 bits for the padding, the following
	// converts the block header length and hash block size to bits in order
	// to ensure the correct number of hash blocks are calculated and then
	// multiplies the result by the block hash block size in bytes.
	getworkDataLen = (1 + ((wire.MaxBlockHeaderPayload*8 + 65) /
		(chainhash.HashBlockSize * 8))) * chainhash.HashBlockSize
)

var (
	// soloMaxGenTime is the threshold (in seconds) at which pool clients will
	// generate a valid share when solo pool mode is activated. This is set to a
	// high value to reduce the number of round trips to the pool by connected
	// pool clients since pool shares are a non factor in solo pool mode.
	soloMaxGenTime = new(big.Int).SetInt64(28)
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
	SoloPool          bool
	PoolFeeAddrs      []dcrutil.Address
	BackupPass        string
	Secret            string
}

// DifficultyData captures the pool target difficulty and pool difficulty
// ratio of a mining client.
type DifficultyData struct {
	target     *big.Int
	difficulty *big.Int
}

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	lastWorkHeight    uint32 // update atomically
	lastPaymentHeight uint32 // update atomically
	clients           int32  // update atomically

	db           *bolt.DB
	httpc        *http.Client
	cfg          *HubConfig
	limiter      *RateLimiter
	rpcc         *rpcclient.Client
	rpccMtx      sync.Mutex
	gConn        *grpc.ClientConn
	grpc         walletrpc.WalletServiceClient
	grpcMtx      sync.Mutex
	poolDiff     map[string]*DifficultyData
	poolDiffMtx  sync.RWMutex
	connCh       chan []byte
	discCh       chan []byte
	ctx          context.Context
	cancel       context.CancelFunc
	txFeeReserve dcrutil.Amount
	endpoints    []*Endpoint
	blake256Pad  []byte
	wg           sync.WaitGroup
}

// GenerateBlake256Pad generates the extra padding needed for work submission
// over the getwork RPC.
func (h *Hub) GenerateBlake256Pad() {
	h.blake256Pad = make([]byte, getworkDataLen-wire.MaxBlockHeaderPayload)
	h.blake256Pad[0] = 0x80
	h.blake256Pad[len(h.blake256Pad)-9] |= 0x01
	binary.BigEndian.PutUint64(h.blake256Pad[len(h.blake256Pad)-8:],
		wire.MaxBlockHeaderPayload*8)
}

// GenerateDifficultyData generates difficulty data for all known miners.
func (h *Hub) GenerateDifficultyData() error {
	maxGenTime := h.cfg.MaxGenTime
	if h.cfg.SoloPool {
		maxGenTime = soloMaxGenTime
	}

	log.Tracef("Max valid share generation time is: %v seconds", maxGenTime)

	h.poolDiffMtx.Lock()
	for miner, hashrate := range dividend.MinerHashes {
		target, difficulty, err := dividend.CalculatePoolTarget(h.cfg.ActiveNet,
			hashrate, maxGenTime)
		if err != nil {
			log.Error("Failed to calculate pool target and diff for miner "+
				"(%v): %v", err)
			return err
		}

		log.Tracef("difficulty for %v is %v", miner, difficulty)

		h.poolDiff[miner] = &DifficultyData{
			target:     target,
			difficulty: difficulty,
		}
	}
	h.poolDiffMtx.Unlock()
	return nil
}

// processWork parses work received and dispatches a work notification to all
// connected pool clients.
func (h *Hub) processWork(headerE string, target string) {
	heightD, err := hex.DecodeString(headerE[256:264])
	if err != nil {
		log.Errorf("Failed to decode block height: %v", err)
		return
	}

	height := binary.LittleEndian.Uint32(heightD)
	atomic.StoreUint32(&h.lastWorkHeight, height)

	log.Tracef("New work at height (%v) received (%v)", height, headerE)

	// Do not process work data id there are no connected pool clients.
	if !h.HasClients() {
		return
	}

	blockVersion := headerE[:8]
	prevBlock := headerE[8:72]
	genTx1 := headerE[72:288]
	nBits := headerE[232:240]
	nTime := headerE[272:280]
	genTx2 := headerE[352:360]

	// Create a job for the received work.
	job, err := NewJob(headerE, height)
	if err != nil {
		log.Errorf("Failed to create job: %v", err)
		return
	}

	err = job.Create(h.db)
	if err != nil {
		log.Errorf("Failed to persist job: %v", err)
		return
	}

	workNotif := WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)

	// Broadcast the work notification to connected pool clients.
	for _, endpoint := range h.endpoints {
		endpoint.clientsMtx.Lock()
		for _, client := range endpoint.clients {
			select {
			case client.ch <- workNotif:
			default:
			}
		}
		endpoint.clientsMtx.Unlock()
	}
}

// NewHub initializes a websocket hub.
func NewHub(ctx context.Context, cancel context.CancelFunc, db *bolt.DB, httpc *http.Client, hcfg *HubConfig, limiter *RateLimiter) (*Hub, error) {
	h := &Hub{
		db:       db,
		httpc:    httpc,
		limiter:  limiter,
		cfg:      hcfg,
		poolDiff: make(map[string]*DifficultyData),
		clients:  0,
		connCh:   make(chan []byte),
		discCh:   make(chan []byte),
		ctx:      ctx,
		cancel:   cancel,
	}

	h.GenerateBlake256Pad()

	if !h.cfg.SoloPool {
		log.Infof("Payment method is %v.", hcfg.PaymentMethod)
	} else {
		log.Infof("Solo pool enabled")
	}

	// Persist the pool mode.
	sp := uint32(0)
	if h.cfg.SoloPool {
		sp = 1
	}

	err := db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		vbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(vbytes, sp)
		err := pbkt.Put(database.SoloPool, vbytes)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Load the tx fee reserve, last payment height and mined blocks count.
	var lastPaymentHeight uint32
	err = db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)

		txFeeReserveB := pbkt.Get(database.TxFeeReserve)
		if txFeeReserveB == nil {
			log.Info("Tx fee reserve value not found in db, initializing.")
			h.txFeeReserve = dcrutil.Amount(0)
			tbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(tbytes, uint32(h.txFeeReserve))
			err := pbkt.Put(database.TxFeeReserve, tbytes)
			if err != nil {
				return err
			}
		}

		if txFeeReserveB != nil {
			feeReserve := binary.LittleEndian.Uint32(txFeeReserveB)
			h.txFeeReserve = dcrutil.Amount(feeReserve)
		}

		lastPaymentHeightB := pbkt.Get(database.LastPaymentHeight)
		if lastPaymentHeightB == nil {
			log.Info("Last payment height value not found in db, initializing.")
			atomic.StoreUint32(&h.lastPaymentHeight, 0)
			lbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(lbytes, 0)
			err := pbkt.Put(database.LastPaymentHeight, lbytes)
			if err != nil {
				return err
			}
		}

		if lastPaymentHeightB != nil {
			lastPaymentHeight = binary.LittleEndian.Uint32(lastPaymentHeightB)
			atomic.StoreUint32(&h.lastPaymentHeight, lastPaymentHeight)
		}

		return nil
	})
	if err != nil {
		log.Errorf("Failed to load cached values: %v", err)
		return nil, err
	}

	if !h.cfg.SoloPool {
		log.Tracef("Tx fee reserve is currently %v, with a max of %v",
			h.txFeeReserve, h.cfg.MaxTxFeeReserve)
	}

	log.Tracef("Last payment height is currently: %v", lastPaymentHeight)

	// Generate difficulty data for all known pool clients.
	err = h.GenerateDifficultyData()
	if err != nil {
		log.Error("Failed to generate difficulty data: %v", err)
		return nil, err
	}

	// Setup listeners for all known pool clients.
	for miner, port := range dividend.MinerPorts {
		endpoint, err := NewEndpoint(h, port, miner)
		if err != nil {
			log.Error("Failed to create listeners: %v", err)
			return nil, err
		}

		h.endpoints = append(h.endpoints, endpoint)
	}

	// Create handlers for chain notifications being subscribed for.
	ntfnHandlers := &rpcclient.NotificationHandlers{
		OnBlockConnected: func(headerB []byte, transactions [][]byte) {
			h.connCh <- headerB
		},

		OnBlockDisconnected: func(headerB []byte) {
			h.discCh <- headerB
		},

		// TODO: Switch to OnWork notifications when dcrd PR #1410 is merged.
		// OnWork: func(headerE string, target string) {
		// 	h.processWork(headerE, target)
		// },
	}

	// Establish RPC connection with dcrd.
	rpcc, err := rpcclient.New(hcfg.DcrdRPCCfg, ntfnHandlers)
	if err != nil {
		return nil, fmt.Errorf("rpc error (dcrd): %v", err)
	}

	h.rpcc = rpcc

	// Subscribe for chain notifications.

	// TODO: Subscribe for OnWork notifications when dcrd PR #1410 is merged.
	// if err := h.rpcc.NotifyWork(); err != nil {
	// 	h.rpccMtx.Lock()
	// 	h.rpcc.Shutdown()
	// 	h.rpccMtx.Unlock()
	// 	return nil, fmt.Errorf("notify work rpc error (dcrd): %v", err)
	// }

	if err := h.rpcc.NotifyBlocks(); err != nil {
		h.rpccMtx.Lock()
		h.rpcc.Shutdown()
		h.rpccMtx.Unlock()
		return nil, fmt.Errorf("notify blocks rpc error (dcrd): %v", err)
	}

	log.Infof("RPC connection established with dcrd.")

	// Establish GRPC connection with the wallet if not in solo pool mode.
	if !h.cfg.SoloPool {
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
			return nil, fmt.Errorf("failed to establish grpc with the wallet")
		}

		h.grpc = walletrpc.NewWalletServiceClient(h.gConn)

		req := &walletrpc.BalanceRequest{
			RequiredConfirmations: 1,
		}

		h.grpcMtx.Lock()
		_, err = h.grpc.Balance(context.TODO(), req)
		h.grpcMtx.Unlock()
		if err != nil {
			return nil, fmt.Errorf("grpc request error: %v", err)
		}

		log.Infof("GPRC connection established with wallet.")
	}

	return h, nil
}

// HasClients asserts the mining pool has clients.
func (h *Hub) HasClients() bool {
	return atomic.LoadInt32(&h.clients) > 0
}

// SubmitWork sends solved block data to the consensus daemon for evaluation.
func (h *Hub) SubmitWork(data *string) (bool, error) {
	h.rpccMtx.Lock()
	status, err := h.rpcc.GetWorkSubmit(*data)
	h.rpccMtx.Unlock()
	if err != nil {
		return false, err
	}

	return status, err
}

// GetWork fetches available work from the consensus daemon.
func (h *Hub) GetWork() (string, string, error) {
	h.rpccMtx.Lock()
	work, err := h.rpcc.GetWork()
	h.rpccMtx.Unlock()
	if err != nil {
		return "", "", err
	}

	return work.Data, work.Target, err
}

// PublishTransaction creates a transaction paying pool accounts for work done.
func (h *Hub) PublishTransaction(payouts map[dcrutil.Address]dcrutil.Amount, targetAmt dcrutil.Amount) (string, error) {
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
		RequiredConfirmations:    1,
		OutputSelectionAlgorithm: walletrpc.ConstructTransactionRequest_ALL,
		NonChangeOutputs:         outs,
	}

	h.grpcMtx.Lock()
	constructTxResp, err := h.grpc.ConstructTransaction(context.TODO(), constructTxReq)
	h.grpcMtx.Unlock()
	if err != nil {
		return "", err
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
		return "", err
	}

	// Publish the transaction.
	pubTxReq := &walletrpc.PublishTransactionRequest{
		SignedTransaction: signedTxResp.Transaction,
	}

	h.grpcMtx.Lock()
	pubTxResp, err := h.grpc.PublishTransaction(context.TODO(), pubTxReq)
	h.grpcMtx.Unlock()
	if err != nil {
		return "", err
	}

	txid := fmt.Sprintf("%x", pubTxResp.TransactionHash)

	log.Debugf("Published tx hash is: %s", txid)

	return txid, nil
}

// handleGetWork periodically fetches available work from the consensus daemon.
// It must be run as a goroutine.
func (h *Hub) handleGetWork(ctx context.Context) {
	var currHeaderE string
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	h.wg.Add(1)
	log.Trace("Started work handler.")

	for {
		select {
		case <-ctx.Done():
			log.Trace("Work handler done.")
			h.wg.Done()
			return
		case <-ticker.C:
			headerE, target, err := h.GetWork()
			if err != nil {
				log.Errorf("Failed to fetch work: %v", err)
				continue
			}

			// Process incoming work if there is no current work.
			if currHeaderE == "" {
				log.Tracef("updated work based on no current work being" +
					" available")
				currHeaderE = headerE
				h.processWork(currHeaderE, target)
				continue
			}

			// Process incoming work if it has a higher height than the
			// current work.
			currHeaderHeightD, err := hex.DecodeString(currHeaderE[256:264])
			if err != nil {
				log.Errorf("Failed to decode current header block height: %v",
					err)
				h.cancel()
				continue
			}

			currHeaderHeight := binary.LittleEndian.Uint32(currHeaderHeightD)

			newHeaderHeightD, err := hex.DecodeString(headerE[256:264])
			if err != nil {
				log.Errorf("Failed to decode new header block height: %v",
					err)
				h.cancel()
				continue
			}

			newHeaderHeight := binary.LittleEndian.Uint32(newHeaderHeightD)
			if newHeaderHeight > currHeaderHeight {
				log.Tracef("updated work based on new work having a higher"+
					" height than the current work: %v > %v", newHeaderHeight,
					currHeaderHeight)
				currHeaderE = headerE
				h.processWork(currHeaderE, target)
				continue
			}

			// Process incoming work if it has more votes than the current work.
			currHeaderVotersD, err := hex.DecodeString(currHeaderE[216:220])
			if err != nil {
				log.Errorf("Failed to decode current header block voters: %v",
					err)
				h.cancel()
				continue
			}

			currHeaderVoters := binary.LittleEndian.Uint16(currHeaderVotersD)

			newHeaderVotersD, err := hex.DecodeString(headerE[216:220])
			if err != nil {
				log.Errorf("Failed to decode new header blockvoters: %v",
					err)
				h.cancel()
				continue
			}

			newHeaderVoters :=
				binary.LittleEndian.Uint16(newHeaderVotersD)
			if newHeaderVoters > currHeaderVoters {
				log.Tracef("updated work based on new work having more voters"+
					" than the current work, %v > %v", newHeaderVoters,
					currHeaderVoters)
				currHeaderE = headerE
				h.processWork(currHeaderE, target)
				continue
			}

			// Process incoming work if it is at least 30 seconds older than
			// the current work.
			currNTimeD, err := hex.DecodeString(currHeaderE[272:280])
			if err != nil {
				log.Errorf("Failed to decode current header block time: %v",
					err)
				return
			}

			currNTime :=
				time.Unix(int64(binary.LittleEndian.Uint32(currNTimeD)), 0)

			newNTimeD, err := hex.DecodeString(headerE[272:280])
			if err != nil {
				log.Errorf("Failed to decode new header block time: %v",
					err)
				return
			}

			newNTime :=
				time.Unix(int64(binary.LittleEndian.Uint32(newNTimeD)), 0)
			timeDiff := newNTime.Sub(currNTime)
			if timeDiff >= time.Second*30 {
				log.Tracef("updated work based on new work being 30 or more"+
					" seconds (%v) younger than the current work", timeDiff)
				currHeaderE = headerE
				h.processWork(currHeaderE, target)
				continue
			}
		}
	}
}

// handleChainUpdates processes connected and disconnected block notifications
// from the consensus daemon.
func (h *Hub) handleChainUpdates(ctx context.Context) {
	h.wg.Add(1)
	log.Trace("Started chain updates handler.")

	for {
		select {
		case <-ctx.Done():
			log.Trace("Chain updates handler done.")
			h.wg.Done()
			return

		case headerB := <-h.connCh:
			var header wire.BlockHeader
			err := header.FromBytes(headerB)
			if err != nil {
				log.Errorf("unable to create header from bytes: %v", err)
				h.cancel()
				continue
			}
			log.Tracef("Block connected at height %v", header.Height)

			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err := PruneJobs(h.db, pruneLimit)
				if err != nil {
					log.Errorf("unable to prune jobs to height %d: %v",
						pruneLimit, err)
					h.cancel()
					continue
				}

				log.Tracef("Pruned jobs below height: %v", pruneLimit)
			}

			// If the parent of the connected block is an accepted work of the
			// pool, confirmed it as mined. The parent of a connected block
			// at this point is guaranteed to have its corresponding accepted
			// work persisted if it was mined by the pool.
			parentID := AcceptedWorkID(header.PrevBlock.String(),
				header.Height-1)
			work, err := FetchAcceptedWork(h.db, parentID)
			if err != nil {
				log.Errorf(
					"unable to fetch accepted work for block #%v's parent"+
						" (%v) : %v", header.Height,
					header.PrevBlock.String(), err)
				continue
			}

			if work == nil {
				log.Tracef("No mined work found for block #%v's parent (%v)",
					header.Height, header.PrevBlock.String())
				continue
			}

			// Update accepted work as confirmed mined.
			work.Confirmed = true
			err = work.Update(h.db)
			if err != nil {
				log.Errorf("unable to confirm accepted work for block"+
					"(%v): %v", header.PrevBlock.String(), err)
				h.cancel()
				continue
			}

			// Prune accepted work that is below the estimated reorg limit.
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err = PruneAcceptedWork(h.db, pruneLimit)
				if err != nil {
					log.Errorf("unable to prune accepted work below"+
						" height (%v): %v", pruneLimit, err)
					h.cancel()
					continue
				}

				log.Tracef("Pruned accepted work below height: %v", pruneLimit)
			}

			log.Tracef("Found mined parent (%v) for connected block (%v)",
				header.PrevBlock.String(), header.BlockHash().String())

			// Fetch the coinbase of the the confirmed accepted work
			// (the parent of the connected block) and process payments
			//  when not mining in solo pool mode.
			if !h.cfg.SoloPool {
				h.rpccMtx.Lock()
				block, err := h.rpcc.GetBlock(&header.PrevBlock)
				h.rpccMtx.Unlock()
				if err != nil {
					log.Errorf("unable to fetch block (%v): %v",
						header.PrevBlock.String(), err)
					h.cancel()
					continue
				}

				coinbase :=
					dcrutil.Amount(block.Transactions[0].TxOut[2].Value)

				log.Tracef("Accepted work (%v) at height %v has coinbase"+
					" of %v", header.PrevBlock.String(), header.Height-1,
					coinbase)

				// Pay dividends per the configured payment scheme and process
				// mature payments.
				switch h.cfg.PaymentMethod {
				case dividend.PPS:
					err := dividend.PayPerShare(h.db, coinbase, h.cfg.PoolFee,
						header.Height, h.cfg.ActiveNet.CoinbaseMaturity)
					if err != nil {
						log.Error("unable to process generate PPS shares: %v", err)
						h.cancel()
						continue
					}

				case dividend.PPLNS:
					err := dividend.PayPerLastNShares(h.db, coinbase,
						h.cfg.PoolFee, header.Height,
						h.cfg.ActiveNet.CoinbaseMaturity, h.cfg.LastNPeriod)
					if err != nil {
						log.Errorf("unable to generate PPLNS shares: %v", err)
						h.cancel()
						continue
					}
				}

				// Process mature payments.
				err = h.ProcessPayments(header.Height)
				if err != nil {
					log.Errorf("unable to process payments: %v", err)
				}
			}

		case headerB := <-h.discCh:
			var header wire.BlockHeader
			err := header.FromBytes(headerB)
			if err != nil {
				log.Errorf("unable to create header from bytes: %v", err)
				h.cancel()
				continue
			}
			log.Tracef("Block disconnected at height %v", header.Height)

			// Delete mined work if it is disconnected from the chain. At this
			// point a mined confirmed block will have its corresponding
			// accepted block record persisted.
			id := AcceptedWorkID(header.BlockHash().String(), header.Height)
			work, err := FetchAcceptedWork(h.db, id)
			if err != nil {
				log.Errorf("unable to fetch mined work: %v", err)
				continue
			}

			err = work.Delete(h.db)
			if err != nil {
				log.Errorf("unable to delete mined work: %v", err)
				h.cancel()
				continue
			}

			// Only remove invalidated payments if not mining in solo pool mode.
			if !h.cfg.SoloPool {
				// If the disconnected block is an accepted work from the pool,
				// delete all associated payments.
				payments, err := dividend.FetchPendingPaymentsAtHeight(h.db,
					header.Height)
				if err != nil {
					log.Errorf("Failed to fetch pending payments"+
						" at height (%v): %v", header.Height, err)
					h.cancel()
					continue
				}

				for _, pmt := range payments {
					err = pmt.Delete(h.db)
					if err != nil {
						log.Errorf("unable to delete pending payment", err)
						h.cancel()
						break
					}
				}
			}
		}
	}
}

// shutdown tears down the hub and releases resources used.
func (h *Hub) shutdown() {
	// Close the wallet grpc connection if in pooled mining mode.
	if !h.cfg.SoloPool {
		h.gConn.Close()
	}

	// Shutdown the daemon rpc connection.
	h.rpccMtx.Lock()
	h.rpcc.Shutdown()
	h.rpccMtx.Unlock()

	// Close the connection and disconnection channels.
	close(h.discCh)
	close(h.connCh)

	h.db.Close()
}

// run handles the process lifecycles of the pool hub.
func (h *Hub) Run(ctx context.Context) {
	h.wg.Add(len(h.endpoints))
	for _, e := range h.endpoints {
		go e.listen()
		go e.connect(h.ctx)
	}

	go h.handleGetWork(h.ctx)
	go h.handleChainUpdates(h.ctx)
	h.wg.Wait()

	h.shutdown()
}

// ProcessPayments fetches all eligible payments and publishes a
// transaction to the network paying dividends to participating accounts.
func (h *Hub) ProcessPayments(height uint32) error {
	// Waiting two blocks after a successful payment before proceeding with
	// another one because the reserved amount for transaction fees becomes
	// change after a successful transaction. Change matures after the next
	// block is processed. The second block is as a result of trying to
	// maximize the transaction fee usage by processing mature payments
	// after the transaction fees reserve has matured and ready for another
	// transaction.
	lastPaymentHeight := atomic.LoadUint32(&h.lastPaymentHeight)
	if lastPaymentHeight != 0 && (height-lastPaymentHeight) < 3 {
		return nil
	}

	// Fetch all eligible payments.
	eligiblePmts, err := dividend.FetchEligiblePaymentBundles(h.db, height,
		h.cfg.MinPayment)
	if err != nil {
		return err
	}

	if len(eligiblePmts) == 0 {
		log.Infof("no eligible payments to process")
		return nil
	}

	log.Tracef("eligible payments are: %v", spew.Sdump(eligiblePmts))

	// Generate the payment details from the eligible payments fetched.
	details, targetAmt, err := dividend.GeneratePaymentDetails(h.db,
		h.cfg.PoolFeeAddrs, eligiblePmts, h.cfg.MaxTxFeeReserve, &h.txFeeReserve)
	if err != nil {
		return err
	}

	log.Tracef("mature rewards at height (%v) is: %v", height, targetAmt)

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
	txid, err := h.PublishTransaction(pmts, *targetAmt)
	if err != nil {
		return err
	}

	// Update all payments published by the tx as paid and archive them.
	for _, bundle := range eligiblePmts {
		bundle.UpdateAsPaid(h.db, height, txid)
		err = bundle.ArchivePayments(h.db)
		if err != nil {
			return err
		}
	}

	// Update the last payment paid on time, the last payment height and the
	// tx fee reserve.
	nowNano := time.Now().UnixNano()
	err = h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		if pbkt == nil {
			return database.ErrBucketNotFound(database.PoolBkt)
		}

		err := pbkt.Put(database.LastPaymentPaidOn,
			util.NanoToBigEndianBytes(nowNano))
		if err != nil {
			return err
		}

		// Update and persist the last payment height.
		atomic.StoreUint32(&h.lastPaymentHeight, height)
		hbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(hbytes, height)
		err = pbkt.Put(database.LastPaymentHeight, hbytes)
		if err != nil {
			return err
		}

		// Persist the tx fee reserve.
		tbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(tbytes, uint32(h.txFeeReserve))
		return pbkt.Put(database.TxFeeReserve, tbytes)
	})
	return err
}

type ClientInfo struct {
	Miner    string
	IP       string
	HashRate *big.Rat
}

// FetchClientInfo returns information about all connected clients
func (h *Hub) FetchClientInfo() map[string][]*ClientInfo {
	clientInfo := make(map[string][]*ClientInfo)

	// Iterate through all connected miners
	for _, endpoint := range h.endpoints {
		endpoint.clientsMtx.Lock()
		for _, client := range endpoint.clients {
			client.hashRateMtx.RLock()
			clientInfo[client.account] = append(clientInfo[client.account], &ClientInfo{
				Miner:    endpoint.miner,
				IP:       client.ip,
				HashRate: client.hashRate,
			})
			client.hashRateMtx.RUnlock()
		}
		endpoint.clientsMtx.Unlock()
	}
	return clientInfo
}

// PoolStats details the pool's work and payment statistics.
type PoolStats struct {
	LastWorkHeight    uint32
	LastPaymentHeight uint32
	MinedWork         []*AcceptedWork
}

// FetchPoolStats returns last height work was generated for.
func (h *Hub) FetchPoolStats() (*PoolStats, error) {
	poolStats := &PoolStats{
		LastWorkHeight: atomic.LoadUint32(&h.lastWorkHeight),
	}

	if !h.cfg.SoloPool {
		poolStats.LastPaymentHeight = atomic.LoadUint32(&h.lastPaymentHeight)
	}

	work, err := ListMinedWork(h.db, 10)
	poolStats.MinedWork = work

	return poolStats, err
}

// Quota details the portion of mining rewrds due an account for work
// contributed to the pool.
type Quota struct {
	AccountID  string
	Percentage *big.Rat
}

// FetchWorkQuotas returns the reward distribution to pool accounts
// based on work contributed per the peyment scheme used by the pool.
func (h *Hub) FetchWorkQuotas() ([]Quota, error) {
	if h.cfg.SoloPool {
		return nil, nil
	}

	height := atomic.LoadUint32(&h.lastWorkHeight)

	var percentages map[string]*big.Rat
	var err error
	if h.cfg.PaymentMethod == dividend.PPS {
		percentages, err =
			dividend.CalculatePPSSharePercentages(h.db, h.cfg.PoolFee, height)
	}

	if h.cfg.PaymentMethod == dividend.PPLNS {
		percentages, err =
			dividend.CalculatePPLNSSharePercentages(h.db, h.cfg.PoolFee,
				height, h.cfg.LastNPeriod)
	}

	if err != nil {
		return nil, err
	}

	quotas := make([]Quota, 0)
	for key, value := range percentages {
		quotas = append(quotas, Quota{
			AccountID:  key,
			Percentage: value,
		})
	}

	return quotas, nil
}

// FetchMinedWorkByAddress returns a list of mined work by the provided address.
// List is ordered, most recent comes first.
func (h *Hub) FetchMinedWorkByAddress(id string) ([]*AcceptedWork, error) {
	work, err := ListMinedWorkByAccount(h.db, id, 10)
	return work, err
}

// FetchPaymentsForAddress returns a list or payments made to the provided address.
// List is ordered, most recent comes first.
func (h *Hub) FetchPaymentsForAddress(id string) ([]*dividend.Payment, error) {
	payments, err := dividend.FetchArchivedPaymentsForAccount(h.db, id, 10)
	return payments, err
}

// AccountExists asserts if the provided account id references a pool account.
func (h *Hub) AccountExists(accountID string) bool {
	_, err := dividend.FetchAccount(h.db, []byte(accountID))
	if err != nil {
		log.Tracef("Unable to fetch account for id: %s", accountID)
		return false
	}

	return true
}
