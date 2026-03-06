package core

import (
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

	streamingBuf := app.smallPool.Get().([]byte)
	defer app.smallPool.Put(streamingBuf)

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

		writer, err := connection.NextWriter(websocket.BinaryMessage)
		if err != nil {
			app.Logger.Error("get writer failed", "error", err)
			break
		}

		// packetBuffer.Reset()

		lr := io.LimitReader(reader, int64(app.Config.PacketMaximumSize)+1)
		n, err := io.CopyBuffer(writer, lr, streamingBuf)
		if err != nil && err != io.EOF {
			break
		}

		if n > int64(app.Config.PacketMaximumSize) {
			app.Logger.Info("Packet too large, kicking client.")
			break
		}
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			app.Logger.Error("Got error: ", "error", err.Error())
			break
		}

		// fmt.Println("Forwarding " + strconv.Itoa(len(packet)) + " bytes to client")

		// Forward to client

		if !exists {
			app.Logger.Error("Room no longer exists")
			return
		}
		err = limiter.WaitN(context.Background(), int(n))
		if err != nil {
			app.Logger.Error("Rate limiting failed: ", "error", err.Error())
			return
		}

		// No need to lock, only this thread will write to the connection
		/* err = connection.WriteMessage(websocket.BinaryMessage, buf[:n])

		if err != nil {
			app.Logger.Error("Message write failed: ", "error", err)
			return
		} */

		writer.Close()

		app.PacketCounter.Add(1)

	}

}
