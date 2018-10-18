package ws

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coreos/bbolt"
	"github.com/davecgh/go-spew/spew"
	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/rpcclient"
	"github.com/decred/dcrwallet/rpc/walletrpc"

	"dnldd/dcrpool/limiter"
	"dnldd/dcrpool/worker"
)

// HubConfig represents configuration details for the hub.
type HubConfig struct {
	DcrdRPCCfg        *rpcclient.ConnConfig
	PoolFee           *big.Rat
	MaxGenTime        *big.Int
	WalletRPCCertFile string
	WalletGRPCHost    string
}

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	db             *bolt.DB
	httpc          *http.Client
	cfg            *HubConfig
	limiter        *limiter.RateLimiter
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
}

// NewHub initializes a websocket hub.
func NewHub(db *bolt.DB, httpc *http.Client, hcfg *HubConfig, limiter *limiter.RateLimiter) (*Hub, error) {
	h := &Hub{
		db:          db,
		httpc:       httpc,
		limiter:     limiter,
		cfg:         hcfg,
		hashRate:    worker.ZeroInt,
		poolTargets: make(map[string]uint32),
		Broadcast:   make(chan Message),
		ConnCount:   0,
		Ticker:      time.NewTicker(time.Second * 5),
	}

	// Calculate pool targets for all known miners.
	h.poolTargetsMtx.Lock()
	for miner, hashrate := range worker.MinerHashes {
		target := worker.CalculatePoolTarget(hashrate, h.cfg.MaxGenTime)
		compactTarget := blockchain.BigToCompact(target)
		h.poolTargets[miner] = compactTarget
	}
	h.poolTargetsMtx.Unlock()

	log.Debugf("Pool targets are: %v", spew.Sdump(h.poolTargets))

	// Create handlers for chain notifications being subscribed for.
	ntfnHandlers := &rpcclient.NotificationHandlers{
		OnBlockConnected: func(blkHeader []byte, transactions [][]byte) {
			log.Debugf("Block connected: %x", blkHeader)

			if !h.HasConnectedClients() {
				return
			}

			blkHeight, err := FetchBlockHeight(blkHeader)
			if err != nil {
				log.Error(err)
				return
			}

			h.Broadcast <- ConnectedBlockNotification(blkHeight)
		},
		OnBlockDisconnected: func(blkHeader []byte) {
			log.Debugf("Block disconnected: %x", blkHeader)

			if !h.HasConnectedClients() {
				return
			}

			blkHeight, err := FetchBlockHeight(blkHeader)
			if err != nil {
				log.Error(err)
				return
			}

			h.Broadcast <- DisconnectedBlockNotification(blkHeight)
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

	h.grpc = walletrpc.NewWalletServiceClient(h.gConn)
	log.Debugf("GRPC connection established with dcrwallet.")

	return h, nil
}

// Shutdown terminates all connected clients to the hub and releases all
// resources used.
func (h *Hub) Shutdown() {
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

// AddHashRate adds the hash power provided by the newly connected client
//to the total estimated hash power of pool.
func (h *Hub) AddHashRate(miner string) {
	hash := worker.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Add(h.hashRate, hash)
	log.Debugf("Client connected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}

// RemoveHashRate removes the hash power previously provided by the
// disconnected client from the total estimated hash power of the pool.
func (h *Hub) RemoveHashRate(miner string) {
	hash := worker.MinerHashes[miner]
	h.hashRateMtx.Lock()
	h.hashRate = new(big.Int).Sub(h.hashRate, hash)
	log.Debugf("Client disconnected, updated pool hash rate is %v", h.hashRate)
	h.hashRateMtx.Unlock()
}
