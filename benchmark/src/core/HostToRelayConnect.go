package core

import (
	"bytes"
	"time"

	"github.com/gorilla/websocket"
)

func (benchmarker *Benchmarker) HandleHostToRelayConnectionReadLoop(conn *websocket.Conn, room *Room) {
	defer conn.Close()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			benchmarker.Logger.Error("read error", "error", err)
			break
		}

		room.packetsPerSecondClientsToHost.Add(1)
	}
}

func (benchmarker *Benchmarker) HandleHostToRelayConnectionWriteLoop(conn *websocket.Conn, room *Room) {
	time.Sleep(5 * time.Second)

	for {
		err := conn.WriteMessage(websocket.BinaryMessage, bytes.Repeat([]byte("A"), benchmarker.HostToClientsBytesPerMessage))

		if err != nil {
			benchmarker.Logger.Error("write failed", "error", err)
			break
		}
	}
}

func (benchmarker *Benchmarker) CreateHostToRelayConnection(gameId string, requestId string) {
	room, exists := benchmarker.GetRoomExists(gameId)

	if !exists {
		benchmarker.Logger.Error("Room ID does not exist")
		return
	}

	urlScheme := benchmarker.GetURLSchemeJoin(true, gameId, requestId, "4.0.0")

	conn, _, err := websocket.DefaultDialer.Dial(urlScheme.String(), nil)

	if err != nil {
		benchmarker.Logger.Error("Failed to connect to the relay", "error", err)
		return
	}

	room.HostConnections = append(room.HostConnections, conn)

	go benchmarker.HandleHostToRelayConnectionReadLoop(conn, room)
	go benchmarker.HandleHostToRelayConnectionWriteLoop(conn, room)

}
