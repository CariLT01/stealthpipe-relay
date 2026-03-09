package core

import (
	"github.com/gorilla/websocket"
)

func (app *ServerData) handleCleanup(conn *websocket.Conn, gameId string) {

	app.Logger.Debug("Handle cleanup called on connection")

	// 1. Acquire the lock ONLY to modify the map
	app.RoomsMu.Lock()

	room, exists := app.Rooms[gameId]
	if !exists || room == nil {
		app.RoomsMu.Unlock()
		return
	}

	// 2. Identify what needs to be closed, but DON'T close it yet
	var toClose []*websocket.Conn

	if room.Host == conn {
		// If host leaves, we need to close all clients
		// The signal WS is disconnected
		for ws1, ws2 := range room.ClientsToHostConnections {
			toClose = append(toClose, ws2)
			toClose = append(toClose, ws1)
		}

		delete(app.Rooms, gameId)
		app.Logger.Info("Host left, room deleted")
	} else {
		// If client leaves, just remove them from the map
		// uuid := room.ClientsReverseMap[conn]
		// delete(room.Clients, uuid)
		// delete(room.ClientsReverseMap, conn)
		// delete(room.ClientsMutexes, conn)

		room.ClientsToHostConnectionsMutex.RLock()
		hostConnection, exists1 := room.ClientsToHostConnections[conn]
		room.ClientsToHostConnectionsMutex.RUnlock()

		room.HostToClientsConnectionsMutex.RLock()
		clientConnection, exists2 := room.HostToClientsConnections[conn]
		room.HostToClientsConnectionsMutex.RUnlock()

		if exists1 {
			app.Logger.Info("Closing host to relay connection. Cause: HandleCleanup")
			toClose = append(toClose, hostConnection)
		}
		if exists2 {
			app.Logger.Info("Closing client to relay connection. Cause: HandleCleanup")
			toClose = append(toClose, clientConnection)
		}

		toClose = append(toClose, conn) // Add the client to the close list
		// fmt.Println("Client left:", uuid)
	}

	// Update global counters

	// RELEASE THE LOCK before doing any network I/O (closing)
	app.RoomsMu.Unlock()

	// NOW close the connections safely
	for _, ws := range toClose {
		if ws == nil {
			app.Logger.Warn("Skipped null pointer websocket")
			continue
		}

		app.Logger.Info("HandleCleanup called, closing websocket in toClose")

		ws.Close()
	}
}
