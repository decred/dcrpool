// Copyright (c) 2018 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package network

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/chaincfg/chainhash"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg"
	"github.com/decred/dcrd/dcrutil"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrd/wire"
	"github.com/decred/dcrwallet/rpc/walletrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/dnldd/dcrpool/database"
	"github.com/dnldd/dcrpool/dividend"
)

const (
	// MaxReorgLimit is an estimated maximum chain reorganization limit.
	// That is, it is highly improbable for the the chain to reorg beyond six
	// blocks from the chain tip.
	MaxReorgLimit = 6

	// uint256Size is the number of bytes needed to represent an unsigned
	// 256-bit integer.
	uint256Size = 32

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
	// Convenience variable
	zeroInt = new(big.Int).SetInt64(0)

	// soloMaxGenTime is the threshold (in seconds) at which pool clients will
	// generate a valid share when solo pool mode is activated. This is set to a
	// high value to reduce the number of round trips to the pool by connected
	// pool clients since pool shares are a non factor in solo pool mode.
	soloMaxGenTime = new(big.Int).SetInt64(25)

	// MinedBlocks is the mined block count key.
	MinedBlocks = []byte("minedblocks")
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
	// atomic variables first for alignment
	minedBlocks uint32

	db                *bolt.DB
	httpc             *http.Client
	cfg               *HubConfig
	limiter           *RateLimiter
	Broadcast         chan Message
	rpcc              *rpcclient.Client
	rpccMtx           sync.Mutex
	gConn             *grpc.ClientConn
	grpc              walletrpc.WalletServiceClient
	grpcMtx           sync.Mutex
	hashRate          *big.Int
	hashRateMtx       sync.Mutex
	poolDiff          map[string]*DifficultyData
	poolDiffMtx       sync.RWMutex
	clients           uint32
	clientsMtx        sync.RWMutex
	connCh            chan []byte
	discCh            chan []byte
	ctx               context.Context
	cancel            context.CancelFunc
	txFeeReserve      dcrutil.Amount
	lastWorkHeight    uint32
	lastPaymentHeight uint32
	endpoints         []*Endpoint
	blake256Pad       []byte
}

// CreateListeners sets up endpoints and listeners for each known pool client.
func (h *Hub) CreateListeners() error {
	for miner, port := range dividend.MinerPorts {
		endpoint, err := NewEndpoint(h, port, miner)
		if err != nil {
			return err
		}

		go endpoint.Listen()
		h.endpoints = append(h.endpoints, endpoint)
	}

	return nil
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
	log.Tracef("New Work (header: %v , target: %v)", headerE,
		target)

	heightD, err := hex.DecodeString(headerE[256:264])
	if err != nil {
		log.Errorf("Failed to decode block height: %v", err)
		return
	}

	height := binary.LittleEndian.Uint32(heightD)
	h.lastWorkHeight = height

	// Do not process work data id there are no connected  pool clients.
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
	h.Broadcast <- workNotif

	log.Tracef("Broadcasting work to pool clients")
}

// NewHub initializes a websocket hub.
func NewHub(ctx context.Context, cancel context.CancelFunc, db *bolt.DB, httpc *http.Client, hcfg *HubConfig, limiter *RateLimiter) (*Hub, error) {
	h := &Hub{
		db:        db,
		httpc:     httpc,
		limiter:   limiter,
		cfg:       hcfg,
		hashRate:  zeroInt,
		poolDiff:  make(map[string]*DifficultyData),
		Broadcast: make(chan Message),
		clients:   0,
		connCh:    make(chan []byte),
		discCh:    make(chan []byte),
		ctx:       ctx,
		cancel:    cancel,
	}

	h.GenerateBlake256Pad()

	if !h.cfg.SoloPool {
		log.Infof("Payment method is %v.", hcfg.PaymentMethod)
	} else {
		log.Infof("Solo pool enabled")
	}

	// Load the tx fee reserve, last payment height and mined blocks count.
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

		v = pbkt.Get(dividend.LastPaymentHeight)
		if v == nil {
			log.Info("Last payment height value not found in db, initializing.")
			h.lastPaymentHeight = 0
		}

		if v != nil {
			h.lastPaymentHeight = binary.LittleEndian.Uint32(v)
		}

		v = pbkt.Get(MinedBlocks)
		if v == nil {
			log.Info("Mined blocks value not found in db, initializing.")
			h.minedBlocks = 0
		}

		if v != nil {
			h.minedBlocks = binary.LittleEndian.Uint32(v)
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

	log.Tracef("Last payment height is currently: %v", h.lastPaymentHeight)
	log.Tracef("Mined blocks counter is currently at: %v blocks", h.minedBlocks)

	// Generate difficulty data for all known pool clients.
	err = h.GenerateDifficultyData()
	if err != nil {
		log.Error("Failed to generate difficulty data: %v", err)
		return nil, err
	}

	// Setup listeners for all known pool clients.
	err = h.CreateListeners()
	if err != nil {
		log.Error("Failed to create listeners: %v", err)
		return nil, err
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

	log.Debugf("RPC connection established with dcrd.")

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
	}

	go h.handleGetWork()
	go h.handleChainUpdates()

	return h, nil
}

// GenerateExtraNonce1 generates a random 4-byte extraNonce1 value.
func GenerateExtraNonce1() string {
	id := make([]byte, 4)
	rand.Read(id)
	return hex.EncodeToString(id)
}

// AddClient records a connected client and the hash rate it contributes to
// to the pool.
func (h *Hub) AddClient(miner string) {
	// Increment the client count.
	h.clientsMtx.Lock()
	h.clients++
	h.clientsMtx.Unlock()

	// Add the client's hash rate.
	hash := dividend.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Add(h.hashRate, hash)
	log.Infof("Client connected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// RemoveClient removes a disconnected client and the hash rate it contributed to
// the pool.
func (h *Hub) RemoveClient(miner string) {
	// Decrement the client count.
	h.clientsMtx.Lock()
	h.clients--
	h.clientsMtx.Unlock()

	// Remove the client's hash rate
	hash := dividend.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Sub(h.hashRate, hash)
	log.Infof("Client disconnected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// HasClients asserts the mining pool has clients.
func (h *Hub) HasClients() bool {
	h.clientsMtx.RLock()
	hasClients := h.clients > 0
	h.clientsMtx.RUnlock()
	return hasClients
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

// Persist saves details of the hub to the database.
func (h *Hub) Persist() error {
	err := h.db.Update(func(tx *bolt.Tx) error {
		pbkt := tx.Bucket(database.PoolBkt)
		vbytes := make([]byte, 4)

		// Persist the tx fee reserve.
		binary.LittleEndian.PutUint32(vbytes, uint32(h.txFeeReserve))
		err := pbkt.Put(dividend.TxFeeReserve, vbytes)
		if err != nil {
			return err
		}

		// Persist the last payment height.
		binary.LittleEndian.PutUint32(vbytes, h.lastPaymentHeight)
		err = pbkt.Put(dividend.LastPaymentHeight, vbytes)
		if err != nil {
			return err
		}

		// Persist the mined blocks count.
		binary.LittleEndian.PutUint32(vbytes, h.minedBlocks)
		return pbkt.Put(MinedBlocks, vbytes)
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
		RequiredConfirmations:    1,
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

// handleGetWork periodically fetches available work from the consensus daemon.
func (h *Hub) handleGetWork() {
	var currHeaderE string
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()
	log.Info("Started getwork handler")

	for {
		select {
		case <-h.ctx.Done():
			log.Info("GetWork handler done.")
			return
		case <-ticker.C:
			headerE, target, err := h.GetWork()
			if err != nil {
				log.Errorf("Failed to fetch work: %v", err)
				continue
			}

			// Process incoming work if there is no current work.
			if currHeaderE == "" {
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
			if newNTime.Sub(currNTime) > time.Second*30 {
				currHeaderE = headerE
				h.processWork(currHeaderE, target)
				continue
			}
		}
	}
}

// handleChainUpdates processes connected and disconnected block notifications
// from the consensus daemon.
func (h *Hub) handleChainUpdates() {
	log.Info("Started chain updates handler.")

	for {
		select {
		case <-h.ctx.Done():
			if !h.cfg.SoloPool {
				h.gConn.Close()
			}

			h.rpccMtx.Lock()
			h.rpcc.Shutdown()
			h.rpccMtx.Unlock()

			close(h.discCh)
			close(h.connCh)
			close(h.Broadcast)

			log.Info("Chain updates handler done.")
			return

		case headerB := <-h.connCh:
			var header wire.BlockHeader
			err := header.FromBytes(headerB)
			if err != nil {
				log.Errorf("Failed to create header from bytes: %v", err)
				h.cancel()
				continue
			}
			log.Tracef("Block connected at height %v", header.Height)

			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err := PruneJobs(h.db, pruneLimit)
				if err != nil {
					log.Errorf("Failed to prune jobs to height %d: %v",
						pruneLimit, err)
					h.cancel()
					continue
				}

				log.Tracef("Pruned jobs below height: %v", pruneLimit)
			}

			blockHash := header.BlockHash()
			id := AcceptedWorkID(blockHash.String(), header.Height)
			work, err := FetchAcceptedWork(h.db, id)
			if err != nil {
				log.Errorf("Failed to fetch accepted work: %v", err)
				continue
			}

			prevWork, err := work.FilterParentAcceptedWork(h.db)
			if err != nil {
				log.Errorf("Failed to filter parent accepted work: %v", err)
				continue
			}

			// Prune accepted work that is below the estimated reorg limit.
			if header.Height > MaxReorgLimit {
				pruneLimit := header.Height - MaxReorgLimit
				err = PruneAcceptedWork(h.db, pruneLimit)
				if err != nil {
					log.Errorf("Failed to prune accepted work below height (%v)"+
						": %v", pruneLimit, err)
					h.cancel()
					continue
				}

				log.Tracef("Pruned accepted work below height: %v", pruneLimit)
			}

			if prevWork == nil {
				log.Tracef("No mined previous work found")
				continue
			}

			log.Tracef("Found mined previous work: %v", prevWork.BlockHash)

			// Persist the mined work and increment the mined blocks counter.
			err = prevWork.PersistMinedWork(h.db)
			if err != nil {
				log.Errorf("Failed to persist mined workd: %v", err)
			}

			atomic.AddUint32(&h.minedBlocks, 1)
			minedCount := atomic.LoadUint32(&h.minedBlocks)
			log.Tracef("Connected to mined block (%v),"+
				" current mined blocks count is (%v)",
				header.BlockHash(), minedCount)

			// Only process shares and payments when not mining in solo
			// pool mode.
			if !h.cfg.SoloPool {
				h.rpccMtx.Lock()
				block, err := h.rpcc.GetBlock(&blockHash)
				h.rpccMtx.Unlock()
				if err != nil {
					log.Errorf("Failed to fetch block: %v", err)
					h.cancel()
					continue
				}

				coinbase :=
					dcrutil.Amount(block.Transactions[0].TxOut[2].Value)

				log.Tracef("Accepted work (%v) at height %v has coinbase"+
					" of %v", header.BlockHash(), header.Height, coinbase)

				// Pay dividends per the configured payment scheme and process
				// mature payments.
				switch h.cfg.PaymentMethod {
				case dividend.PPS:
					err := dividend.PayPerShare(h.db, coinbase, h.cfg.PoolFee,
						header.Height, h.cfg.ActiveNet.CoinbaseMaturity)
					if err != nil {
						log.Error("Failed to process generate PPS shares: %v", err)
						h.cancel()
						continue
					}

				case dividend.PPLNS:
					err := dividend.PayPerLastNShares(h.db, coinbase,
						h.cfg.PoolFee, header.Height,
						h.cfg.ActiveNet.CoinbaseMaturity, h.cfg.LastNPeriod)
					if err != nil {
						log.Error("Failed to generate PPLNS shares: %v", err)
						h.cancel()
						continue
					}
				}

				// Process mature payments.
				err = h.ProcessPayments(header.Height)
				if err != nil {
					log.Errorf("Failed to process payments: %v", err)
					h.cancel()
				}
			}

		case headerB := <-h.discCh:
			var header wire.BlockHeader
			err := header.FromBytes(headerB)
			if err != nil {
				log.Errorf("Failed to create header from bytes: %v", err)
				h.cancel()
				continue
			}
			log.Tracef("Block disconnected at height %v", header.Height)

			// Delete mined work if it is disconnected from the chain.
			id := AcceptedWorkID(header.BlockHash().String(), header.Height)
			work, err := FetchMinedWork(h.db, id)
			if err != nil {
				log.Errorf("Failed to fetch mined work: %v", err)
				continue
			}

			err = work.DeleteMinedWork(h.db)
			if err != nil {
				log.Errorf("Failed to delete mined work: %v", err)
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
						log.Errorf("Failed to delete payment", err)
						h.cancel()
						break
					}
				}
			}
		}
	}
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
	if h.lastPaymentHeight != 0 && (height-h.lastPaymentHeight) < 3 {
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
	err = h.PublishTransaction(pmts, *targetAmt)
	if err != nil {
		return err
	}

	// Update all payments published by the tx as paid and archive them.
	for _, bundle := range eligiblePmts {
		bundle.UpdateAsPaid(h.db, height)
		err = bundle.ArchivePayments(h.db)
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

	h.lastPaymentHeight = height

	return nil
}

// BigToLEUint256 returns the passed big integer as an unsigned 256-bit integer
// encoded as little-endian bytes.  Numbers which are larger than the max
// unsigned 256-bit integer are truncated.
func BigToLEUint256(n *big.Int) [uint256Size]byte {
	// Pad or truncate the big-endian big int to correct number of bytes.
	nBytes := n.Bytes()
	nlen := len(nBytes)
	pad := 0
	start := 0
	if nlen <= uint256Size {
		pad = uint256Size - nlen
	} else {
		start = nlen - uint256Size
	}
	var buf [uint256Size]byte
	copy(buf[pad:], nBytes[start:])

	// Reverse the bytes to little endian and return them.
	for i := 0; i < uint256Size/2; i++ {
		buf[i], buf[uint256Size-1-i] = buf[uint256Size-1-i], buf[i]
	}
	return buf
}

// LEUint256ToBig returns the passed unsigned 256-bit integer
// encoded as little-endian as a big integer.
func LEUint256ToBig(n [uint256Size]byte) *big.Int {
	var buf [uint256Size]byte
	copy(buf[:], n[:])

	// Reverse the bytes to big endian and create a big.Int.
	for i := 0; i < uint256Size/2; i++ {
		buf[i], buf[uint256Size-1-i] = buf[uint256Size-1-i], buf[i]
	}

	v := new(big.Int).SetBytes(buf[:])

	return v
}
