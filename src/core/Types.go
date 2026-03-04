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

/*

// Versionning
var RELAY_VERSION = "4.1.0" // Testing

// Bandwidth throttling - protects server bandwidth usage
// These settings are optimized for 6 players on Vanilla or lightly-modded clients (standard for Fabric 1.21.11)
var packetThrottlingOutboundHost = 1_500_000   // 1.5 MB/s of outbound size from host -> clients
var packetThrottlingBurstOutbound = 12_000_000 // 12 MB of "burst" bandwidth, handles joining spike and initial chunk loading
var packetThrottlingInboundHost = 300_000      // 300 KB/s of inbound size from clients -> host
var packetThrottlingBurstInbound = 4_000_000   // 4 MB of "burst" bandwidth, handles sync spike

// Packet size
var packetMaximumSize = 2_200_000 // 2.2 MB of maximum packet size

// Proof of work difficulty - prevents brute force / creating thousands of rooms
var difficulty6Threshold = 20   // Trigger difficulty 6 if exeeding 20 creation requests/s
var difficulty7Threshold = 100  // Trigger difficulty 7 if exceeding 100 creation requests/s
var difficultyCooldown = 30_000 // Wait 30 seconds before dropping the difficulty
// 67

// Cleanup
var roomEmptyCleanupDelay = int64(60_000)      // Cleanup an empty room if no host is connected after a full minute
var roomIdleNoClientDelay = int64(60_000 * 15) // Cleanup an empty room if there were no clients for 15 minutes

// Stability and instance health
var terminateWhenUnhealthy = true // Automatically terminate this instance to force a restart
var signalSocketWait = 50         // wait 50ms before parsing next message in SIGNAL connection

*/

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
}

func NewServerConfig() *ServerConfig {
	return &ServerConfig{
		RelayVersion:                  "4.1.0",
		PacketThrottlingOutboundHost:  1_500_000,
		PacketThrottlingBurstOutbound: 12_000_000,
		PacketThrottlingInboundHost:   300_000,
		PacketThrottlingBurstInbound:  4_000_000,
		PacketMaximumSize:             2_200_000,

		Difficulty6Threshold: 20,
		Difficulty7Threshold: 100,
		DifficultyCooldown:   30_000,

		RoomEmptyCleanupDelay:  60_000,
		RoomIdleNoClientDelay:  60_000 * 15,
		TerminateWhenUnhealthy: true,
		SignalSocketWait:       50,
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
