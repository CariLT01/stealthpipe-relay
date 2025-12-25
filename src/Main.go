package main

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Room struct {
	Host              *websocket.Conn
	HostMu            sync.Mutex
	HostIP            string
	Clients           map[string]*websocket.Conn
	ClientsReverseMap map[*websocket.Conn]string
	PendingClients    []*websocket.Conn
}

var (
	rooms   = make(map[string]*Room)
	roomsMu sync.RWMutex // Protects the rooms map from concurrent access
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  65536,
	WriteBufferSize: 65536,
}

var lastTick = time.Now().UnixMilli()
var packetCounter = 0
var packetsPerSecond = 0
var numberOfClientsConnected = 0

func cleanupInternal(conn *websocket.Conn, gameId string) {
	room := rooms[gameId]
	if room == nil {
		return
	}

	if room.Host == conn {
		room.Host = nil
		for uuid, ws := range room.Clients {
			ws.Close()
			// Just delete directly, don't recurse
			delete(room.Clients, uuid)
			delete(room.ClientsReverseMap, ws)
		}
		delete(rooms, gameId)
		fmt.Println("Host and room cleaned up")
	} else {
		uuid := room.ClientsReverseMap[conn]
		delete(room.Clients, uuid)
		delete(room.ClientsReverseMap, conn)
		fmt.Println("Client cleaned up:", uuid)
	}

	numberOfClientsConnected -= 1
	if numberOfClientsConnected < 0 {
		numberOfClientsConnected = 0
	}

	if numberOfClientsConnected == 0 {
		packetsPerSecond = 0
	}
}

func handleCleanup(conn *websocket.Conn, gameId string) {

	roomsMu.Lock()
	defer roomsMu.Unlock()

	cleanupInternal(conn, gameId) // Put in internal to not get stuck in deadlock
}

func handlePacket(packetData []byte, messageType int, ws *websocket.Conn, gameId string, isHost bool) {

	// Check CLIENTUUID_ prefix

	// fmt.Println("Received packet: ", string(packetData))

	if rooms[gameId] == nil {
		// fmt.Println("Room not found")
		return
	}

	packetCounter++
	if time.Now().UnixMilli()-lastTick > 1000 {
		packetsPerSecond = packetCounter
		packetCounter = 0
		lastTick = time.Now().UnixMilli()
	}

	clientUuidPrefix := []byte("CLIENTUUID_")
	clientUuidPrefixLen := len(clientUuidPrefix)
	uuidLen := 36

	if len(packetData) >= clientUuidPrefixLen+uuidLen {
		if bytes.HasPrefix(packetData, clientUuidPrefix) {
			uuid := packetData[clientUuidPrefixLen : clientUuidPrefixLen+uuidLen]

			roomsMu.Lock()

			rooms[gameId].Clients[string(uuid)] = ws
			rooms[gameId].ClientsReverseMap[ws] = string(uuid)

			roomsMu.Unlock()
			roomsMu.RLock()

			fmt.Println("Registered client with UUID: ", string(uuid))

			// Don't forget to forward the packet

			host := rooms[gameId].Host

			if host != nil {

				rooms[gameId].HostMu.Lock()

				err := host.WriteMessage(websocket.BinaryMessage, packetData)

				rooms[gameId].HostMu.Unlock()

				if err != nil {
					fmt.Println("An error occurred while trying to forward packet: ", err.Error())
					return
				}
			}

			roomsMu.RUnlock()

			return
		}
	}

	closeConnectionPrefix := []byte("CLOSECONNECTION_")
	closeConnectionPrefixLen := len(closeConnectionPrefix)

	if len(packetData) >= closeConnectionPrefixLen+uuidLen {
		if bytes.HasPrefix(packetData, closeConnectionPrefix) {
			uuid := packetData[closeConnectionPrefixLen : closeConnectionPrefixLen+uuidLen]

			if isHost {

				// The host sent it, that means it wants to disconnect a client

				roomsMu.RLock()

				targetClient := rooms[gameId].Clients[string(uuid)]

				roomsMu.RUnlock()

				if targetClient != nil {
					targetClient.Close()
					handleCleanup(targetClient, gameId)
				} else {
					fmt.Println("Client not found when trying to close connection: ", string(uuid))
				}

				fmt.Println("Host requested to disconnect, comply with req")

				return

			} else {
				// Client will never send such a packet

			}

		}
	}

	// Any other case, forward the packet

	roomsMu.RLock()
	defer roomsMu.RUnlock()

	if isHost {
		uuidBytes := packetData[:36]
		uuidKey := string(uuidBytes)

		if rooms[gameId] == nil {
			return
		}

		targetConn, exists := rooms[gameId].Clients[uuidKey]

		if !exists {
			// fmt.Println("Connection not found: ", uuidKey)
			return
		}

		payload := packetData[36:]

		rooms[gameId].HostMu.Lock()
		err := targetConn.WriteMessage(websocket.BinaryMessage, payload)
		rooms[gameId].HostMu.Unlock()
		if err != nil {
			fmt.Println("An error occurred while trying to forward the packet: ", err.Error())
			return
		}

		// fmt.Println("Forwarded packet")
	} else {
		rooms[gameId].HostMu.Lock()
		err := rooms[gameId].Host.WriteMessage(websocket.BinaryMessage, packetData)
		rooms[gameId].HostMu.Unlock()

		if err != nil {
			fmt.Println("An error occurred while trying to forward the packet: ", err.Error())
			return
		}
		// fmt.Println("Forwarded packet as client")
	}

}

func relayForwardingLoop(conn *websocket.Conn, isHost bool, gameId string) {
	fmt.Println("Start new relay loop")
	mainBuf := make([]byte, 65536)

	for {
		messageType, reader, err := conn.NextReader()
		if err != nil {
			fmt.Println("Got error: ", err.Error())
			break
		}

		if messageType != websocket.BinaryMessage {
			fmt.Println("Warning: Message not binary, skipping")
			continue
		}

		packetBuffer := bytes.NewBuffer(mainBuf[:0])
		limitReader := io.LimitReader(reader, 2200000)

		n, err := packetBuffer.ReadFrom(limitReader)
		if err != nil {
			break
		}

		if n > 2097152 {
			fmt.Println("Packet exceeded Minecraft 2MB limit! Security Kick.")
			break
		}

		packet := packetBuffer.Bytes()

		handlePacket(packet, messageType, conn, gameId, isHost)
	}

	// Before cleaning up, send a disonnect packet to the server
	if !isHost {
		uuid := rooms[gameId].ClientsReverseMap[conn]
		if true {
			packetString := "CLOSECONNECTION_" + string(uuid)
			packet := []byte(packetString)

			host := rooms[gameId].Host

			if host != nil {

				rooms[gameId].HostMu.Lock()

				err := host.WriteMessage(websocket.BinaryMessage, packet)

				rooms[gameId].HostMu.Unlock()

				if err != nil {
					fmt.Println("An error occurred while trying to write disconnect signal: ", err.Error())
				}
			}
		}
	}

	handleCleanup(conn, gameId)
}

func handleRelay(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	roomID := params.Get("id")
	isHost := params.Get("host")

	if roomID == "" {
		http.Error(w, "{\"ok\":false,\"message\":\"No room ID provided\"}", http.StatusBadRequest)
		return
	}

	roomsMu.RLock()

	if rooms[roomID] == nil {
		roomsMu.RUnlock()
		http.Error(w, "{\"ok\":false,\"message\":\"Room not found\"}", http.StatusNotFound)
		return
	}

	roomsMu.RUnlock()

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		http.Error(w, "Failed to upgrade connection", http.StatusInternalServerError)
		return
	}

	if isHost != "" {

		roomsMu.Lock()

		rooms[roomID].Host = conn
		rooms[roomID].HostIP = r.RemoteAddr

		roomsMu.Unlock()
	}

	numberOfClientsConnected++
	relayForwardingLoop(conn, isHost != "", roomID)

}

func handleCreatePath(w http.ResponseWriter, r *http.Request) {
	gameId := -1
	attempts := 0

	roomsMu.Lock()
	defer roomsMu.Unlock()

	for {
		gameId = 100000 + rand.Intn(899999)
		if rooms[strconv.Itoa(gameId)] != nil {
			attempts++
		} else {
			break
		}
		if attempts >= 10000 {
			http.Error(w, "Failed to get a random game ID in reasonable time", http.StatusServiceUnavailable)
			return
		}
	}

	rooms[strconv.Itoa(gameId)] = &Room{
		Host:              nil,
		HostIP:            "",
		Clients:           make(map[string]*websocket.Conn),
		ClientsReverseMap: make(map[*websocket.Conn]string),
		PendingClients:    make([]*websocket.Conn, 0),
		HostMu:            sync.Mutex{},
	}

	fmt.Fprintf(w, "{\"ok\":true,\"message\":%d}", gameId)
}

func mainPathHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "StealthPipe Relay: Online")
}

func statsPathHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "{\"ok\":true,\"packetsPerSecond\":%d,\"clientsConnected\":%d}", packetsPerSecond, numberOfClientsConnected)
}

func pingPathHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "OK")
}

func main() {

	port := os.Getenv("PORT")
	if port == "" {
		port = "7860" // Default for Hugging Face or Local testing
	}

	http.HandleFunc("/", mainPathHandler)
	http.HandleFunc("/create", handleCreatePath)
	http.HandleFunc("/join", handleRelay)
	http.HandleFunc("/stats", statsPathHandler)
	http.HandleFunc("/ping", pingPathHandler)
	fmt.Println("StealthPipeRelay: Server started on :7860")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		panic(err)
	}
}
