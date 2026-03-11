package core

import (
	"context"
	"io"
	"time"

	"github.com/gorilla/websocket"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (app *ServerData) handleHostToRelayGameConnection(conn *websocket.Conn, gameId string) {

	closeReason := WebsocketConnectionCloseReason.Unspecified

	defer func() {
		if r := recover(); r != nil {
			app.Logger.Info("Recovered from panic in handleHostToRelayGameConnection:", "r", r)
			app.handleCleanup(conn, gameId, WebsocketConnectionCloseReason.InternalError)
		}
	}()

	app.Logger.Info("new SERVER ws")

	defer func(reason CloseReasonType) {
		app.handleCleanup(conn, gameId, reason)
	}(closeReason)

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

		start := time.Now()

		if err != nil {
			app.Logger.Error("Got error", "error", err.Error())
			closeReason = WebsocketConnectionCloseReason.SocketReadFailed
			break
		}

		if messageType != websocket.BinaryMessage {
			app.Logger.Warn("Warning: Message not binary, skipping")
			continue
		}

		writer, err := connection.NextWriter(websocket.BinaryMessage)
		if err != nil {
			app.Logger.Error("get writer failed", "error", err)
			closeReason = WebsocketConnectionCloseReason.SocketWriteFailed
			break
		}

		// packetBuffer.Reset()

		lr := io.LimitReader(reader, int64(app.Config.PacketMaximumSize)+1)
		n, err := io.CopyBuffer(writer, lr, streamingBuf)
		if err != nil && err != io.EOF {
			closeReason = WebsocketConnectionCloseReason.SocketReadFailed
			break
		}

		if n > int64(app.Config.PacketMaximumSize) {
			app.Logger.Info("Packet too large, kicking client.")
			closeReason = WebsocketConnectionCloseReason.PacketTooLarge
			break
		}
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			app.Logger.Error("Got error: ", "error", err.Error())
			closeReason = WebsocketConnectionCloseReason.SocketReadFailed
			break
		}

		// fmt.Println("Forwarding " + strconv.Itoa(len(packet)) + " bytes to client")

		// Forward to client

		if !exists {
			app.Logger.Error("Room no longer exists")
			closeReason = WebsocketConnectionCloseReason.RoomNotFound
			return
		}
		err = limiter.WaitN(context.Background(), int(n))
		if err != nil {
			app.Logger.Error("Rate limiting failed: ", "error", err.Error())
			closeReason = WebsocketConnectionCloseReason.InternalError
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
		app.Statistics.packetsPerSecond.Add(app.Ctx, 1, metric.WithAttributes(
			attribute.String("flow", "from_host"),
		))
		app.Statistics.bandwidth.Add(app.Ctx, n, metric.WithAttributes(
			attribute.String("flow", "from_host"),
		))
		duration := float64(time.Since(start).Nanoseconds()) / 1e6
		app.Statistics.packetProcessingTime.Record(app.Ctx, duration, metric.WithAttributes(
			attribute.String("flow", "from_host"),
		))

	}

}
