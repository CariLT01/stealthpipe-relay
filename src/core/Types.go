package core

import (
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

type Room struct {
	Host                          *websocket.Conn
	HostMu                        *sync.Mutex
	HostIP                        string
	LastRoomFilledTime            atomic.Int64
	ClientsToHostConnections      map[*websocket.Conn]*websocket.Conn
	HostToClientsConnections      map[*websocket.Conn]*websocket.Conn
	RequestedConnectionsMap       map[string]*websocket.Conn
	RequestedConnectionsMapMutex  *sync.RWMutex
	ClientsToHostConnectionsMutex *sync.RWMutex
	HostToClientsConnectionsMutex *sync.RWMutex
	HostOutboundLimiter           *rate.Limiter
	ClientsLimiter                *rate.Limiter
	CreatedTime                   int64
	HasHost                       bool
}

type ServerData struct {
	Rooms                    map[string]*Room
	RoomsMu                  sync.RWMutex
	Upgrader                 websocket.Upgrader
	LastTick                 int64
	PacketCounter            atomic.Int64
	PacketsPerSecond         atomic.Int64
	NumberOfClientsConnected atomic.Int64

	HandshakeCounter  uint64
	CurrentDifficulty int32
	LastUpped         time.Time
	UsedSalts         map[string]int64
	UsedSaltsMutex    sync.Mutex
	IsHealthy         atomic.Bool

	Logger *slog.Logger

	Config *ServerConfig
}

type ServerConfig struct {
	// Versioning
	RelayVersion string

	// Bandwidth throttling (optimized for 6 players)
	PacketThrottlingOutboundHost  int // bytes/sec from host -> clients
	PacketThrottlingBurstOutbound int // burst bytes
	PacketThrottlingInboundHost   int // bytes/sec from clients -> host
	PacketThrottlingBurstInbound  int // burst bytes

	// Packet size
	PacketMaximumSize int // maximum packet size in bytes

	// Proof-of-work difficulty
	Difficulty6Threshold int   // trigger difficulty 6
	Difficulty7Threshold int   // trigger difficulty 7
	DifficultyCooldown   int64 // cooldown in milliseconds

	// Cleanup
	RoomEmptyCleanupDelay int64 // milliseconds
	RoomIdleNoClientDelay int64 // milliseconds

	// Stability and instance health
	TerminateWhenUnhealthy bool
	SignalSocketWait       int // milliseconds

	// Tokens
	reuseTokenExpiryHours int
	powTokenExpiryMinutes int
}

func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		RelayVersion:                  "4.1.0",    // relay protocol version
		PacketThrottlingOutboundHost:  1_500_000,  // 1.5 MB
		PacketThrottlingBurstOutbound: 12_000_000, // 12 MB
		PacketThrottlingInboundHost:   300_000,    // 300 KB
		PacketThrottlingBurstInbound:  4_000_000,  // 4 MB
		PacketMaximumSize:             2_200_000,  // 2.2 MB

		Difficulty6Threshold: 20,     // 20 rps
		Difficulty7Threshold: 100,    // 100 rps
		DifficultyCooldown:   30_000, // milliseconds

		RoomEmptyCleanupDelay:  60_000,      // milliseconds
		RoomIdleNoClientDelay:  60_000 * 15, // milliseconds
		TerminateWhenUnhealthy: true,
		SignalSocketWait:       50,

		reuseTokenExpiryHours: 3,
		powTokenExpiryMinutes: 5,
	}
}

var opts = &slog.HandlerOptions{
	AddSource: true, // include file:line
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))

func NewServer() *ServerData {
	return &ServerData{
		Rooms:                    make(map[string]*Room),
		RoomsMu:                  sync.RWMutex{},
		Upgrader:                 websocket.Upgrader{ReadBufferSize: 65536, WriteBufferSize: 65536},
		LastTick:                 time.Now().UnixMilli(),
		PacketCounter:            atomic.Int64{},
		PacketsPerSecond:         atomic.Int64{},
		NumberOfClientsConnected: atomic.Int64{},
		CurrentDifficulty:        5,
		UsedSalts:                make(map[string]int64),
		UsedSaltsMutex:           sync.Mutex{},
		IsHealthy:                atomic.Bool{},
		Logger:                   logger,
		Config:                   NewServerConfig(),
	}
}
