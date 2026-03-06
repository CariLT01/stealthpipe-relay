package core

import (
	"bytes"
	"time"

	"github.com/gorilla/websocket"
)

func (benchmarker *Benchmarker) HandleClientToRelayConnectionReadLoop(conn *websocket.Conn, room *Room) {
	defer conn.Close()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			benchmarker.Logger.Error("read failed", "error", err)
			break
		}

		room.packetsPerSecondHostToClients.Add(1)
	}
}

func (benchmarker *Benchmarker) HandleClientToRelayConnectionWriteLoop(conn *websocket.Conn, room *Room) {

	time.Sleep(5 * time.Second)
	for {
		err := conn.WriteMessage(websocket.BinaryMessage, bytes.Repeat([]byte("A"), benchmarker.ClientToHostBytesPerMessage))

		if err != nil {
			benchmarker.Logger.Error("write failed", "error", err)
			break
		}
	}
}

func (benchmarker *Benchmarker) ConnectClientToRelay(gameId string) {
	room, exists := benchmarker.GetRoomExists(gameId)

	if !exists {
		benchmarker.Logger.Error("room Id does not exist")
		return
	}

	urlScheme := benchmarker.GetURLSchemeJoin(false, gameId, "", "4.0.0")

	conn, _, err := websocket.DefaultDialer.Dial(urlScheme.String(), nil)

	if err != nil {
		benchmarker.Logger.Error("wss dialing failed", "error", err)
		return
	}

	go benchmarker.HandleClientToRelayConnectionReadLoop(conn, room)
	go benchmarker.HandleClientToRelayConnectionWriteLoop(conn, room)

}
