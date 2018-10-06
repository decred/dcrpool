package ws

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coreos/bbolt"
	"github.com/decred/dcrd/rpcclient"

	"dnldd/dcrpool/limiter"
	"dnldd/dcrpool/worker"
)

// Hub maintains the set of active clients and facilitates message broadcasting
// to all active clients.
type Hub struct {
	db             *bolt.DB
	httpc          *http.Client
	limiter        *limiter.RateLimiter
	Broadcast      chan Message
	rpcc           *rpcclient.Client
	rpccMtx        sync.Mutex
	currTarget     uint32
	poolTargets    map[string]uint32
	poolTargetsMtx sync.RWMutex
	ConnCount      uint64
	Ticker         *time.Ticker
}

// NewHub initializes a websocket hub.
func NewHub(db *bolt.DB, httpc *http.Client, rpccfg *rpcclient.ConnConfig, limiter *limiter.RateLimiter) (*Hub, error) {
	h := &Hub{
		db:          db,
		httpc:       httpc,
		limiter:     limiter,
		currTarget:  0,
		poolTargets: make(map[string]uint32, 0),
		Broadcast:   make(chan Message),
		ConnCount:   0,
		Ticker:      time.NewTicker(time.Second * 5),
	}

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

			// Fetch the target difficulty.
			header := []byte(blkHeader)
			currTarget, err := FetchTargetDifficulty(header)
			if err != nil {
				log.Error(err)
				return
			}

			// Update the current target difficulty and calculate the pool
			// targets for all miner types if the received target difficulty
			// is not equal to the cached target difficulty.
			if h.currTarget == 0 || h.currTarget != currTarget {
				atomic.StoreUint32(&h.currTarget, currTarget)
				h.calculatePoolTargets()
			}

			// Broadcast a work notification.
			h.Broadcast <- WorkNotification(blkHeader, "")
		},
	}

	rpcc, err := rpcclient.New(rpccfg, ntfnHandlers)
	if err != nil {
		return nil, err
	}

	h.rpcc = rpcc

	// Subscribe for chain notifications.
	if err := rpcc.NotifyWork(); err != nil {
		rpcc.Shutdown()
		return nil, err
	}
	if err := rpcc.NotifyBlocks(); err != nil {
		rpcc.Shutdown()
		return nil, err
	}

	return h, nil
}

// Close terminates all connected clients to the hub.
func (h *Hub) Close() {
	h.Broadcast <- nil
}

// calculatePoolTargets computes the pool targets for all miner types.
func (h *Hub) calculatePoolTargets() {
	tdiff := atomic.LoadUint32(&h.currTarget)
	h.poolTargetsMtx.Lock()
	// 1 percent of the target diff for cpu miners.
	h.poolTargets[worker.CPU] = uint32(0.01 * float64(tdiff))
	// TODO: add more miner types.
	h.poolTargetsMtx.Unlock()
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
