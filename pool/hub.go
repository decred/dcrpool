// Copyright (c) 2019 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bolt "github.com/coreos/bbolt"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v2"
	"github.com/decred/dcrd/dcrutil/v2"
	"github.com/decred/dcrd/rpcclient/v5"
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

	NewParent = "newparent"
	NewVotes  = "newvotes"
	NewTxns   = "newtxns"
)

var (
	// soloMaxGenTime is the threshold (in seconds) at which pool clients will
	// generate a valid share when solo pool mode is activated. This is set to a
	// high value to reduce the number of round trips to the pool by connected
	// pool clients since pool shares are a non factor in solo pool mode.
	soloMaxGenTime = time.Second * 28
)

// HubConfig represents configuration details for the hub.
type HubConfig struct {
	ActiveNet             *chaincfg.Params
	DB                    *bolt.DB
	DcrdRPCCfg            *rpcclient.ConnConfig
	PoolFee               float64
	MaxTxFeeReserve       dcrutil.Amount
	MaxGenTime            time.Duration
	WalletRPCCertFile     string
	WalletGRPCHost        string
	PaymentMethod         string
	LastNPeriod           time.Duration
	WalletPass            string
	MinPayment            dcrutil.Amount
	SoloPool              bool
	PoolFeeAddrs          []dcrutil.Address
	BackupPass            string
	Secret                string
	NonceIterations       float64
	MinerPorts            map[string]uint32
	MaxConnectionsPerHost uint32
}

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	clients int32 // update atomically.

	db             *bolt.DB
	cfg            *HubConfig
	limiter        *RateLimiter
	rpcc           *rpcclient.Client
	gConn          *grpc.ClientConn
	grpc           walletrpc.WalletServiceClient
	grpcMtx        sync.Mutex
	poolDiffs      *DifficultySet
	paymentMgr     *PaymentMgr
	chainState     *ChainState
	connections    map[string]uint32
	connectionsMtx sync.RWMutex
	cancel         context.CancelFunc
	endpoints      []*Endpoint
	blake256Pad    []byte
	wg             *sync.WaitGroup
}

// persistPoolMode saves the pool mode to the db.
func (h *Hub) persistPoolMode(tx *bolt.Tx, mode uint32) error {
	pbkt := tx.Bucket(poolBkt)
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, mode)
	return pbkt.Put(soloPool, b)
}

// generateBlake256Pad creates the extra padding needed for work
// submissions over the getwork RPC.
func generateBlake256Pad() []byte {
	blake256Pad := make([]byte, getworkDataLen-wire.MaxBlockHeaderPayload)
	blake256Pad[0] = 0x80
	blake256Pad[len(blake256Pad)-9] |= 0x01
	binary.BigEndian.PutUint64(blake256Pad[len(blake256Pad)-8:],
		wire.MaxBlockHeaderPayload*8)
	return blake256Pad
}

// NewHub initializes the mining pool hub.
func NewHub(cancel context.CancelFunc, hcfg *HubConfig) (*Hub, error) {
	h := &Hub{
		cfg:         hcfg,
		db:          hcfg.DB,
		limiter:     NewRateLimiter(),
		wg:          new(sync.WaitGroup),
		connections: make(map[string]uint32),
		cancel:      cancel,
	}
	h.blake256Pad = generateBlake256Pad()
	powLimit := new(big.Rat).SetInt(h.cfg.ActiveNet.PowLimit)
	maxGenTime := h.cfg.MaxGenTime
	if h.cfg.SoloPool {
		maxGenTime = soloMaxGenTime
	}

	log.Infof("Maximum work submission generation time at "+
		"pool difficulty is %s.", maxGenTime)

	var err error
	h.poolDiffs, err = NewDifficultySet(h.cfg.ActiveNet, powLimit, maxGenTime)
	if err != nil {
		return nil, err
	}

	pCfg := &PaymentMgrConfig{
		DB:                 h.db,
		ActiveNet:          h.cfg.ActiveNet,
		PoolFee:            h.cfg.PoolFee,
		LastNPeriod:        h.cfg.LastNPeriod,
		SoloPool:           h.cfg.SoloPool,
		PaymentMethod:      h.cfg.PaymentMethod,
		MinPayment:         h.cfg.MinPayment,
		PoolFeeAddrs:       h.cfg.PoolFeeAddrs,
		MaxTxFeeReserve:    h.cfg.MaxTxFeeReserve,
		PublishTransaction: h.PublishTransaction,
	}
	h.paymentMgr, err = NewPaymentMgr(pCfg)
	if err != nil {
		return nil, err
	}

	sCfg := &ChainStateConfig{
		DB:               h.db,
		SoloPool:         h.cfg.SoloPool,
		PayDividends:     h.paymentMgr.payDividends,
		GeneratePayments: h.paymentMgr.generatePayments,
		GetBlock:         h.getBlock,
		Cancel:           h.cancel,
		HubWg:            h.wg,
	}
	h.chainState = NewChainState(sCfg)

	if !h.cfg.SoloPool {
		log.Infof("Payment method is %s.", strings.ToUpper(hcfg.PaymentMethod))
	} else {
		log.Infof("Solo pool mode active.")
	}

	err = h.db.Update(func(tx *bolt.Tx) error {
		mode := uint32(0)
		if h.cfg.SoloPool {
			mode = 1
		}
		return h.persistPoolMode(tx, mode)
	})
	if err != nil {
		return nil, err
	}
	return h, nil
}

// submitWork sends solved block data to the consensus daemon for evaluation.
func (h *Hub) submitWork(data *string) (bool, error) {
	status, err := h.rpcc.GetWorkSubmit(*data)
	if err != nil {
		return false, err
	}
	return status, err
}

// getWork fetches available work from the consensus daemon.
func (h *Hub) getWork() (string, string, error) {
	work, err := h.rpcc.GetWork()
	if err != nil {
		return "", "", err
	}
	return work.Data, work.Target, err
}

// WithinLimit returns if a client is within its request limits.
func (h *Hub) WithinLimit(ip string, clientType int) bool {
	return h.limiter.withinLimit(ip, clientType)
}

// FetchLastWorkHeight returns the last work height of the pool.
func (h *Hub) FetchLastWorkHeight() uint32 {
	return h.chainState.fetchLastWorkHeight()
}

// FetchLastPaymentheight returns the last payment height of the pool.
func (h *Hub) FetchLastPaymentHeight() uint32 {
	return h.paymentMgr.fetchLastPaymentHeight()
}

// AddPaymentRequest creates a payment request for the provided address.
func (h *Hub) AddPaymentRequest(addr string) error {
	return h.paymentMgr.addPaymentRequest(addr)
}

// getBlock fetches the blocks associated with the provided block hash.
func (h *Hub) getBlock(blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	block, err := h.rpcc.GetBlock(blockHash)
	if err != nil {
		return nil, err
	}
	return block, err
}

// fetchHostConnections returns the client connection count for the
// provided host.
func (h *Hub) fetchHostConnections(host string) uint32 {
	h.connectionsMtx.RLock()
	defer h.connectionsMtx.RUnlock()
	return h.connections[host]
}

// addConnection records a new client connection for the provided host.
func (h *Hub) addConnection(host string) {
	h.connectionsMtx.Lock()
	h.connections[host]++
	h.connectionsMtx.Unlock()
	atomic.AddInt32(&h.clients, 1)
}

// removeConnection removes a client connection for the provided host.
func (h *Hub) removeConnection(host string) {
	h.connectionsMtx.Lock()
	h.connections[host]--
	h.connectionsMtx.Unlock()
	atomic.AddInt32(&h.clients, -1)
}

// processWork parses work received and dispatches a work notification to all
// connected pool clients.
func (h *Hub) processWork(headerE string) {
	heightD, err := hex.DecodeString(headerE[256:264])
	if err != nil {
		log.Errorf("failed to decode block height %s: %v", string(heightD), err)
		return
	}
	height := binary.LittleEndian.Uint32(heightD)
	log.Tracef("New work at height #%d received: %s", height, headerE)
	h.chainState.setLastWorkHeight(height)
	if !h.HasClients() {
		return
	}

	blockVersion := headerE[:8]
	prevBlock := headerE[8:72]
	genTx1 := headerE[72:288]
	nBits := headerE[232:240]
	nTime := headerE[272:280]
	genTx2 := headerE[352:360]
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

// Listen creates listeners for all supported pool clients.
func (h *Hub) Listen() error {
	for miner, port := range h.cfg.MinerPorts {
		diffInfo, err := h.poolDiffs.fetchMinerDifficulty(miner)
		if err != nil {
			return err
		}
		eCfg := &EndpointConfig{
			ActiveNet:             h.cfg.ActiveNet,
			DB:                    h.db,
			SoloPool:              h.cfg.SoloPool,
			Blake256Pad:           h.blake256Pad,
			NonceIterations:       h.cfg.NonceIterations,
			MaxConnectionsPerHost: h.cfg.MaxConnectionsPerHost,
			HubWg:                 h.wg,
			SubmitWork:            h.submitWork,
			FetchCurrentWork:      h.chainState.fetchCurrentWork,
			WithinLimit:           h.limiter.withinLimit,
			AddConnection:         h.addConnection,
			RemoveConnection:      h.removeConnection,
			FetchHostConnections:  h.fetchHostConnections,
			MaxGenTime:            h.cfg.MaxGenTime,
		}
		endpoint, err := NewEndpoint(eCfg, diffInfo, port, miner)
		if err != nil {
			desc := fmt.Sprintf("unable to create %s listener", miner)
			return MakeError(ErrOther, desc, err)
		}
		h.endpoints = append(h.endpoints, endpoint)
	}
	return nil
}

// CloseListeners terminates listeners created by endpoints of the hub. This
// should only be used in the pool's shutdown process the hub is not running.
func (h *Hub) CloseListeners() {
	for _, e := range h.endpoints {
		e.listener.Close()
	}
}

// Connect establishes connections with the consensus daemon and the wallet.
func (h *Hub) Connect() error {
	// Create handlers for chain notifications being subscribed for.
	ntfnHandlers := &rpcclient.NotificationHandlers{
		OnBlockConnected: func(headerB []byte, transactions [][]byte) {
			h.chainState.connCh <- &blockNotification{
				Header: headerB,
				Done:   make(chan bool),
			}
		},
		OnBlockDisconnected: func(headerB []byte) {
			h.chainState.discCh <- &blockNotification{
				Header: headerB,
				Done:   make(chan bool),
			}
		},
		OnWork: func(headerB []byte, target []byte, reason string) {
			currWork := hex.EncodeToString(headerB)
			switch reason {
			case NewTxns:
				h.chainState.setCurrentWork(currWork)

			case NewParent, NewVotes:
				h.chainState.setCurrentWork(currWork)
				h.processWork(currWork)
			}
		},
	}

	// Establish RPC connection with dcrd.
	rpcc, err := rpcclient.New(h.cfg.DcrdRPCCfg, ntfnHandlers)
	if err != nil {
		desc := "dcrd rpc error"
		return MakeError(ErrOther, desc, err)
	}

	h.rpcc = rpcc
	if err := h.rpcc.NotifyWork(); err != nil {
		h.rpcc.Shutdown()
		desc := "notify work rpc error (dcrd)"
		return MakeError(ErrOther, desc, err)
	}
	if err := h.rpcc.NotifyBlocks(); err != nil {
		h.rpcc.Shutdown()
		desc := "notify blocks error (dcrd)"
		return MakeError(ErrOther, desc, err)
	}

	// Establish GRPC connection with the wallet if not in solo pool mode.
	if !h.cfg.SoloPool {
		creds, err := credentials.NewClientTLSFromFile(h.cfg.WalletRPCCertFile,
			"localhost")
		if err != nil {
			desc := "grpc tls error (dcrwallet)"
			return MakeError(ErrOther, desc, err)
		}
		h.gConn, err = grpc.Dial(h.cfg.WalletGRPCHost,
			grpc.WithTransportCredentials(creds))
		if err != nil {
			desc := "grpc dial error (dcrwallet)"
			return MakeError(ErrOther, desc, err)
		}
		if h.gConn == nil {
			desc := "unable to establish grpc with dcrwallet"
			return MakeError(ErrOther, desc, nil)
		}
		h.grpc = walletrpc.NewWalletServiceClient(h.gConn)
		req := &walletrpc.BalanceRequest{
			RequiredConfirmations: 1,
		}
		h.grpcMtx.Lock()
		_, err = h.grpc.Balance(context.TODO(), req)
		h.grpcMtx.Unlock()
		if err != nil {
			desc := "grpc request error (dcrwallet)"
			return MakeError(ErrOther, desc, err)
		}
	}
	work, _, err := h.getWork()
	if err != nil {
		desc := "unable to fetch current work"
		return MakeError(ErrOther, desc, err)
	}
	h.chainState.setCurrentWork(work)
	return nil
}

// HasClients asserts the mining pool has clients.
func (h *Hub) HasClients() bool {
	return atomic.LoadInt32(&h.clients) > 0
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
	return txid.String(), nil
}

// shutdown tears down the hub and releases resources used.
func (h *Hub) shutdown() {
	if !h.cfg.SoloPool {
		if h.gConn != nil {
			h.gConn.Close()
		}
	}
	if h.rpcc != nil {
		h.rpcc.Shutdown()
	}
	h.db.Close()
}

// run handles the process lifecycles of the pool hub.
func (h *Hub) Run(ctx context.Context) {
	for _, e := range h.endpoints {
		go e.run(ctx)
		h.wg.Add(1)
	}
	go h.chainState.handleChainUpdates(ctx)
	h.wg.Add(1)

	h.wg.Wait()
	h.shutdown()
}

// ClientInfo represents client miner information.
type ClientInfo struct {
	Miner    string
	IP       string
	HashRate *big.Rat
}

// FetchClientInfo returns connection details about all pool clients.
func (h *Hub) FetchClientInfo() map[string][]*ClientInfo {
	clientInfo := make(map[string][]*ClientInfo)
	for _, endpoint := range h.endpoints {
		endpoint.clientsMtx.Lock()
		for _, client := range endpoint.clients {
			hash := client.fetchHashRate()
			clientInfo[client.account] = append(clientInfo[client.account],
				&ClientInfo{
					Miner:    endpoint.miner,
					IP:       client.addr.String(),
					HashRate: hash,
				})
		}
		endpoint.clientsMtx.Unlock()
	}
	return clientInfo
}

// FetchAccountClientInfo returns all clients belonging to the provided
// account id.
func (h *Hub) FetchAccountClientInfo(accountID string) []*ClientInfo {
	info := make([]*ClientInfo, 0)
	for _, endpoint := range h.endpoints {
		endpoint.clientsMtx.Lock()
		for _, client := range endpoint.clients {
			if client.account == accountID {
				client.hashRateMtx.RLock()
				hash := client.hashRate
				client.hashRateMtx.RUnlock()
				info = append(info, &ClientInfo{
					Miner:    endpoint.miner,
					IP:       client.addr.String(),
					HashRate: hash,
				})
			}
		}
		endpoint.clientsMtx.Unlock()
	}
	return info
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
// based on work contributed per the payment scheme used by the pool.
func (h *Hub) FetchWorkQuotas() ([]*Quota, error) {
	if h.cfg.SoloPool {
		return nil, nil
	}
	var percentages map[string]*big.Rat
	var err error
	if h.cfg.PaymentMethod == PPS {
		percentages, err = h.paymentMgr.PPSSharePercentages()
	}
	if h.cfg.PaymentMethod == PPLNS {
		percentages, err = h.paymentMgr.PPLNSSharePercentages()
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

// FetchMinedWorkByAccount returns a list of mined work by the provided address.
// List is ordered, most recent comes first.
func (h *Hub) FetchMinedWorkByAccount(id string) ([]*AcceptedWork, error) {
	work, err := listMinedWorkByAccount(h.db, id, 10)
	return work, err
}

// FetchPaymentsForAccount returns a list or payments made to the provided address.
// List is ordered, most recent comes first.
func (h *Hub) FetchPaymentsForAccount(id string) ([]*Payment, error) {
	payments, err := fetchPaymentsForAccount(h.db, id, 10)
	return payments, err
}

// AccountExists checks if the provided account id references a pool account.
func (h *Hub) AccountExists(accountID string) bool {
	_, err := FetchAccount(h.db, []byte(accountID))
	if err != nil {
		log.Tracef("Unable to fetch account for id: %s", accountID)
		return false
	}
	return true
}

// CSRFSecret fetches a persisted secret or generates a new one.
func (h *Hub) CSRFSecret() ([]byte, error) {
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

		var err error
		secret = make([]byte, 32)
		_, err = rand.Read(secret)
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
