package core

import (
	"bytes"
	"context"
	"io"

	"github.com/gorilla/websocket"
)

func (app *ServerData) handleHostToRelayGameConnection(conn *websocket.Conn, gameId string) {

	defer func() {
		if r := recover(); r != nil {
			app.Logger.Info("Recovered from panic in handleHostToRelayGameConnection:", "r", r)
			app.handleCleanup(conn, gameId)
		}
	}()

	app.Logger.Info("new SERVER ws")

	defer app.handleCleanup(conn, gameId)

	mainBuf := make([]byte, app.Config.PacketMaximumSize)
	packetBuffer := bytes.NewBuffer(mainBuf)

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists || room == nil {
		return
	}

	limiter := room.HostOutboundLimiter

	room.ClientsToHostConnectionsMutex.RLock()
	connection, exists := room.HostToClientsConnections[conn]
	room.ClientsToHostConnectionsMutex.RUnlock()

	if !exists {
		app.Logger.Warn("Connection not found")
		return
	}

	for {

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

		n, err := io.Copy(packetBuffer, io.LimitReader(reader, int64(app.Config.PacketMaximumSize)+1))

		if n > int64(app.Config.PacketMaximumSize) {
			app.Logger.Info("Packet too large, kicking client.")
			break
		}

		packet := packetBuffer.Bytes()

		// fmt.Println("Forwarding " + strconv.Itoa(len(packet)) + " bytes to client")

		// Forward to client

		if !exists {
			app.Logger.Error("Room no longer exists")
			return
		}
		err = limiter.WaitN(context.Background(), len(packet))
		if err != nil {
			app.Logger.Error("Rate limiting failed: ", "error", err.Error())
			return
		}

		// No need to lock, only this thread will write to the connection
		connection.WriteMessage(websocket.BinaryMessage, packet)

	}

}
