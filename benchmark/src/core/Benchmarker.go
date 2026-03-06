package core

import (
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

type Room struct {
	GameId                        string
	Host                          *websocket.Conn
	HostWriteMu                   sync.Mutex
	Clients                       []*websocket.Conn
	HostConnections               []*websocket.Conn
	packetsPerSecondClientsToHost atomic.Int64
	packetsPerSecondHostToClients atomic.Int64
}

func NewRoom(GameId string) *Room {
	return &Room{
		GameId:                        GameId,
		Clients:                       []*websocket.Conn{},
		HostConnections:               []*websocket.Conn{},
		HostWriteMu:                   sync.Mutex{},
		Host:                          nil,
		packetsPerSecondClientsToHost: atomic.Int64{},
		packetsPerSecondHostToClients: atomic.Int64{},
	}
}

type Benchmarker struct {
	Rooms                        map[string]*Room
	RelayURL                     string
	HostToClientsBytesPerMessage int
	ClientToHostBytesPerMessage  int
	Logger                       *slog.Logger
	RelayURLButWithoutTheHTTP    string

	RoomsMu sync.RWMutex
}

var opts = &slog.HandlerOptions{
	AddSource: true, // include file:line
}

var logger = slog.New(slog.NewJSONHandler(os.Stdout, opts))

func NewBenchmarker() *Benchmarker {
	return &Benchmarker{
		Rooms:                     make(map[string]*Room),
		RelayURL:                  "http://127.0.0.1:7860",
		RelayURLButWithoutTheHTTP: "127.0.0.1:7860",
		Logger:                    logger,
		RoomsMu:                   sync.RWMutex{},

		HostToClientsBytesPerMessage: 1,
		ClientToHostBytesPerMessage:  1,
	}
}
