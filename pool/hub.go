// Copyright (c) 2019-2021 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package pool

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"decred.org/dcrwallet/rpc/walletrpc"
	"github.com/decred/dcrd/chaincfg/chainhash"
	"github.com/decred/dcrd/chaincfg/v3"
	"github.com/decred/dcrd/dcrutil/v3"
	chainjson "github.com/decred/dcrd/rpc/jsonrpc/types/v2"
	"github.com/decred/dcrd/rpcclient/v6"
	"github.com/decred/dcrd/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	errs "github.com/decred/dcrpool/errors"
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

	// NewParent is the reason given when a work notification is generated
	// because there is a new chain tip.
	NewParent = "newparent"
	// NewVotes is the reason given when a work notification is generated
	// because new votes were received.
	NewVotes = "newvotes"
	// NewTxns is the reason given when a work notification is generated
	// because new transactions were received.
	NewTxns = "newtxns"
)

// CacheUpdateEvent represents the a cache update event message.
type CacheUpdateEvent int

// Constants for the type of template regeneration event messages.
const (
	// Confirmed indicates an accepted work has been updated as
	// confirmed mined.
	Confirmed CacheUpdateEvent = iota

	// Unconfirmed indicates a previously confimed mined work has been
	// updated to unconfirmed due to a reorg.
	Unconfirmed

	// ClaimedShare indicates work quotas for participating clients have
	// been updated.
	ClaimedShare

	// DividendsPaid indicates dividends due participating miners have been
	// paid.
	DividendsPaid
)

var (
	// soloMaxGenTime is the threshold (in seconds) at which pool clients will
	// generate a valid share when solo pool mode is activated. This is set to a
	// high value to reduce the number of round trips to the pool by connected
	// pool clients since pool shares are a non factor in solo pool mode.
	soloMaxGenTime = time.Second * 28
)

// WalletConnection defines the functionality needed by a wallet
// grpc connection for the pool.
type WalletConnection interface {
	SignTransaction(context.Context, *walletrpc.SignTransactionRequest, ...grpc.CallOption) (*walletrpc.SignTransactionResponse, error)
	PublishTransaction(context.Context, *walletrpc.PublishTransactionRequest, ...grpc.CallOption) (*walletrpc.PublishTransactionResponse, error)
	Rescan(ctx context.Context, in *walletrpc.RescanRequest, opts ...grpc.CallOption) (walletrpc.WalletService_RescanClient, error)
}

// NodeConnection defines the functionality needed by a mining node
// connection for the pool.
type NodeConnection interface {
	GetTxOut(context.Context, *chainhash.Hash, uint32, bool) (*chainjson.GetTxOutResult, error)
	CreateRawTransaction(context.Context, []chainjson.TransactionInput, map[dcrutil.Address]dcrutil.Amount, *int64, *int64) (*wire.MsgTx, error)
	GetWorkSubmit(context.Context, string) (bool, error)
	GetWork(context.Context) (*chainjson.GetWorkResult, error)
	GetBlockVerbose(context.Context, *chainhash.Hash, bool) (*chainjson.GetBlockVerboseResult, error)
	GetBlock(context.Context, *chainhash.Hash) (*wire.MsgBlock, error)
	NotifyWork(context.Context) error
	NotifyBlocks(context.Context) error
	Shutdown()
}

// HubConfig represents configuration details for the hub.
type HubConfig struct {
	// ActiveNet represents the active network being mined on.
	ActiveNet *chaincfg.Params
	// DB represents the pool database.
	DB Database
	// NodeRPCConfig represents the mining node's RPC configuration details.
	NodeRPCConfig *rpcclient.ConnConfig
	// WalletRPCCert represents the wallet's RPC certificate.
	WalletRPCCert string
	// WalletTLSCert represents the wallet client's TLS certificate.
	WalletTLSCert string
	// WalletTLSKey represents the wallet client's TLS key file.
	WalletTLSKey string
	// WalletGRPCHost represents the ip:port establish a GRPC connection for
	// the wallet.
	WalletGRPCHost string
	// PoolFee represents the fee charged to participating accounts of the pool.
	PoolFee float64
	// MaxGenTime represents the share creation target time for the pool.
	MaxGenTime time.Duration
	// PaymentMethod represents the payment scheme of the pool.
	PaymentMethod string
	// LastNPeriod represents the period to source shares from when using the
	// PPLNS payment scheme.
	LastNPeriod time.Duration
	// WalletPass represents the passphrase to unlock the wallet with.
	WalletPass string
	// SoloPool represents the solo pool mining mode.
	SoloPool bool
	// PoolFeeAddrs represents the pool fee addresses of the pool.
	PoolFeeAddrs []dcrutil.Address
	// AdminPass represents the admin password.
	AdminPass string
	// NonceIterations returns the possible header nonce iterations.
	NonceIterations float64
	// MinerListen represents the listening address for miner connections.
	MinerListen string
	// MaxConnectionsPerHost represents the maximum number of connections
	// allowed per host.
	MaxConnectionsPerHost uint32
	// WalletAccount represents the wallet account to process payments from.
	WalletAccount uint32
	// CoinbaseConfTimeout is the duration to wait for coinbase confirmations
	// when generating a payout transaction.
	CoinbaseConfTimeout time.Duration
	// MonitorCycle represents the time monitoring a mining client to access
	// possible upgrades if needed.
	MonitorCycle time.Duration
	// MaxUpgradeTries represents the maximum number of consecutive miner
	// monitoring and upgrade tries.
	MaxUpgradeTries uint32
	// ClientTimeout represents the read/write timeout for the client.
	ClientTimeout time.Duration
}

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	clients int32 // update atomically.

	cfg            *HubConfig
	limiter        *RateLimiter
	nodeConn       NodeConnection
	walletClose    func() error
	walletConn     WalletConnection
	notifClient    walletrpc.WalletService_ConfirmationNotificationsClient
	poolDiffs      *DifficultySet
	paymentMgr     *PaymentMgr
	chainState     *ChainState
	connections    map[string]uint32
	connectionsMtx sync.RWMutex
	cancel         context.CancelFunc
	endpoint       *Endpoint
	blake256Pad    []byte
	wg             *sync.WaitGroup
	cacheCh        chan CacheUpdateEvent
}

// SignalCache sends the provided cache update event to the gui cache.
func (h *Hub) SignalCache(event CacheUpdateEvent) {
	select {
	case h.cacheCh <- event:
	default:
		// Non-breaking send fallthrough.
	}
}

// FetchCacheChannel returns the gui cache signal chanel.
func (h *Hub) FetchCacheChannel() chan CacheUpdateEvent {
	return h.cacheCh
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
		limiter:     NewRateLimiter(),
		wg:          new(sync.WaitGroup),
		connections: make(map[string]uint32),
		cacheCh:     make(chan CacheUpdateEvent, bufferSize),
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

	h.poolDiffs = NewDifficultySet(h.cfg.ActiveNet, powLimit, maxGenTime)

	pCfg := &PaymentMgrConfig{
		db:                     h.cfg.DB,
		ActiveNet:              h.cfg.ActiveNet,
		PoolFee:                h.cfg.PoolFee,
		LastNPeriod:            h.cfg.LastNPeriod,
		SoloPool:               h.cfg.SoloPool,
		PaymentMethod:          h.cfg.PaymentMethod,
		PoolFeeAddrs:           h.cfg.PoolFeeAddrs,
		WalletAccount:          h.cfg.WalletAccount,
		WalletPass:             h.cfg.WalletPass,
		GetBlockConfirmations:  h.getBlockConfirmations,
		GetTxConfNotifications: h.getTxConfNotifications,
		FetchTxCreator:         func() TxCreator { return h.nodeConn },
		FetchTxBroadcaster:     func() TxBroadcaster { return h.walletConn },
		CoinbaseConfTimeout:    h.cfg.CoinbaseConfTimeout,
	}

	var err error
	h.paymentMgr, err = NewPaymentMgr(pCfg)
	if err != nil {
		return nil, err
	}

	sCfg := &ChainStateConfig{
		db:                    h.cfg.DB,
		SoloPool:              h.cfg.SoloPool,
		PayDividends:          h.paymentMgr.payDividends,
		GeneratePayments:      h.paymentMgr.generatePayments,
		GetBlock:              h.getBlock,
		GetBlockConfirmations: h.getBlockConfirmations,
		Cancel:                h.cancel,
		SignalCache:           h.SignalCache,
		HubWg:                 h.wg,
	}
	h.chainState = NewChainState(sCfg)

	// Ensure database is in the correct mode.
	cfgMode := uint32(0)
	if h.cfg.SoloPool {
		cfgMode = 1
	}

	dbMode, err := hcfg.DB.fetchPoolMode()
	if err != nil {
		// If pool mode is not set, assume the database is new and persist the
		// pool mode.
		if errors.Is(err, errs.ValueNotFound) {
			err = hcfg.DB.persistPoolMode(cfgMode)
			if err != nil {
				return nil, fmt.Errorf("failed to persist pool mode: %v", err)
			}
			dbMode = cfgMode
		} else {
			return nil, err
		}
	}

	if cfgMode != dbMode {
		return nil, fmt.Errorf("database and config have differing values for "+
			"pool mode, config=%d, database=%d", cfgMode, dbMode)
	}

	if !h.cfg.SoloPool {
		log.Infof("Payment method is %s.", strings.ToUpper(hcfg.PaymentMethod))
	} else {
		log.Infof("Solo pool mode active.")
	}

	eCfg := &EndpointConfig{
		ActiveNet:             h.cfg.ActiveNet,
		db:                    h.cfg.DB,
		SoloPool:              h.cfg.SoloPool,
		Blake256Pad:           h.blake256Pad,
		NonceIterations:       h.cfg.NonceIterations,
		MaxConnectionsPerHost: h.cfg.MaxConnectionsPerHost,
		HubWg:                 h.wg,
		FetchMinerDifficulty:  h.poolDiffs.fetchMinerDifficulty,
		SubmitWork:            h.submitWork,
		FetchCurrentWork:      h.chainState.fetchCurrentWork,
		WithinLimit:           h.limiter.withinLimit,
		AddConnection:         h.addConnection,
		RemoveConnection:      h.removeConnection,
		FetchHostConnections:  h.fetchHostConnections,
		MaxGenTime:            h.cfg.MaxGenTime,
		SignalCache:           h.SignalCache,
		MonitorCycle:          h.cfg.MonitorCycle,
		MaxUpgradeTries:       h.cfg.MaxUpgradeTries,
		ClientTimeout:         h.cfg.ClientTimeout,
	}

	h.endpoint, err = NewEndpoint(eCfg, h.cfg.MinerListen)
	if err != nil {
		return nil, err
	}

	return h, nil
}

// Connect establishes a connection to the mining node and a wallet connection
// if the pool is a publicly avialable one.
func (h *Hub) Connect(ctx context.Context) error {
	// Establish a connection to the mining node.
	nodeConn, err := rpcclient.New(h.cfg.NodeRPCConfig,
		h.createNotificationHandlers())
	if err != nil {
		return err
	}

	if err := nodeConn.NotifyWork(ctx); err != nil {
		nodeConn.Shutdown()
		return fmt.Errorf("unable to subscribe for work "+
			"notifications: %v", err)
	}
	if err := nodeConn.NotifyBlocks(ctx); err != nil {
		nodeConn.Shutdown()
		return fmt.Errorf("unable to subscribe for block "+
			"notifications: %v", err)
	}

	h.nodeConn = nodeConn

	// Establish a connection to the wallet if the pool is
	// mining as a publicly available mining pool.
	if !h.cfg.SoloPool {
		serverCAs := x509.NewCertPool()
		serverCert, err := ioutil.ReadFile(h.cfg.WalletRPCCert)
		if err != nil {
			return err
		}
		if !serverCAs.AppendCertsFromPEM(serverCert) {
			return fmt.Errorf("no certificates found in %s",
				h.cfg.WalletRPCCert)
		}
		keypair, err := tls.LoadX509KeyPair(h.cfg.WalletTLSCert,
			h.cfg.WalletTLSKey)
		if err != nil {
			return fmt.Errorf("unable to read keypair: %v", err)
		}
		creds := credentials.NewTLS(&tls.Config{
			Certificates: []tls.Certificate{keypair},
			RootCAs:      serverCAs,
		})
		grpc, err := grpc.Dial(h.cfg.WalletGRPCHost,
			grpc.WithTransportCredentials(creds))
		if err != nil {
			return fmt.Errorf("unable to establish wallet "+
				"grpc connection: %v", err)
		}

		// Perform a Balance request to check connectivity and account
		// existence.
		walletConn := walletrpc.NewWalletServiceClient(grpc)
		req := &walletrpc.BalanceRequest{
			AccountNumber:         h.cfg.WalletAccount,
			RequiredConfirmations: 1,
		}
		_, err = walletConn.Balance(ctx, req)
		if err != nil {
			return err
		}

		h.walletConn = walletConn
		h.walletClose = grpc.Close

		confNotifs, err := walletConn.ConfirmationNotifications(ctx)
		if err != nil {
			return fmt.Errorf("unable to create confirmation "+
				"notification client: %v", err)
		}

		h.notifClient = confNotifs
	}

	return nil
}

// submitWork sends solved block data to the consensus daemon for evaluation.
func (h *Hub) submitWork(ctx context.Context, data *string) (bool, error) {
	if h.nodeConn == nil {
		return false, errs.PoolError(errs.Disconnected, "node disconnected")
	}

	return h.nodeConn.GetWorkSubmit(ctx, *data)
}

// getWork fetches available work from the consensus daemon.
func (h *Hub) getWork(ctx context.Context) (string, string, error) {
	if h.nodeConn == nil {
		return "", "", errs.PoolError(errs.Disconnected, "node disonnected")
	}
	work, err := h.nodeConn.GetWork(ctx)
	if err != nil {
		desc := fmt.Sprintf("unable to fetch current work: %v", err)
		return "", "", errs.PoolError(errs.GetWork, desc)
	}
	return work.Data, work.Target, err
}

// getTxConfNotifications streams transaction confirmation notifications for
// the provided transaction hashes.
func (h *Hub) getTxConfNotifications(txHashes []*chainhash.Hash, stopAfter int32) (func() (*walletrpc.ConfirmationNotificationsResponse, error), error) {
	hashes := make([][]byte, 0, len(txHashes))
	for _, hash := range txHashes {
		hashes = append(hashes, hash[:])
	}

	req := &walletrpc.ConfirmationNotificationsRequest{
		TxHashes:  hashes,
		StopAfter: stopAfter,
	}

	err := h.notifClient.Send(req)
	if err != nil {
		desc := fmt.Sprintf("unable to fetch tx confirmations: %v", err)
		return nil, errs.PoolError(errs.TxConf, desc)
	}

	return h.notifClient.Recv, nil
}

// getBlockConfirmation returns the number of block confirmations for the
// provided block height.
func (h *Hub) getBlockConfirmations(ctx context.Context, hash *chainhash.Hash) (int64, error) {
	info, err := h.nodeConn.GetBlockVerbose(ctx, hash, false)
	if err != nil {
		desc := fmt.Sprintf("unable to fetch block confirmations: %v", err)
		return 0, errs.PoolError(errs.BlockConf, desc)
	}
	return info.Confirmations, nil
}

// WithinLimit returns if a client is within its request limits.
func (h *Hub) WithinLimit(ip string, clientType int) bool {
	return h.limiter.withinLimit(ip, clientType)
}

// FetchLastWorkHeight returns the last work height of the pool.
func (h *Hub) FetchLastWorkHeight() uint32 {
	return h.chainState.fetchLastWorkHeight()
}

// FetchLastPaymentInfo returns the height, paid on time, and created on time,
// for the last payment made by the pool.
func (h *Hub) FetchLastPaymentInfo() (uint32, int64, int64, error) {
	height, paidOn, err := h.cfg.DB.loadLastPaymentInfo()
	if err != nil {
		return 0, 0, 0, nil
	}
	createdOn, err := h.cfg.DB.loadLastPaymentCreatedOn()
	if err != nil {
		return 0, 0, 0, nil
	}
	return height, paidOn, createdOn, nil
}

// getBlock fetches the blocks associated with the provided block hash.
func (h *Hub) getBlock(ctx context.Context, blockHash *chainhash.Hash) (*wire.MsgBlock, error) {
	if h.nodeConn == nil {
		return nil, errs.PoolError(errs.Disconnected, "node disconnected")
	}
	block, err := h.nodeConn.GetBlock(ctx, blockHash)
	if err != nil {
		desc := fmt.Sprintf("unable to fetch block %s: %v",
			blockHash.String(), err)
		return nil, errs.PoolError(errs.GetBlock, desc)
	}
	return block, nil
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
		log.Errorf("unable to decode block height %s: %v",
			string(heightD), err)
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
	job := NewJob(headerE, height)
	err = h.cfg.DB.persistJob(job)
	if err != nil {
		log.Error(err)
		return
	}
	workNotif := WorkNotification(job.UUID, prevBlock, genTx1, genTx2,
		blockVersion, nBits, nTime, true)
	h.endpoint.clientsMtx.Lock()
	for _, client := range h.endpoint.clients {
		select {
		case client.ch <- workNotif:
		default:
			// Non-blocking send fallthrough.
		}
	}
	h.endpoint.clientsMtx.Unlock()
}

// createNotificationHandlers returns handlers for block and work notifications.
func (h *Hub) createNotificationHandlers() *rpcclient.NotificationHandlers {
	return &rpcclient.NotificationHandlers{
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
}

// FetchWork queries the mining node for work. This should be called
// immediately the pool starts to avoid waiting for a work notification.
func (h *Hub) FetchWork(ctx context.Context) error {
	work, _, err := h.getWork(ctx)
	if err != nil {
		return err
	}
	h.chainState.setCurrentWork(work)
	return nil
}

// HasClients asserts the mining pool has clients.
func (h *Hub) HasClients() bool {
	return atomic.LoadInt32(&h.clients) > 0
}

// shutdown tears down the hub and releases resources used.
func (h *Hub) shutdown() {
	if h.endpoint.listener != nil {
		h.endpoint.listener.Close()
	}
	if !h.cfg.SoloPool {
		if h.walletClose != nil {
			_ = h.walletClose()
		}
	}
	if h.nodeConn != nil {
		h.nodeConn.Shutdown()
	}
	if h.notifClient != nil {
		_ = h.notifClient.CloseSend()
	}
}

// Run handles the process lifecycles of the pool hub.
func (h *Hub) Run(ctx context.Context) {
	h.wg.Add(2)
	go h.endpoint.run(ctx)
	go h.chainState.handleChainUpdates(ctx)

	// Wait until all hub processes have terminated, and then shutdown.
	h.wg.Wait()
	h.shutdown()
}

// FetchHashData returns all hash data from connected pool clients
// which have been updated in the last five minutes.
func (h *Hub) FetchHashData() (map[string][]*HashData, error) {
	fiveMinutesAgo := time.Now().Add(-time.Minute * 5).UnixNano()
	hashData, err := h.cfg.DB.listHashData(fiveMinutesAgo)
	if err != nil {
		return nil, err
	}

	toRemove := []string{}
	ids := h.endpoint.generateHashIDs()
	for _, data := range hashData {
		_, ok := ids[data.UUID]
		if !ok {
			toRemove = append(toRemove, data.UUID)
		}
	}

	// Remove all hash data not associated with clients currently
	// connected to the pool.
	for _, id := range toRemove {
		delete(hashData, id)
	}

	toReturn := make(map[string][]*HashData)
	for _, data := range hashData {
		toReturn[data.AccountID] = append(toReturn[data.AccountID], data)
	}

	return toReturn, err
}

// FetchPendingPayments fetches all unpaid payments.
func (h *Hub) FetchPendingPayments() ([]*Payment, error) {
	return h.cfg.DB.fetchPendingPayments()
}

// FetchArchivedPayments fetches all paid payments.
func (h *Hub) FetchArchivedPayments() ([]*Payment, error) {
	return h.cfg.DB.archivedPayments()
}

// FetchMinedWork returns work data associated with all blocks mined by the pool
// regardless of whether they are confirmed or not.
//
// List is ordered, most recent comes first.
func (h *Hub) FetchMinedWork() ([]*AcceptedWork, error) {
	return h.cfg.DB.listMinedWork()
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
		percentages, err = h.paymentMgr.PPSSharePercentages(time.Now().UnixNano())
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

// AccountExists checks if the provided account id references a pool account.
func (h *Hub) AccountExists(accountID string) bool {
	_, err := h.cfg.DB.fetchAccount(accountID)
	if err != nil {
		log.Tracef("unable to fetch account for id: %s", accountID)
		return false
	}
	return true
}

// CSRFSecret fetches a persisted secret or generates a new one.
func (h *Hub) CSRFSecret() ([]byte, error) {
	secret, err := h.cfg.DB.fetchCSRFSecret()

	if err != nil {
		if errors.Is(err, errs.ValueNotFound) {
			// If the database doesnt contain a CSRF secret, generate one and
			// persist it.
			secret = make([]byte, 32)
			_, err = rand.Read(secret)
			if err != nil {
				return nil, err
			}

			err = h.cfg.DB.persistCSRFSecret(secret)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return secret, nil
}

// HTTPBackupDB streams a backup of the database over an http response.
func (h *Hub) HTTPBackupDB(w http.ResponseWriter) error {
	return h.cfg.DB.httpBackup(w)
}
