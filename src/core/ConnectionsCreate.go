package core

import (
	"net/http"
	"time"

	"github.com/Masterminds/semver/v3"
)

func (app *ServerData) HandleRelay(w http.ResponseWriter, r *http.Request) {

	LogRequest(app, r)

	app.Logger.Info("new CONNECTION request")

	params := r.URL.Query()
	roomID := params.Get("id")
	isHost := params.Get("host")
	requestId := params.Get("request")
	version := params.Get("version")

	// Check client version

	if version == "" {
		app.Logger.Error("Outdated client found")
		http.Error(w, "{\"ok\":false,\"message\":\"Outdated client\"}", http.StatusUpgradeRequired)
		return
	}

	relayVersionSem, err := semver.NewVersion(app.Config.RelayVersion)
	if err != nil {
		http.Error(w, "{\"ok\":false,\"message\":\"Server error\"}", http.StatusInternalServerError)
		return
	}

	v, err := semver.NewVersion(version)

	if err != nil {
		http.Error(w, "{\"ok\":false,\"message\":\"Invalid client version\"}", http.StatusBadRequest)
		return
	}

	if v.Major() > relayVersionSem.Major() {
		http.Error(w, "{\"ok\":false,\"message\":\"Version mismatch, client too new\"}", http.StatusUpgradeRequired)
		return
	}

	if v.Major() < relayVersionSem.Major() {
		http.Error(w, "{\"ok\":false,\"message\":\"Outdated client, client too old\"}", http.StatusUpgradeRequired)
		return
	}

	if roomID == "" {
		app.Logger.Error("No room ID provided")
		http.Error(w, "{\"ok\":false,\"message\":\"No room ID provided\"}", http.StatusBadRequest)
		return
	}

	app.RoomsMu.RLock()

	if app.Rooms[roomID] == nil {
		app.RoomsMu.RUnlock()
		app.Logger.Error("Room not found", "room", roomID)
		http.Error(w, "{\"ok\":false,\"message\":\"Room not found\"}", http.StatusNotFound)
		return
	}

	app.RoomsMu.RUnlock()

	conn, err := app.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)
		return
	}

	if isHost != "" {

		if requestId != "" {
			app.Logger.Info("Attempt to create parallel connection")

			app.Logger.Debug("Aquiring roomsMu Lock")
			room, exists := app.GetRoomExists(roomID)

			app.Logger.Debug("Released lock")

			if !exists {
				app.Logger.Error("Room not found")
				conn.Close()
				return
			}

			app.Logger.Debug("Aquire room RCM lock")
			room.RequestedConnectionsMapMutex.Lock()
			clientConn, exists := room.RequestedConnectionsMap[requestId]

			if !exists {
				app.Logger.Error("Request ID not found: ", "request", requestId)
				room.RequestedConnectionsMapMutex.Unlock()
				conn.Close()
				return
			}

			delete(room.RequestedConnectionsMap, requestId)
			app.Logger.Debug("Release room RCM lock")
			room.RequestedConnectionsMapMutex.Unlock()

			app.Logger.Debug("Acquire room CTHCM lock")
			room.ClientsToHostConnectionsMutex.Lock()
			hostConn, exists := room.ClientsToHostConnections[clientConn]

			if !exists {
				app.Logger.Error("Client connection not found")
				room.ClientsToHostConnectionsMutex.Unlock()
				conn.Close()
				return
			}

			if hostConn == nil {
				app.Logger.Warn("warn: Host connection is nil")
			}

			room.ClientsToHostConnections[clientConn] = conn
			app.Logger.Debug("Release room CTHCM lock")
			room.ClientsToHostConnectionsMutex.Unlock()

			app.Logger.Debug("Acquire room HTCM lock")
			room.HostToClientsConnectionsMutex.Lock()
			room.HostToClientsConnections[conn] = clientConn
			app.Logger.Debug("Release room HTCM lock")
			room.HostToClientsConnectionsMutex.Unlock()

			app.Logger.Info("Done: Registered new host connection with request ID")

			room.LastRoomFilledTime.Store(time.Now().UnixMilli())

			app.handleHostToRelayGameConnection(conn, roomID)

			return

		} else {
			app.Logger.Info("Joining as host, not client parallel connection")

			app.RoomsMu.Lock()

			if room, exists := app.Rooms[roomID]; exists && room != nil {
				if room.Host != nil {
					app.Logger.Error("Room already has a host! Disconnecting")
					app.RoomsMu.Unlock()
					conn.Close()
					return
				}
				room.Host = conn
				room.HostIP = r.RemoteAddr
				room.HasHost = true
			} else {

				app.RoomsMu.Unlock()
				app.Logger.Error("Room not found")
				conn.Close()
				return
			}

			app.Rooms[roomID].Host = conn
			app.Rooms[roomID].HostIP = r.RemoteAddr
			app.Rooms[roomID].HasHost = true

			app.Rooms[roomID].LastRoomFilledTime.Store(time.Now().UnixMilli())

			app.RoomsMu.Unlock()
		}

	}

	app.NumberOfClientsConnected.Add(1)

	if isHost != "" {
		app.Logger.Info("Joining as signal connection as host")
		app.signalRelayHandler(conn, roomID)
	} else {
		app.Logger.Info("Joining as client")
		app.clientRelayHandler(conn, roomID)
	}

	//relayForwardingLoop(conn, isHost != "", roomID)

}
