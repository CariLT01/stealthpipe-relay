package core

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/time/rate"
)

func (app *ServerData) HandleCreatePath(w http.ResponseWriter, r *http.Request) {

	LogRequest(app, r)
	gameId := ""
	attempts := 0

	params := r.URL.Query()
	token := params.Get("token")
	nonce := params.Get("nonce")
	existingCodeToken := params.Get("reuseToken")

	if token == "" {
		http.Error(w, "{\"ok\":false,\"message\":\"No token provided\"}", http.StatusBadRequest)
		return
	}

	if nonce == "" {
		http.Error(w, "{\"ok\":false,\"message\":\"No nonce provided\"}", http.StatusBadRequest)
		return
	}

	nonceInt, err := strconv.ParseInt(nonce, 10, 64)

	if err != nil {
		http.Error(w, "{\"ok\":false,\"message\":\"Invalid nonce provided\"}", http.StatusBadRequest)
		return
	}

	workValid := IsProofOfWorkValid(app, token, int64(nonceInt))

	if !workValid {
		app.Logger.Info("Rejected proof of work token")
		http.Error(w, "{\"ok\":false,\"message\":\"Invalid proof of work\"}", http.StatusBadRequest)
		return
	}

	atomic.AddUint64(&app.HandshakeCounter, 1)

	app.RoomsMu.Lock()
	defer app.RoomsMu.Unlock()

	if existingCodeToken == "" {

		/*
			random feature: 1/15 chance to generate 676767 code
		*/

		foundCode := false

		if rand.Float32() < 0.067 {
			gameId = "676767"
			if app.Rooms[gameId] == nil {
				// code is available for use
				foundCode = true
			}
		}

		if !foundCode {
			for {
				gameId = generateRandomId()
				if app.Rooms[gameId] != nil {
					attempts++
				} else {
					break
				}
				if attempts >= 10000 {
					http.Error(w, "Failed to get a random game ID in reasonable time", http.StatusServiceUnavailable)
					return
				}
			}
		}

	} else {
		existingCode := IsReuseTokenValid(app, existingCodeToken)

		if existingCode == "" {
			http.Error(w, "Reuse token is not valid", http.StatusUnauthorized)
			return
		}

		if existingCode == "676767" {
			http.Error(w, "Aww man... this code can't be reused :( Please generate a new room code.", http.StatusUnauthorized)
			return
		}

		gameId = existingCode
		if oldRoom, exists := app.Rooms[gameId]; exists {

			app.Logger.Info("room code used, client presented valid reuse token")
			if app.Rooms[gameId].Host != nil {
				app.Logger.Info("old client detected, evicting. new client presented a valid reuse token, and chance of collision is too thin.")
				app.RoomsMu.Unlock() // unlock for cleanup
				oldRoom.Host.Close() // close

				// wait go scheduler
				time.Sleep(50 * time.Millisecond) // give time to go scheduler

				app.RoomsMu.Lock() // acquire lock again
			}

			// double-check delete
			delete(app.Rooms, gameId) // we must delete, since we have the lock
		}

		time.Sleep(5 * time.Second) // ensure full deletion

		app.Logger.Info("Successfully reused a room code using a reuse token")
	}

	app.Rooms[gameId] = &Room{
		Host:                          nil,
		HostIP:                        "",
		ClientsToHostConnections:      make(map[*websocket.Conn]*websocket.Conn),
		HostToClientsConnections:      make(map[*websocket.Conn]*websocket.Conn),
		ClientsToHostConnectionsMutex: &sync.RWMutex{},
		HostToClientsConnectionsMutex: &sync.RWMutex{},
		RequestedConnectionsMap:       make(map[string]*websocket.Conn),
		RequestedConnectionsMapMutex:  &sync.RWMutex{},
		HostMu:                        &sync.Mutex{},
		CreatedTime:                   time.Now().UnixMilli(),
		HasHost:                       false,
		LastRoomFilledTime:            atomic.Int64{},
		HostOutboundLimiter:           rate.NewLimiter(rate.Limit(app.Config.PacketThrottlingOutboundHost), app.Config.PacketThrottlingBurstOutbound),
		ClientsLimiter:                rate.NewLimiter(rate.Limit(app.Config.PacketThrottlingInboundHost), app.Config.PacketThrottlingBurstInbound),
	}

	app.Rooms[gameId].LastRoomFilledTime.Store(time.Now().UnixMilli())

	reuseToken := ""
	if existingCodeToken == "" {
		reuseToken = GenerateReuseToken(app, gameId)
	}

	fmt.Fprintf(w, "{\"ok\":true,\"message\":\"%s\",\"reuseToken\":\"%s\"}", gameId, reuseToken)
}
