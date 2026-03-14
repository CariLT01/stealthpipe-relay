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
	clientSignal := params.Get("signal")

	// Let it connect first
	conn, err := app.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)
		return
	}

	// Check client version

	if version == "" {
		app.Logger.Error("Outdated client found")
		// http.Error(w, "{\"ok\":false,\"message\":\"Outdated client\"}", http.StatusUpgradeRequired)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.OutdatedClient)
		return
	}

	relayVersionSem, err := semver.NewVersion(app.Config.RelayVersion)
	if err != nil {
		// http.Error(w, "{\"ok\":false,\"message\":\"Server error\"}", http.StatusInternalServerError)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.InternalError)
		return
	}

	v, err := semver.NewVersion(version)

	if err != nil {
		// http.Error(w, "{\"ok\":false,\"message\":\"Invalid client version\"}", http.StatusBadRequest)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.InvalidVersion)
		return
	}

	if v.Major() > relayVersionSem.Major() {
		// http.Error(w, "{\"ok\":false,\"message\":\"Version mismatch, client too new\"}", http.StatusUpgradeRequired)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.UnsupportedClient)
		return
	}

	if v.Major() < relayVersionSem.Major() {
		// http.Error(w, "{\"ok\":false,\"message\":\"Outdated client, client too old\"}", http.StatusUpgradeRequired)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.OutdatedClient)
		return
	}

	if roomID == "" {
		app.Logger.Error("No room ID provided")
		// http.Error(w, "{\"ok\":false,\"message\":\"No room ID provided\"}", http.StatusBadRequest)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.BadRequest)
		return
	}

	app.RoomsMu.RLock()

	if app.Rooms[roomID] == nil {
		app.RoomsMu.RUnlock()
		app.Logger.Error("Room not found", "room", roomID)
		// http.Error(w, "{\"ok\":false,\"message\":\"Room not found\"}", http.StatusNotFound)
		app.CloseWebsocket(conn, WebsocketConnectionCloseReason.RoomNotFound)
		return
	}

	app.RoomsMu.RUnlock()

	if isHost != "" {

		if requestId != "" {
			app.Logger.Info("Attempt to create parallel connection")

			app.Logger.Debug("Aquiring roomsMu Lock")
			room, exists := app.GetRoomExists(roomID)

			app.Logger.Debug("Released lock")

			if !exists {
				app.Logger.Error("Room not found")
				app.CloseWebsocket(conn, WebsocketConnectionCloseReason.RoomNotFound)
				return
			}

			app.Logger.Debug("Aquire room RCM lock")
			room.RequestedConnectionsMapMutex.Lock()
			clientConn, exists := room.RequestedConnectionsMap[requestId]

			if !exists {
				app.Logger.Error("Request ID not found. Closing relay connection", "request", requestId)
				room.RequestedConnectionsMapMutex.Unlock()
				app.CloseWebsocket(conn, WebsocketConnectionCloseReason.InvalidRequestID)
				return
			}

			delete(room.RequestedConnectionsMap, requestId)
			app.Logger.Debug("Release room RCM lock")
			room.RequestedConnectionsMapMutex.Unlock()

			app.Logger.Debug("Acquire room CTHCM lock")
			room.ClientsToHostConnectionsMutex.Lock()
			hostConn, exists := room.ClientsToHostConnections[clientConn]

			if !exists {
				app.Logger.Error("Client connection not found. Closing relay connection")
				room.ClientsToHostConnectionsMutex.Unlock()
				app.CloseWebsocket(conn, WebsocketConnectionCloseReason.PairLinkFailed)
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
					app.CloseWebsocket(conn, WebsocketConnectionCloseReason.InvalidAction)
					return
				}
				room.Host = conn
				room.HostIP = r.RemoteAddr
				room.HasHost = true
			} else {

				app.RoomsMu.Unlock()
				app.Logger.Error("Room not found")
				app.CloseWebsocket(conn, WebsocketConnectionCloseReason.RoomNotFound)
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
	app.Statistics.numberOfClientsConnected.Record(app.Ctx, app.NumberOfClientsConnected.Load())

	if isHost != "" {
		app.Logger.Info("Joining as signal connection as host")
		app.HostSignalToRelayHandler(conn, roomID)
	} else {
		if clientSignal != "" {
			app.Logger.Info("Joining as signal connection as client")
			app.ClientSignalToRelayHandler(conn, roomID)
		} else {
			app.Logger.Info("Joining as client")
			app.clientRelayHandler(conn, roomID)
		}

	}

	//relayForwardingLoop(conn, isHost != "", roomID)

}
