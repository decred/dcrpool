package network

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/decred/dcrd/blockchain"
	"github.com/decred/dcrd/wire"
	"github.com/gorilla/websocket"

	"dnldd/dcrpool/dividend"
)

const (
	// uint256Size is the number of bytes needed to represent an unsigned
	// 256-bit integer.
	uint256Size = 32
)

// Client defines an established websocket connection.
type Client struct {
	hub         *Hub
	minerType   string
	id          uint64
	ws          *websocket.Conn
	wsMtx       sync.RWMutex
	req         map[uint64]string
	reqMtx      sync.RWMutex
	ch          chan Message
	Ctx         context.Context
	cancel      context.CancelFunc
	ip          string
	ticker      *time.Ticker
	accountID   string
	pingRetries uint64
}

// recordRequest logs the client request as an id/method pair.
func (c *Client) recordRequest(id uint64, method string) {
	c.reqMtx.Lock()
	c.req[id] = method
	c.reqMtx.Unlock()
}

// fetchRequest fetches the request method of the provided request id.
func (c *Client) fetchRequest(id uint64) string {
	var method string
	c.reqMtx.RLock()
	method = c.req[id]
	c.reqMtx.RUnlock()
	return method
}

// deleteRequest removes the request referenced by the provided id.
func (c *Client) deleteRequest(id uint64) {
	c.reqMtx.Lock()
	delete(c.req, id)
	c.reqMtx.Unlock()
}

// claimWeightedShare records a weighted share corresponding to the hash power
// of the mining client for the account it is authenticated under. This serves
// as proof of verifiable work contributed to the mining pool.
func (c *Client) claimWeightedShare() {
	weight := dividend.ShareWeights[c.minerType]
	share := dividend.NewShare(c.accountID, weight)
	share.Create(c.hub.db)
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

// NewClient initializes a new websocket client.
func NewClient(h *Hub, socket *websocket.Conn, ip string, ticker *time.Ticker,
	minerType string, account string) *Client {
	ctx, cancel := context.WithCancel(context.TODO())
	atomic.AddUint64(&h.ConnCount, 1)
	return &Client{
		hub:         h,
		minerType:   minerType,
		ws:          socket,
		ip:          ip,
		ch:          make(chan Message),
		Ctx:         ctx,
		cancel:      cancel,
		ticker:      ticker,
		accountID:   account,
		req:         make(map[uint64]string, 0),
		pingRetries: 0,
	}
}

// Process parses and handles inbound client messages.
func (c *Client) Process(ctx context.Context) {
	c.ws.SetReadLimit(MaxMessageSize)
out:
	for {
		select {
		case <-ctx.Done():
			// Decrement the connection counter.
			atomic.AddUint64(&c.hub.ConnCount, ^uint64(0))

			// Update the estimated hash rate of the pool.
			c.hub.RemoveHashRate(c.minerType)

			break out
		default:
			// Non-blocking receive fallthrough.
		}

		// Wait for a message.
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway, websocket.CloseAbnormalClosure,
				websocket.CloseNormalClosure) {
				log.Errorf("Websocket read error (pool client): %v", err)
			}
			c.cancel()
			continue
		}

		msg, reqType, err := IdentifyMessage(data)
		if err != nil {
			log.Debug(err)
			c.cancel()
			continue
		}

		// Ensure the requesting client is within their request limits, which
		// applies for both websocket requests by the client as well as
		// requests via the API.
		allow := c.hub.limiter.WithinLimit(c.ip)
		if !allow {
			switch reqType {
			case RequestType:
				req := msg.(*Request)
				c.ch <- tooManyRequestsResponse(req.ID)
				continue
			case NotificationType:
				// Clients are not allowed to send notifications.
				c.cancel()
				continue
			}
		}

		switch reqType {
		case RequestType:
			req := msg.(*Request)
			switch req.Method {
			case SubmitWork:
				submission, err := ParseWorkSubmissionRequest(req)
				if err != nil {
					log.Debug(err)
					msg := err.Error()
					c.ch <- EvaluatedWorkResponse(req.ID, &msg, false)
					continue
				}

				decoded, err := DecodeHeader([]byte(*submission))
				if err != nil {
					log.Errorf("failed to decode header: %v", err)
					continue
				}

				header, err := ParseBlockHeader(decoded)
				if err != nil {
					log.Errorf("failed to parse block header: %v", err)
					return
				}

				// Fetch the pool target for the client based on its
				// miner type.
				c.hub.poolTargetsMtx.RLock()
				pTarget := c.hub.poolTargets[c.minerType]
				c.hub.poolTargetsMtx.RUnlock()

				target := blockchain.CompactToBig(header.Bits)
				poolTarget := blockchain.CompactToBig(pTarget)
				hash := header.BlockHash()
				hashNum := blockchain.HashToBig(&hash)

				// Only submit work if the submitted blockhash is below the
				// target difficulty and the specified pool target for the
				// client.
				if hashNum.Cmp(poolTarget) > 0 {
					log.Errorf("submitted work (%v) is not less than the "+
						"client's pool target (%v)", hashNum, poolTarget)
					continue
				}

				if hashNum.Cmp(target) < 0 {
					accepted, err := c.hub.SubmitWork(submission)
					if err != nil {
						msg := err.Error()
						c.ch <- EvaluatedWorkResponse(req.ID, &msg, false)
						continue
					}

					// if the work is accepted, parse the embedded header and
					// persist the accepted work created from it.
					if accepted {
						nonce := FetchNonce(decoded)
						work := NewAcceptedWork(nonce)
						work.Create(c.hub.db)
					}

					c.ch <- EvaluatedWorkResponse(req.ID, nil, accepted)
				}

				// Claim a weighted share for work contributed to the pool.
				c.claimWeightedShare()

			default:
				log.Debugf("Unknowning request type received")
			}

		case ResponseType:
			resp := msg.(*Response)
			method := c.fetchRequest(*resp.ID)
			if method == "" {
				log.Error("No request found for received response "+
					"with id: ", *resp.ID)
				continue
			}

			switch method {
			case Ping:
				c.deleteRequest(*resp.ID)
				reply := resp.Result["response"].(string)
				if reply != Pong {
					continue
				}

				atomic.StoreUint64(&c.pingRetries, 0)
			default:
				log.Debugf("Unknown response type received")
				c.cancel()
				continue
			}

		case NotificationType:
		default:
			log.Errorf("Unknown message type received")
			c.cancel()
			continue
		}
	}
}

// Send messages to a client.
func (c *Client) Send(ctx context.Context) {
out:
	for {
		select {
		case <-ctx.Done():
			c.wsMtx.Lock()
			c.ws.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			c.wsMtx.Unlock()
			close(c.ch)
			break out

		case <-c.ticker.C:
			// Close the connection if unreachable after 3 pings.
			retries := atomic.LoadUint64(&c.pingRetries)
			if retries == MaxPingRetries {
				log.Debugf("Client %v unreachable, closing connection.", c.ip)
				c.cancel()
				continue
			}

			id := c.nextID()
			c.recordRequest(*id, Ping)
			c.wsMtx.Lock()
			err := c.ws.WriteJSON(PingRequest(id))
			c.wsMtx.Unlock()
			if err != nil {
				log.Error(err)
				c.cancel()
				continue
			}
			atomic.AddUint64(&c.pingRetries, 1)

		case msg := <-c.ch:
			// Close the connection on receiving a nil message reference.
			if msg == nil {
				c.cancel()
				continue
			}

			c.wsMtx.Lock()
			err := c.ws.WriteJSON(msg)
			c.wsMtx.Unlock()
			if err != nil {
				log.Error(err)
				c.cancel()
				continue
			}

		case msg := <-c.hub.Broadcast:
			// Close the connection on receiving a nil message reference.
			if msg == nil {
				c.cancel()
				continue
			}

			req := msg.(*Request)
			switch req.Method {
			case Work:
				// Fetch the header of the work notification.
				header, _, err := ParseWorkNotification(req)
				if err != nil {
					log.Error(err)
					c.cancel()
					continue
				}

				// Fetch the pool target for the client based on its
				// miner type.
				c.hub.poolTargetsMtx.RLock()
				target := c.hub.poolTargets[c.minerType]
				c.hub.poolTargetsMtx.RUnlock()

				// Set the miner's pool id.
				id := GeneratePoolID()

				// Set the pool id of the client.
				setPoolID(header, id)

				// Encode the updated haeader.
				encoded := EncodeHeader(header)

				// Covert the pool target to little endian hex encoded byte
				// slice.
				targetLE := BigToLEUint256(blockchain.CompactToBig(target))
				t := hex.EncodeToString(targetLE[:])

				// Send an updated work notification per the client's miner type.
				r := WorkNotification(string(encoded), t)
				c.wsMtx.Lock()
				err = c.ws.WriteJSON(r)
				c.wsMtx.Unlock()
				if err != nil {
					log.Error(err)
					c.cancel()
					continue
				}

			default:
				// For all other message types forward the broadcasted message
				// to the client.
				c.wsMtx.Lock()
				err := c.ws.WriteJSON(msg)
				c.wsMtx.Unlock()
				if err != nil {
					log.Error(err)
					c.cancel()
					continue
				}
			}
		}
	}
}

// nextID returns the next message id for the client.
func (c *Client) nextID() *uint64 {
	id := atomic.AddUint64(&c.id, 1)
	return &id
}

// FetchBlockHeight retrieves the block height from the provided hex encoded
// block header.
func FetchBlockHeight(encoded []byte) (uint32, error) {
	data := []byte(encoded)
	decoded := make([]byte, hex.DecodedLen(len(encoded)))
	_, err := hex.Decode(decoded, data)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(decoded[128:132]), nil
}

// FetchTargetDifficulty retrieves the target difficulty from the provided hex
// encoded block header.
func FetchTargetDifficulty(encoded []byte) (uint32, error) {
	data := []byte(encoded)
	decoded := make([]byte, hex.DecodedLen(len(encoded)))
	_, err := hex.Decode(decoded, data)
	if err != nil {
		return 0, err
	}

	return binary.LittleEndian.Uint32(decoded[116:120]), nil
}

// GeneratePoolID generates a random 4-byte slice.  This is intended to be used
// as the pool id for miners.
func GeneratePoolID() []byte {
	id := make([]byte, 4)
	rand.Read(id)
	return id
}

// The block header provides 12-bytes of nonce space which constitutes of
// 8-bytes nonce and 4-bytes worker id to prevent duplicate work for mining
// pools by haste semantics.
// Format:
// 	<0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00>, [0x00, 0x00, 0x00, 0x00]

// setPoolID sets the assigned pool id for a miner.  This is to prevent
// duplicate work.
func setPoolID(decoded []byte, id []byte) {
	copy(decoded[148:152], id[:])
}

// SetGeneratedNonce sets the worker generated nonce for the decoded header data.
func SetGeneratedNonce(decoded []byte, nonce uint64) {
	binary.LittleEndian.PutUint64(decoded[140:148], nonce)
}

// FetchNonce fetches the worker generated nonce and the assigned pool id
// from the decoded header data.
func FetchNonce(decoded []byte) []byte {
	return decoded[140:152]
}

// EncodeHeader encodes the decoded header data.
func EncodeHeader(decoded []byte) []byte {
	data := make([]byte, hex.EncodedLen(len(decoded)))
	_ = hex.Encode(data, decoded)
	return data
}

// DecodeHeader decodes the hex encoded header data.
func DecodeHeader(encoded []byte) ([]byte, error) {
	decoded := make([]byte, hex.DecodedLen(len(encoded)))
	_, err := hex.Decode(decoded, encoded)
	if err != nil {
		return nil, err
	}

	return decoded, nil
}

// ParseBlockHeader deserializes the block header from the provided hex
// encoded header data.
func ParseBlockHeader(decoded []byte) (*wire.BlockHeader, error) {
	var header wire.BlockHeader
	reader := bytes.NewReader(decoded[:180])
	err := header.Deserialize(reader)
	if err != nil {
		return nil, err
	}

	return &header, nil
}
