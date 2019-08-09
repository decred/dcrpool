// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
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

	CPU           = "cpu"
	InnosiliconD9 = "innosilicond9"
	AntminerDR3   = "antminerdr3"
	AntminerDR5   = "antminerdr5"
	WhatsminerD1  = "whatsminerd1"
)

var (
	// soloMaxGenTime is the threshold (in seconds) at which pool clients will
	// generate a valid share when solo pool mode is activated. This is set to a
	// high value to reduce the number of round trips to the pool by connected
	// pool clients since pool shares are a non factor in solo pool mode.
	soloMaxGenTime = new(big.Int).SetInt64(28)

	// minerHashes is a map of all known DCR miners and their coressponding
	// hashrates.
	minerHashes = map[string]*big.Int{
		CPU:           new(big.Int).SetInt64(70E3),
		InnosiliconD9: new(big.Int).SetInt64(2.4E12),
		AntminerDR3:   new(big.Int).SetInt64(7.8E12),
		AntminerDR5:   new(big.Int).SetInt64(35E12),
		WhatsminerD1:  new(big.Int).SetInt64(48E12),
	}
)

// HubConfig represents configuration details for the hub.
type HubConfig struct {
	ActiveNet         *chaincfg.Params
	DcrdRPCCfg        *rpcclient.ConnConfig
	PoolFee           float64
	MaxTxFeeReserve   dcrutil.Amount
	MaxGenTime        uint64
	WalletRPCCertFile string
	WalletGRPCHost    string
	PaymentMethod     string
	LastNPeriod       uint32
	WalletPass        string
	MinPayment        dcrutil.Amount
	DBFile            string
	SoloPool          bool
	PoolFeeAddrs      []dcrutil.Address
	BackupPass        string
	Secret            string
	NonceIterations   float64
	MinerPorts        map[string]uint32
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
	endpoints    []*endpoint
	blake256Pad  []byte
	wg           sync.WaitGroup
}

// GenerateBlake256Pad generates the extra padding needed for work submission
// over the getwork RPC.
func (h *Hub) generateBlake256Pad() {
	h.blake256Pad = make([]byte, getworkDataLen-wire.MaxBlockHeaderPayload)
	h.blake256Pad[0] = 0x80
	h.blake256Pad[len(h.blake256Pad)-9] |= 0x01
	binary.BigEndian.PutUint64(h.blake256Pad[len(h.blake256Pad)-8:],
		wire.MaxBlockHeaderPayload*8)
}

// initDB handles the creation, upgrading and backup of the pool database.
func (h *Hub) initDB() error {
	db, err := openDB(h.cfg.DBFile)
	if err != nil {
		return MakeError(ErrDBOpen, "unable to open db file", err)
	}

	h.db = db
	err = createBuckets(h.db)
	if err != nil {
		return err
	}

	err = upgradeDB(h.db)
	if err != nil {
		return err
	}

	var switchMode bool
	err = db.View(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			return err
		}

		v := pbkt.Get(soloPool)
		if v == nil {
			return nil
		}

		spMode := binary.LittleEndian.Uint32(v) == 1
		if h.cfg.SoloPool != spMode {
			switchMode = true
		}

		return nil
	})
	if err != nil {
		return err
	}

	// If the pool mode changed, backup the current database and purge the
	// database.
	if switchMode {
		log.Info("Pool mode changed, backing up database before purge.")
		err := backup(h.db)
		if err != nil {
			return err
		}

		err = purge(h.db)
		if err != nil {
			return err
		}
	}

	return nil
}

// generateDifficultyData generates difficulty data for all supported miners.
func (h *Hub) generateDifficultyData() error {
	maxGenTime := new(big.Int).SetUint64(h.cfg.MaxGenTime)
	if h.cfg.SoloPool {
		maxGenTime = soloMaxGenTime
	}

	log.Infof("Max valid share generation time is: %v seconds", maxGenTime)

	h.poolDiffMtx.Lock()
	for miner, hashrate := range minerHashes {
		target, difficulty, err := calculatePoolTarget(h.cfg.ActiveNet,
			hashrate, maxGenTime)
		if err != nil {
			desc := fmt.Sprintf("failed to calculate pool target for %s", miner)
			return MakeError(ErrCalcPoolTarget, desc, err)
		}

		log.Infof("difficulty for %s is %v", miner, difficulty)

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
		log.Errorf("failed to decode block height %s: %v", string(heightD), err)
		return
	}

	height := binary.LittleEndian.Uint32(heightD)
	atomic.StoreUint32(&h.lastWorkHeight, height)

	log.Tracef("New work at height #%d received: %s", height, headerE)

	// Do not process work data if there are no connected pool clients.
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
		log.Errorf("failed to create job: %v", err)
		return
	}

	err = job.Create(h.db)
	if err != nil {
		log.Errorf("failed to persist job: %v", err)
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
func NewHub(ctx context.Context, cancel context.CancelFunc, httpc *http.Client, hcfg *HubConfig, limiter *RateLimiter) (*Hub, error) {
	h := &Hub{
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

	err := h.initDB()
	if err != nil {
		return nil, err
	}

	h.generateBlake256Pad()

	if !h.cfg.SoloPool {
		log.Infof("Payment method is %s.", hcfg.PaymentMethod)
	} else {
		log.Infof("Solo pool enabled")
	}

	// Persist the pool mode.
	sp := uint32(0)
	if h.cfg.SoloPool {
		sp = 1
	}

	err = h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		vbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(vbytes, sp)
		err := pbkt.Put(soloPool, vbytes)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Load the tx fee reserve, last payment height and mined blocks count.
	var lastPmtHeight uint32
	err = h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)

		txFeeReserveB := pbkt.Get(txFeeReserve)
		if txFeeReserveB == nil {
			log.Info("Tx fee reserve value not found in db, initializing.")
			h.txFeeReserve = dcrutil.Amount(0)
			tbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(tbytes, uint32(h.txFeeReserve))
			err := pbkt.Put(txFeeReserve, tbytes)
			if err != nil {
				return err
			}
		}

		if txFeeReserveB != nil {
			feeReserve := binary.LittleEndian.Uint32(txFeeReserveB)
			h.txFeeReserve = dcrutil.Amount(feeReserve)
		}

		lastPaymentHeightB := pbkt.Get(lastPaymentHeight)
		if lastPaymentHeightB == nil {
			log.Info("Last payment height value not found in db, initializing.")
			atomic.StoreUint32(&h.lastPaymentHeight, 0)
			lbytes := make([]byte, 4)
			binary.LittleEndian.PutUint32(lbytes, 0)
			err := pbkt.Put(lastPaymentHeight, lbytes)
			if err != nil {
				return err
			}
		}

		if lastPaymentHeightB != nil {
			lastPmtHeight = binary.LittleEndian.Uint32(lastPaymentHeightB)
			atomic.StoreUint32(&h.lastPaymentHeight, lastPmtHeight)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	if !h.cfg.SoloPool {
		log.Tracef("Tx fee reserve is currently %v, with a max of %v",
			h.txFeeReserve, h.cfg.MaxTxFeeReserve)
	}

	log.Tracef("Last payment height is currently: %d", lastPmtHeight)

	err = h.generateDifficultyData()
	if err != nil {
		return nil, err
	}

	// Setup listeners for all supported pool clients.
	for miner, port := range h.cfg.MinerPorts {
		endpoint, err := newEndpoint(h, port, miner)
		if err != nil {
			return nil, MakeError(ErrOther, "failed to create listeners", err)
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
		return nil, MakeError(ErrOther, "dcrd rpc error", err)
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
		return nil, MakeError(ErrOther, "dcrd notify blocks error", err)
	}

	log.Tracef("rpc connection established with dcrd.")

	// Establish GRPC connection with the wallet if not in solo pool mode.
	if !h.cfg.SoloPool {
		creds, err := credentials.NewClientTLSFromFile(hcfg.WalletRPCCertFile,
			"localhost")
		if err != nil {
			return nil, MakeError(ErrOther, "dcrwallet grpc tls error", err)
		}

		h.gConn, err = grpc.Dial(hcfg.WalletGRPCHost,
			grpc.WithTransportCredentials(creds))
		if err != nil {
			return nil, MakeError(ErrOther, "dcrwallet grpc dial error", err)
		}

		if h.gConn == nil {
			desc := "failed to establish grpc with dcrwallet"
			return nil, MakeError(ErrOther, desc, nil)
		}

		h.grpc = walletrpc.NewWalletServiceClient(h.gConn)

		req := &walletrpc.BalanceRequest{
			RequiredConfirmations: 1,
		}

		h.grpcMtx.Lock()
		_, err = h.grpc.Balance(context.TODO(), req)
		h.grpcMtx.Unlock()
		if err != nil {
			return nil, MakeError(ErrOther, "dcrwallet grpc request error", err)
		}

		log.Infof("grpc connection established with dcrwallet.")
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

	txid, err := chainhash.NewHash(pubTxResp.TransactionHash)
	if err != nil {
		return "", err
	}

	log.Tracef("published tx hash is %s", txid.String())

	return txid.String(), nil
}

// handleGetWork periodically fetches available work from the consensus daemon.
// It must be run as a goroutine.
func (h *Hub) handleGetWork(ctx context.Context) {
	var currHeaderE string
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	h.wg.Add(1)

	for {
		select {
		case <-ctx.Done():
			h.wg.Done()
			return
		case <-ticker.C:
			headerE, target, err := h.GetWork()
			if err != nil {
				log.Errorf("failed to fetch work: %v", err)
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
				log.Errorf("failed to decode current header block height: %v",
					err)
				h.cancel()
				continue
			}

			currHeaderHeight := binary.LittleEndian.Uint32(currHeaderHeightD)

			newHeaderHeightD, err := hex.DecodeString(headerE[256:264])
			if err != nil {
				log.Errorf("failed to decode new header block height: %v",
					err)
				h.cancel()
				continue
			}

			newHeaderHeight := binary.LittleEndian.Uint32(newHeaderHeightD)
			if newHeaderHeight > currHeaderHeight {
				log.Tracef("updated work based on new work having a higher"+
					" height than the current work: %d > %d",
					newHeaderHeight, currHeaderHeight)
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
				log.Errorf("failed to decode new header blockvoters: %v", err)
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
				log.Errorf("failed to decode current header block time: %v", err)
				return
			}

			currNTime :=
				time.Unix(int64(binary.LittleEndian.Uint32(currNTimeD)), 0)

			newNTimeD, err := hex.DecodeString(headerE[272:280])
			if err != nil {
				log.Errorf("failed to decode new header block time: %v", err)
				return
			}

			newNTime :=
				time.Unix(int64(binary.LittleEndian.Uint32(newNTimeD)), 0)
			timeDiff := newNTime.Sub(currNTime)
			if timeDiff >= time.Second*30 {
				log.Tracef("updated work based on new work being 30 "+
					"or more seconds younger than the current work, "+
					"%v second exactly", timeDiff)
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

	for {
		select {
		case <-ctx.Done():
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
			log.Tracef("Block connected at height #%d", header.Height)

			// Process mature payments.
			err = h.processPayments(header.Height)
			if err != nil {
				log.Errorf("unable to process payments: %v", err)
			}

			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err := PruneJobs(h.db, pruneLimit)
				if err != nil {
					log.Errorf("unable to prune jobs to height %d: %v",
						pruneLimit, err)
					h.cancel()
					continue
				}

				log.Tracef("Pruned jobs below height #%d", pruneLimit)
			}

			// If the parent of the connected block is an accepted work of the
			// pool, confirm it as mined. The parent of a connected block
			// at this point is guaranteed to have its corresponding accepted
			// work persisted if it was mined by the pool.
			parentID := AcceptedWorkID(header.PrevBlock.String(), header.Height-1)
			work, err := FetchAcceptedWork(h.db, parentID)
			if err != nil {
				log.Errorf("unable to fetch accepted work for block #%d's "+
					"parent %s : %v", header.Height,
					header.PrevBlock.String(), err)
				continue
			}

			if work == nil {
				log.Tracef("No mined work found for block #%v's parent %s",
					header.Height, header.PrevBlock.String())
				continue
			}

			// Update accepted work as confirmed mined.
			work.Confirmed = true
			err = work.Update(h.db)
			if err != nil {
				log.Errorf("unable to confirm accepted work for block "+
					"%s: %v", header.PrevBlock.String(), err)
				h.cancel()
				continue
			}

			// Prune accepted work that is below the estimated reorg limit.
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err = PruneAcceptedWork(h.db, pruneLimit)
				if err != nil {
					log.Errorf("unable to prune accepted work below "+
						"height #%d: %v", pruneLimit, err)
					h.cancel()
					continue
				}

				log.Tracef("Pruned accepted work below height #%d", pruneLimit)
			}

			log.Tracef("Found mined parent %s for connected block %s",
				header.PrevBlock.String(), header.BlockHash().String())

			// Fetch the coinbase of the the confirmed accepted work
			// (the parent of the connected block) and process payments
			//  when not mining in solo pool mode.
			if !h.cfg.SoloPool {
				h.rpccMtx.Lock()
				block, err := h.rpcc.GetBlock(&header.PrevBlock)
				h.rpccMtx.Unlock()
				if err != nil {
					log.Errorf("unable to fetch block %s: %v",
						header.PrevBlock.String(), err)
					h.cancel()
					continue
				}

				coinbase :=
					dcrutil.Amount(block.Transactions[0].TxOut[2].Value)

				log.Tracef("Accepted work %s at height #%d has coinbase"+
					" of %v", header.PrevBlock.String(), header.Height-1,
					coinbase)

				// Pay dividends per the configured payment scheme and process
				// mature payments.
				switch h.cfg.PaymentMethod {
				case PPS:
					err := PayPerShare(h.db, coinbase, h.cfg.PoolFee,
						header.Height, h.cfg.ActiveNet.CoinbaseMaturity)
					if err != nil {
						log.Error("unable to generate PPS shares: %v", err)
						h.cancel()
						continue
					}

				case PPLNS:
					err := PayPerLastNShares(h.db, coinbase,
						h.cfg.PoolFee, header.Height,
						h.cfg.ActiveNet.CoinbaseMaturity, h.cfg.LastNPeriod)
					if err != nil {
						log.Errorf("unable to generate PPLNS shares: %v", err)
						h.cancel()
						continue
					}
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

			log.Tracef("Block disconnected at height #%s", header.Height)

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
				payments, err := FetchPendingPaymentsAtHeight(h.db,
					header.Height)
				if err != nil {
					log.Errorf("failed to fetch pending payments "+
						"at height #%d: %v", header.Height, err)
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

// processPayments fetches all eligible payments and publishes a
// transaction to the network paying dividends to participating accounts.
func (h *Hub) processPayments(height uint32) error {
	// Waiting two blocks after a successful payment before proceeding with
	// another one because the reserved amount for transaction fees becomes
	// change after a successful transaction. Change matures after the next
	// block is processed. The second block is as a result of trying to
	// maximize the transaction fee usage by processing mature payments
	// after the transaction fees reserve has matured and ready for another
	// transaction.
	lastPmtHeight := atomic.LoadUint32(&h.lastPaymentHeight)
	if lastPmtHeight != 0 && (height-lastPmtHeight) < 3 {
		return nil
	}

	// Fetch all eligible payments.
	eligiblePmts, err := FetchEligiblePaymentBundles(h.db, height,
		h.cfg.MinPayment)
	if err != nil {
		return err
	}

	if len(eligiblePmts) == 0 {
		log.Tracef("no eligible payments to process")
		return nil
	}

	log.Tracef("eligible payments are: %v", spew.Sdump(eligiblePmts))

	// Generate the payment details from the eligible payments fetched.
	details, targetAmt, err := generatePaymentDetails(h.db,
		h.cfg.PoolFeeAddrs, eligiblePmts, h.cfg.MaxTxFeeReserve, &h.txFeeReserve)
	if err != nil {
		return err
	}

	log.Tracef("mature rewards at height #%d is: %v", height, targetAmt)

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
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}

		err := pbkt.Put(lastPaymentPaidOn, nanoToBigEndianBytes(nowNano))
		if err != nil {
			return err
		}

		// Update and persist the last payment height.
		atomic.StoreUint32(&h.lastPaymentHeight, height)
		hbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(hbytes, height)
		err = pbkt.Put(lastPaymentHeight, hbytes)
		if err != nil {
			return err
		}

		// Persist the tx fee reserve.
		tbytes := make([]byte, 4)
		binary.LittleEndian.PutUint32(tbytes, uint32(h.txFeeReserve))
		err = pbkt.Put(txFeeReserve, tbytes)
		if err != nil {
			return err
		}

		return nil
	})
	return err
}

type ClientInfo struct {
	Miner    string
	IP       string
	HashRate *big.Rat
}

// FetchClientInfo returns connection details about all pool clients.
func (h *Hub) FetchClientInfo() map[string][]*ClientInfo {
	clientInfo := make(map[string][]*ClientInfo)

	// Iterate through all connected miners.
	for _, endpoint := range h.endpoints {
		endpoint.clientsMtx.Lock()
		for _, client := range endpoint.clients {
			client.hashRateMtx.RLock()
			clientInfo[client.account] = append(clientInfo[client.account],
				&ClientInfo{
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

// FetchAccountClientInfo returns all clients belonging to the provided
// account id.
func (h *Hub) FetchAccountClientInfo(accountID string) []*ClientInfo {
	info := make([]*ClientInfo, 0)

	// Filter through through all connected miners.
	for _, endpoint := range h.endpoints {
		endpoint.clientsMtx.Lock()
		for _, client := range endpoint.clients {
			if client.account == accountID {
				client.hashRateMtx.RLock()
				hash := client.hashRate
				client.hashRateMtx.RUnlock()

				info = append(info, &ClientInfo{
					Miner:    endpoint.miner,
					IP:       client.ip,
					HashRate: hash,
				})
			}
		}
		endpoint.clientsMtx.Unlock()
	}
	return info
}

// FetchLastWorkHeight returns  the last height work was generated for.
func (h *Hub) FetchLastWorkHeight() uint32 {
	return atomic.LoadUint32(&h.lastWorkHeight)
}

// FetchLastPaymentHeight returns the last height payments were made at.
func (h *Hub) FetchLastPaymentHeight() uint32 {
	if !h.cfg.SoloPool {
		return atomic.LoadUint32(&h.lastPaymentHeight)
	}

	return 0
}

// FetchMinedWork returns the last ten mined blocks by the pool.
func (h *Hub) FetchMinedWork() ([]*AcceptedWork, error) {
	return ListMinedWork(h.db, 10)
}

// FetchPoolHashRate returns the hash rate of the pool.
func (h *Hub) FetchPoolHashRate() (*big.Rat, map[string][]*ClientInfo) {
	clientInfo := h.FetchClientInfo()
	poolHashRate := new(big.Rat).SetInt64(0)
	for _, clients := range clientInfo {
		for _, miner := range clients {
			poolHashRate = poolHashRate.Add(poolHashRate, miner.HashRate)
		}
	}

	return poolHashRate, clientInfo
}

// Quota details the portion of mining rewrds due an account for work
// contributed to the pool.
type Quota struct {
	AccountID  string
	Percentage *big.Rat
}

// FetchWorkQuotas returns the reward distribution to pool accounts
// based on work contributed per the peyment scheme used by the pool.
func (h *Hub) FetchWorkQuotas() ([]*Quota, error) {
	if h.cfg.SoloPool {
		return nil, nil
	}

	height := atomic.LoadUint32(&h.lastWorkHeight)

	var percentages map[string]*big.Rat
	var err error
	if h.cfg.PaymentMethod == PPS {
		percentages, err =
			PPSSharePercentages(h.db, h.cfg.PoolFee, height)
	}

	if h.cfg.PaymentMethod == PPLNS {
		percentages, err =
			PPLNSSharePercentages(h.db, h.cfg.PoolFee, height, h.cfg.LastNPeriod)
	}

	if err != nil {
		return nil, err
	}

	quotas := make([]*Quota, 0)
	for key, value := range percentages {
		quotas = append(quotas, &Quota{
			AccountID:  key,
			Percentage: value,
		})
	}

	return quotas, nil
}

// FetchMinedWorkByAddress returns a list of mined work by the provided address.
// List is ordered, most recent comes first.
func (h *Hub) FetchMinedWorkByAddress(id string) ([]*AcceptedWork, error) {
	work, err := listMinedWorkByAccount(h.db, id, 10)
	return work, err
}

// FetchPaymentsForAddress returns a list or payments made to the provided address.
// List is ordered, most recent comes first.
func (h *Hub) FetchPaymentsForAddress(id string) ([]*Payment, error) {
	payments, err := fetchArchivedPaymentsForAccount(h.db, id, 10)
	return payments, err
}

// AccountExists asserts if the provided account id references a pool account.
func (h *Hub) AccountExists(accountID string) bool {
	_, err := FetchAccount(h.db, []byte(accountID))
	if err != nil {
		log.Tracef("Unable to fetch account for id: %s", accountID)
		return false
	}

	return true
}

// CSRFSecret fetches a persisted secret or generates a new one.
func (h *Hub) CSRFSecret(getCSRF func() ([]byte, error)) ([]byte, error) {
	var secret []byte
	err := h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(poolBkt)
		if pbkt == nil {
			desc := fmt.Sprintf("bucket %s not found", string(poolBkt))
			return MakeError(ErrBucketNotFound, desc, nil)
		}

		v := pbkt.Get(csrfSecret)
		if v != nil {
			secret = make([]byte, len(v))
			copy(secret, v)
			return nil
		}

		log.Info("CSRF secret value not found in db, initializing.")

		var err error
		secret, err = getCSRF()
		if err != nil {
			return err
		}

		err = pbkt.Put(csrfSecret, secret)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return secret, nil
}

// BackupDB streams a backup of the database over an http response.
func (h *Hub) BackupDB(w http.ResponseWriter) error {
	err := h.db.View(func(tx *bolt.Tx) error {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="backup.db"`)
		w.Header().Set("Content-Length", strconv.Itoa(int(tx.Size())))
		_, err := tx.WriteTo(w)
		return err
	})

	return err
}
