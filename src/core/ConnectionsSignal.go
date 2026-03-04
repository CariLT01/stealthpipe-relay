package core

import (
	"bytes"
	"github.com/gorilla/websocket"
	"io"
	"time"
)

func handleSignalRelayPacket(app *ServerData, message []byte, conn *websocket.Conn, gameId string) {
	/*

		bytes:
		0 - keep-alive
		1 - ping
		2 - pong
	*/

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists {
		return
	}

	if len(message) == 0 {
		return
	}
	packetType := message[0]

	switch packetType {
	// only act for ping, and return pong
	case 0x01:
		room.HostMu.Lock() // lock to prevent race condition
		conn.WriteMessage(websocket.BinaryMessage, []byte{0x02})
		room.HostMu.Unlock()
	}

}

func (app *ServerData) signalRelayHandler(conn *websocket.Conn, gameId string) {

	app.Logger.Info("New SIGNAL ws")

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists || room == nil {
		return
	}

	defer app.handleCleanup(conn, gameId)
	defer app.NumberOfClientsConnected.Add(-1)

	mainBuf := make([]byte, app.Config.PacketMaximumSize)
	packetBuffer := bytes.NewBuffer(mainBuf)

	for {

		time.Sleep(time.Duration(app.Config.SignalSocketWait) * time.Millisecond)

		messageType, reader, err := conn.NextReader()
		if err != nil {
			app.Logger.Error("Got error", "error", err.Error())
			break
		}

		if messageType != websocket.BinaryMessage {
			app.Logger.Warn("Warning: Message not binary, skipping")
			continue
		}

		packetBuffer.Reset()

		n, _ := io.Copy(packetBuffer, io.LimitReader(reader, int64(app.Config.PacketMaximumSize)+1))

		if n > int64(app.Config.PacketMaximumSize) {
			app.Logger.Info("Packet too large, kicking client.")
			break // This exits the loop and triggers handleCleanup
		}

		packet := packetBuffer.Bytes()

		handleSignalRelayPacket(app, packet, conn, gameId)
	}

}
