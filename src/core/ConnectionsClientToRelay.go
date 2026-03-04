package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"math/big"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
)

/*
Handles cleaning up data after a REQUESTCONNECTION is received and a new host to relay connection is detected
*/
func handleRequestCleanup(app *ServerData, request string, gameId string) {

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists {
		return
	}

	room.RequestedConnectionsMapMutex.RLock()
	_, exists = room.RequestedConnectionsMap[request]
	room.RequestedConnectionsMapMutex.RUnlock()

	if !exists {
		return
	}

	room.RequestedConnectionsMapMutex.Lock()
	delete(room.RequestedConnectionsMap, request)
	room.RequestedConnectionsMapMutex.Unlock()
}

func waitForHostConnectionAndReturnIt(app *ServerData, request string, clientConn *websocket.Conn, gameId string) *websocket.Conn {

	start := time.Now()

	defer handleRequestCleanup(app, request, gameId)

	for {

		app.RoomsMu.RLock()
		room, exists := app.Rooms[gameId]
		app.RoomsMu.RUnlock()

		if !exists {
			app.Logger.Error("Room ID does not exist", "roomId", gameId)
			return nil
		}

		if room.Host == nil {
			app.Logger.Error("Room has no host", "roomId", gameId)
			return nil
		}

		room.ClientsToHostConnectionsMutex.RLock()
		if hostConn, exists := room.ClientsToHostConnections[clientConn]; exists {

			if !exists {
				room.ClientsToHostConnectionsMutex.RUnlock()
				// fmt.Println("Host -> Relay connection not found")
				continue
			}

			if hostConn == nil {
				room.ClientsToHostConnectionsMutex.RUnlock()
				// fmt.Println("Host -> Relay connection is nil")
				continue
			}

			room.ClientsToHostConnectionsMutex.RUnlock()
			return hostConn
		}
		room.ClientsToHostConnectionsMutex.RUnlock()

		time.Sleep(100 * time.Millisecond)

		// Cancel if it's taking too long
		if time.Since(start) > 1*time.Minute {
			app.Logger.Error("Timed out waiting for host connection")
			return nil
		}
	}
}

func (app *ServerData) clientRelayHandler(conn *websocket.Conn, gameId string) {

	app.Logger.Info("new CLIENT ws")

	defer app.handleCleanup(conn, gameId)
	defer app.NumberOfClientsConnected.Add(-1)

	mainBuf := make([]byte, app.Config.PacketMaximumSize)

	app.RoomsMu.RLock()
	room, exists := app.Rooms[gameId]
	app.RoomsMu.RUnlock()

	if !exists {
		return
	}

	host := room.Host

	if host == nil {
		return
	}

	hostMu := room.HostMu

	if hostMu == nil {
		return
	}

	inboundLimiter := room.ClientsLimiter

	// Send signal

	n, err := rand.Int(rand.Reader, big.NewInt(9999999))

	if err != nil {
		app.Logger.Error("Error occurred while generating random number")

		return
	}

	message := "REQUESTCONNECTION_" + strconv.Itoa(int(n.Int64())) + gameId
	messageBytes := []byte(message)

	room.RequestedConnectionsMapMutex.Lock()
	app.Logger.Debug("Saved original message: " + message)
	room.RequestedConnectionsMap[message] = conn
	room.RequestedConnectionsMapMutex.Unlock()

	room.ClientsToHostConnectionsMutex.Lock()
	room.ClientsToHostConnections[conn] = nil
	room.ClientsToHostConnectionsMutex.Unlock()

	hostMu.Lock()
	err = host.WriteMessage(websocket.BinaryMessage, messageBytes)
	hostMu.Unlock()

	if err != nil {
		app.Logger.Error("Error occurred while trying to write signal: ", "error", err.Error())
		return
	}

	app.Logger.Info("Waiting for signal response...")

	hostConn := waitForHostConnectionAndReturnIt(app, message, conn, gameId)

	if hostConn == nil {
		app.Logger.Error("Failed to get host connection", "message", message)
		return
	}

	packetBuffer := bytes.NewBuffer(mainBuf)

	for {
		messageType, reader, err := conn.NextReader()
		if err != nil {
			app.Logger.Error("Got error: ", "error", err.Error())
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
			break
		}

		packet := packetBuffer.Bytes()

		err = inboundLimiter.WaitN(context.Background(), len(packet))
		if err != nil {
			app.Logger.Error("Inbound rate limiting failed: ", "error", err.Error())
			return
		}

		// Forward

		// fmt.Println("Forwarding " + strconv.Itoa(len(packet)) + " bytes to Server")

		err = hostConn.WriteMessage(websocket.BinaryMessage, packet)

		if err != nil {
			app.Logger.Error("Got error: ", "error", err.Error())
			break
		}

	}

}
