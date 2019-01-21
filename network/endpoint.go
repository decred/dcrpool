package network

import (
	"fmt"
	"net"
	"strings"
	"sync"
)

const (
	// TCP represents the tcp network protocol.
	TCP = "tcp"

	// protocol defines the stratum protocol identifier.
	protocol = "stratum+tcp"

	// MaxMessageSize represents the maximum size of a  transmitted message,
	// in bytes.
	MaxMessageSize = 511

	// BigEndian represents the big endian byte layout scheme.
	BigEndian = "bigendian"

	// LittleEndian represent the little endian byte layout scheme.
	LittleEndian = "littleendian"
)

// Endpoint represents a stratum endpoint.
type Endpoint struct {
	addr       *net.TCPAddr
	server     *net.TCPListener
	target     uint32
	miner      string
	hub        *Hub
	clients    []*Client
	clientsMtx sync.Mutex
}

// SanitizeAddress sanitizes a stratum address.
func SanitizeAddress(address string) string {
	return strings.Replace(address, protocol, "", 1)
}

// NewEndpoint creates an endpoint instance.
func NewEndpoint(hub *Hub, port uint32, miner string) (*Endpoint, error) {
	url := SanitizeAddress(fmt.Sprintf("%s:%d", hub.cfg.Domain, port))
	addr, err := net.ResolveTCPAddr("tcp", url)
	if err != nil {
		return nil, fmt.Errorf("Failed to resolve tcp address: %v", err)
	}

	endpoint := &Endpoint{
		addr:  addr,
		hub:   hub,
		miner: miner,
	}

	hub.poolTargetsMtx.Lock()
	target := hub.poolTargets[miner]
	hub.poolTargetsMtx.Unlock()
	if target == 0 {
		return nil, fmt.Errorf("Pool target not found for miner (%s)", miner)
	}

	endpoint.target = target

	return endpoint, nil
}

// Listen sets up a listener for incoming messages on the endpoint. It must be
// run as a goroutine.
func (e *Endpoint) Listen() {
	server, err := net.ListenTCP(TCP, e.addr)
	if err != nil {
		log.Errorf("Failed to listen on tcp address: %v", err)
	}

	e.server = server
	defer e.server.Close()

	log.Infof("Listening on %v for %v", e.addr.Port, e.miner)

	for {
		select {
		case <-e.hub.ctx.Done():
			e.clientsMtx.Lock()
			for _, client := range e.clients {
				client.cancel()
			}
			e.clientsMtx.Unlock()

			log.Infof("Listener for %v on %v done", e.miner, e.addr.Port)
			return

		default:
			// Non-blocking receive fallthrough.
		}

		conn, err := e.server.AcceptTCP()
		if err != nil {
			log.Tracef("Failed to accept tcp connection: %v", err)
			continue
		}

		conn.SetKeepAlive(true)
		ip, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
		client := NewClient(conn, e, ip)
		go client.Listen()
		go client.Send()
	}
}
