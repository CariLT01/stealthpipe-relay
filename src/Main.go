/*
StealthPipe Relay

*/

package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

// --- CONFIGURATION ---

/*

Security features:
 - Proof of work when creating a new room, makes botting and filling all rooms extremely slow, also helps manage traffic and load
 - Bandwidth throttling prevents players from utilizing all of your server's monthly bandwidth
 - Maximum packet size prevents players from taking all of the relay's memory

*/

// Bandwidth throttling - protects server bandwidth usage
var packetThrottlingLimitBaseline = int64(0.25 * 1024 * 1024) // A minimum of 0.25 MB with 0 clients, enough for keep-alive and handshake
var packetThrottlingMs = 100                                  // 100ms per packet if throttled
var packetThrottlingLimitBonus = int64(1.25 * 1024 * 1024)    // Extra 1.25 MB per client connected (sqrt, diminishing rewards)
var packetThrottlingLimitMaximum = int64(4 * 1024 * 1024)     // Never give more than 4 MB/s bandwidth
var packetThrottlingAllowance = int64(20 * 1024 * 1024)       // Maximum allow 20 MB/s once
var packetThrottlingDisconnectDelayClient = 20_000            // Sustained 20 second use beyond allowed bandwidth, disconnect client
var packetThrottlingDisconnectDelayHost = 60_000              // Sustained 60 second use beyond allowed bandwidth, disconnect host

// Packet size - prevents memory attacks
var packetMaxSize = 2.1 * 1024 * 1024 // 2.1 MB before instantly disconnecting

// Proof of work difficulty - prevents brute force / creating thousands of rooms
var difficulty6Threshold = 20   // Trigger difficulty 6 if exeeding 20 creation requests/s
var difficulty7Threshold = 100  // Trigger difficulty 7 if exceeding 100 creation requests/s
var difficultyCooldown = 30_000 // Wait 30 seconds before dropping the difficulty
// 67

// Cleanup
var roomEmptyCleanupDelay = int64(60_000)  // Cleanup an empty room if no host is connected after a full minute
var roomIdleNoClientDelay = int64(300_000) // Cleanup an empty room if there were no clients for 5 minutes

// Other
var receiveIdleDelay = 50 // 50 ms idle delay between each packet when no clients are connected to the room

// Stability and instance health
var terminateWhenUnhealthy = true // Automatically terminate this instance to force a restart

// --------------------------------------------------------------------------------------- //

type Room struct {
	Host                          *websocket.Conn
	HostMu                        *sync.Mutex
	HostIP                        string
	LastRoomFilledTime            atomic.Int64
	ClientsToHostConnections      map[*websocket.Conn]*websocket.Conn
	HostToClientsConnections      map[*websocket.Conn]*websocket.Conn
	RequestedConnectionsMap       map[string]*websocket.Conn
	RequestedConnectionsMapMutex  *sync.RWMutex
	ClientsToHostConnectionsMutex *sync.RWMutex
	HostToClientsConnectionsMutex *sync.RWMutex
	CreatedTime                   int64
	HasHost                       bool
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
var packetCounter = atomic.Int64{}
var packetsPerSecond = atomic.Int64{}
var numberOfClientsConnected = atomic.Int64{}

var (
	// HandshakeCounter tracks requests in the current second
	handshakeCounter uint64
	// CurrentDifficulty is what we tell new clients to use
	currentDifficulty int32     = 5
	lastUpped         time.Time // Tracks when we last moved difficulty UP
)

var (
	// usedSalts stores salts that have already been verified
	usedSalts = make(map[string]int64)
	// mu protects the map from concurrent access
	usedSaltsMutex sync.Mutex
)

var (
	isHealthy atomic.Bool
)

func healthCheckGoroutines() bool {
	count := runtime.NumGoroutine()
	if count > 5000 { // Adjust based on your expected max players
		fmt.Printf("UNHEALTHY: Goroutine leak detected (%d)\n", count)
		return false
	}
	return true
}

func deadlockWatchdog() {
	ticker := time.NewTicker(5 * time.Second)
	for range ticker.C {
		// Create a channel to wait for the lock
		lockAcquired := make(chan bool, 1)

		go func() {
			roomsMu.RLock()
			// Do nothing
			roomsMu.RUnlock()
			lockAcquired <- true
		}()

		select {
		case <-lockAcquired:
			isHealthy.Store(true)
		case <-time.After(10 * time.Second): // If we can't RLock in 10s, we are deadlocked

			isHealthy.Store(false)

			fmt.Println("\n========================================")
			fmt.Println("CRITICAL: DEADLOCK DETECTED ON roomsMu")
			fmt.Println("Dumping all goroutine stacks:")
			fmt.Println("========================================")

			// Create a buffer large enough for many goroutines
			buf := make([]byte, 1024*1024)
			n := runtime.Stack(buf, true) // 'true' gets stacks for ALL goroutines
			fmt.Printf("%s\n", buf[:n])

			fmt.Println("========================================")

		}

		if !healthCheckGoroutines() {
			fmt.Println("UNHEALTHY: Goroutine leak detected")
			os.Exit(1)
		}

		if !isHealthy.Load() {
			if terminateWhenUnhealthy {
				fmt.Println("Terminating instance to force a restart")
				os.Exit(1)
			}
		}
	}
}

func monitorTraffic() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		rps := atomic.SwapUint64(&handshakeCounter, 0)

		// 1. Determine what the difficulty SHOULD be based on RPS
		var targetDiff int32
		switch {
		case rps > uint64(difficulty7Threshold):
			targetDiff = 7 // Panic
		case rps > uint64(difficulty6Threshold):
			targetDiff = 6 // Busy
		default:
			targetDiff = 5 // Normal
		}

		// 2. Apply Cooldown Logic
		current := atomic.LoadInt32(&currentDifficulty)

		if targetDiff > current {
			// Moving UP: Do it immediately and reset the timer
			atomic.StoreInt32(&currentDifficulty, targetDiff)
			lastUpped = time.Now()
		} else if targetDiff < current {
			// Moving DOWN: Only allow if enough time has passed
			if time.Since(lastUpped) > time.Duration(difficultyCooldown) {
				atomic.StoreInt32(&currentDifficulty, targetDiff)
			}
		}
	}
}

func cleanUnusedRooms() {
	// 15 seconds is a good frequency for a "Pre-Host" check
	ticker := time.NewTicker(15 * time.Second)

	for range ticker.C {
		var staleRooms []string
		now := time.Now().UnixMilli()

		// Phase 1: Quick Scan with Read Lock
		roomsMu.RLock()
		for code, room := range rooms {
			// Logic: No host joined AND the room creation was too long ago
			if !room.HasHost && (now-room.CreatedTime) > roomEmptyCleanupDelay {
				staleRooms = append(staleRooms, code)
			}
		}
		roomsMu.RUnlock()

		// Phase 2: Targeted Deletion with Write Lock
		if len(staleRooms) > 0 {
			roomsMu.Lock()
			for _, code := range staleRooms {
				// Double-check existence and status (it might have changed during lock switch)
				if room, exists := rooms[code]; exists && !room.HasHost {
					delete(rooms, code)
					fmt.Printf("Pruned ghost room: %s\n", code)
				}
			}
			roomsMu.Unlock()
		}
	}
}

func cleanIdleRooms() {
	ticker := time.NewTicker(35 * time.Second)
	for range ticker.C {
		var toClose []*websocket.Conn
		var codesToDelete []string
		now := time.Now().UnixMilli()

		roomsMu.RLock()
		for code, room := range rooms {
			if now-room.LastRoomFilledTime.Load() > roomIdleNoClientDelay && len(room.ClientsToHostConnections) == 0 {
				codesToDelete = append(codesToDelete, code)
				if room.Host != nil {
					toClose = append(toClose, room.Host)
				}
			}
		}
		roomsMu.RUnlock()

		if len(codesToDelete) > 0 {
			roomsMu.Lock()
			for _, code := range codesToDelete {
				delete(rooms, code)
			}
			roomsMu.Unlock()

			// Close connections AFTER unlocking roomsMu
			for _, conn := range toClose {
				conn.Close()
				fmt.Println("Closed idle connection")
			}
		}
	}
}

func isReplay(salt string, expiry int64) bool {
	usedSaltsMutex.Lock()
	defer usedSaltsMutex.Unlock()

	// 1. Check if it's already there
	if _, exists := usedSalts[salt]; exists {
		return true
	}

	// 2. If not, add it with the expiry time from the JWT
	usedSalts[salt] = expiry
	return false
}

func startCleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute) // Check every minute
	go func() {
		for range ticker.C {
			now := time.Now().Unix()
			usedSaltsMutex.Lock()
			for salt, expiry := range usedSalts {
				if now > expiry {
					delete(usedSalts, salt)
				}
			}
			usedSaltsMutex.Unlock()
		}
	}()
}

// HANDLE CLEANUP NEEDS THE roomsMu LOCK! ALWAYS UNLOCK BEFORE CLEANING UP. ALSO, ALWAYS UNLOCK WHEN CALLING .CLOSE() ON A WEBSOCKET THAT HAS A RELAY LOOP ALREADY RUNNING, WHICH WILL TRIGGER HANDLECLEANUP, WHICH WILL NEED THE LOCK.
// IF NOT UNLOCKED BEFORE, A DEADLOCK WILL HAPPEN
func handleCleanup(conn *websocket.Conn, gameId string) {

	fmt.Println("Handle cleanup called on connection")

	// 1. Acquire the lock ONLY to modify the map
	roomsMu.Lock()

	room, exists := rooms[gameId]
	if !exists || room == nil {
		roomsMu.Unlock()
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

		delete(rooms, gameId)
		fmt.Println("Host left, room deleted")
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
			toClose = append(toClose, hostConnection)
		}
		if exists2 {
			toClose = append(toClose, clientConnection)
		}

		toClose = append(toClose, conn) // Add the client to the close list
		// fmt.Println("Client left:", uuid)
	}

	// Update global counters

	// RELEASE THE LOCK before doing any network I/O (closing)
	roomsMu.Unlock()

	// NOW close the connections safely
	for _, ws := range toClose {
		if ws == nil {
			fmt.Println("Skipped null pointer websocket")
			continue
		}
		numberOfClientsConnected.Add(-1)
		ws.Close()
	}
}

func getCurrentBandwidth(gameId string) int64 {
	roomsMu.RLock()
	numberOfClients := len(rooms[gameId].ClientsToHostConnections)
	roomsMu.RUnlock()

	bandwidthCalculated := packetThrottlingLimitBaseline + int64(packetThrottlingLimitBonus)*int64(math.Sqrt(float64(numberOfClients)))

	return min(bandwidthCalculated, packetThrottlingLimitMaximum)

}

func handleSignalRelayPacket(message []byte, conn *websocket.Conn) {

}

func signalRelayHandler(conn *websocket.Conn, gameId string) {

	fmt.Println("New SIGNAL ws")

	roomsMu.RLock()
	room, exists := rooms[gameId]
	roomsMu.RUnlock()

	if !exists || room == nil {
		return
	}

	defer handleCleanup(conn, gameId)

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
			fmt.Println("Failed to read buffer: ", err.Error())
			break
		}

		if n > int64(packetMaxSize) {
			fmt.Println("Packet exceeded Minecraft 2MB limit! Security Kick.")
			break
		}

		packet := packetBuffer.Bytes()

		handleSignalRelayPacket(packet, conn)
	}

}

func handleRequestCleanup(request string, gameId string) {

	roomsMu.RLock()
	room, exists := rooms[gameId]
	roomsMu.RUnlock()

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

func waitForHostConnectionAndReturnIt(request string, clientConn *websocket.Conn, gameId string) *websocket.Conn {

	start := time.Now()

	defer handleRequestCleanup(request, gameId)

	for {

		roomsMu.RLock()
		room, exists := rooms[gameId]
		roomsMu.RUnlock()

		if !exists {
			fmt.Println("Room ID does not exist")
			return nil
		}

		if room.Host == nil {
			fmt.Println("Room has no host")
			return nil
		}

		room.ClientsToHostConnectionsMutex.RLock()
		if hostConn, exists := room.ClientsToHostConnections[clientConn]; exists {

			if !exists {
				room.ClientsToHostConnectionsMutex.RUnlock()
				fmt.Println("Host -> Relay connection not found")
				continue
			}

			if hostConn == nil {
				room.ClientsToHostConnectionsMutex.RUnlock()
				fmt.Println("Host -> Relay connection is nil")
				continue
			}

			room.ClientsToHostConnectionsMutex.RUnlock()
			return hostConn
		}
		room.ClientsToHostConnectionsMutex.RUnlock()

		time.Sleep(100 * time.Millisecond)

		// Cancel if it's taking too long
		if time.Since(start) > 1*time.Minute {
			fmt.Println("Timed out waiting for host connection")
			return nil
		}
	}
}

func clientRelayHandler(conn *websocket.Conn, gameId string) {

	fmt.Println("new CLIENT ws")

	defer handleCleanup(conn, gameId)

	mainBuf := make([]byte, 65536)

	roomsMu.RLock()
	room, exists := rooms[gameId]
	roomsMu.RUnlock()

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

	// Send signal

	n, err := rand.Int(rand.Reader, big.NewInt(9999999))

	if err != nil {
		fmt.Println("Error occurred while generating random number")

		return
	}

	message := "REQUESTCONNECTION_" + strconv.Itoa(int(n.Int64())) + gameId
	messageBytes := []byte(message)

	room.RequestedConnectionsMapMutex.Lock()
	fmt.Println("Saved original message: " + message)
	room.RequestedConnectionsMap[message] = conn
	room.RequestedConnectionsMapMutex.Unlock()

	room.ClientsToHostConnectionsMutex.Lock()
	room.ClientsToHostConnections[conn] = nil
	room.ClientsToHostConnectionsMutex.Unlock()

	hostMu.Lock()
	err = host.WriteMessage(websocket.BinaryMessage, messageBytes)
	hostMu.Unlock()

	if err != nil {
		fmt.Println("Error occurred while trying to write signal: ", err.Error())
		return
	}

	fmt.Println("Waiting for signal response...")

	hostConn := waitForHostConnectionAndReturnIt(message, conn, gameId)

	if hostConn == nil {
		fmt.Println("Failed to get host connection")
		return
	}

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

		if n > int64(packetMaxSize) {
			fmt.Println("Packet exceeded Minecraft 2MB limit! Security Kick.")
			break
		}

		packet := packetBuffer.Bytes()

		// Forward

		// fmt.Println("Forwarding " + strconv.Itoa(len(packet)) + " bytes to Server")

		err = hostConn.WriteMessage(websocket.BinaryMessage, packet)

		if err != nil {
			fmt.Println("Got error: ", err.Error())
			break
		}

	}

}

func handleHostToRelayGameConnection(conn *websocket.Conn, gameId string) {

	fmt.Println("new SERVER ws")

	defer handleCleanup(conn, gameId)

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
			fmt.Println("Failed to read buffer: ", err.Error())
			break
		}

		if n > int64(packetMaxSize) {
			fmt.Println("Packet exceeded Minecraft 2MB limit! Security Kick.")
			break
		}

		packet := packetBuffer.Bytes()

		// fmt.Println("Forwarding " + strconv.Itoa(len(packet)) + " bytes to client")

		// Forward

		roomsMu.RLock()
		room, exists := rooms[gameId]
		roomsMu.RUnlock()

		if !exists {
			fmt.Println("Room no longer exists")
			return
		}

		room.ClientsToHostConnectionsMutex.RLock()
		connection, exists := room.HostToClientsConnections[conn]
		room.ClientsToHostConnectionsMutex.RUnlock()

		if !exists {
			fmt.Println("Connection not found")
			return
		}

		// No need to lock, only this thread will write to the connection
		connection.WriteMessage(websocket.BinaryMessage, packet)

	}

}

func handleRelay(w http.ResponseWriter, r *http.Request) {

	fmt.Println("new CONNECTION request")

	params := r.URL.Query()
	roomID := params.Get("id")
	isHost := params.Get("host")
	requestId := params.Get("request")

	if roomID == "" {
		fmt.Println("No room ID provided")
		http.Error(w, "{\"ok\":false,\"message\":\"No room ID provided\"}", http.StatusBadRequest)
		return
	}

	roomsMu.RLock()

	if rooms[roomID] == nil {
		roomsMu.RUnlock()
		fmt.Println("Room not found")
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

		if requestId != "" {
			fmt.Println("Attempt to create parallel connection")

			fmt.Println("Aquiring roomsMu Lock")
			roomsMu.RLock()
			room, exists := rooms[roomID]
			roomsMu.RUnlock()

			fmt.Println("Released lock")

			if !exists {
				fmt.Println("Room not found")
				conn.Close()
				return
			}

			fmt.Println("Aquire room RCM lock")
			room.RequestedConnectionsMapMutex.Lock()
			clientConn, exists := room.RequestedConnectionsMap[requestId]

			if !exists {
				fmt.Println("Request ID not found: " + requestId)
				room.RequestedConnectionsMapMutex.Unlock()
				conn.Close()
				return
			}

			delete(room.RequestedConnectionsMap, requestId)
			fmt.Println("Release room RCM lock")
			room.RequestedConnectionsMapMutex.Unlock()

			fmt.Println("Acquire room CTHCM lock")
			room.ClientsToHostConnectionsMutex.Lock()
			hostConn, exists := room.ClientsToHostConnections[clientConn]

			if !exists {
				fmt.Println("Client connection not found")
				room.ClientsToHostConnectionsMutex.Unlock()
				conn.Close()
				return
			}

			if hostConn == nil {
				fmt.Println("warn: Host connection is nil")
			}

			room.ClientsToHostConnections[clientConn] = conn
			fmt.Println("Release room CTHCM lock")
			room.ClientsToHostConnectionsMutex.Unlock()

			fmt.Println("Acquire room HTCM lock")
			room.HostToClientsConnectionsMutex.Lock()
			room.HostToClientsConnections[conn] = clientConn
			fmt.Println("Release room HTCM lock")
			room.HostToClientsConnectionsMutex.Unlock()

			fmt.Println("Done: Registered new host connection with request ID")

			room.LastRoomFilledTime.Store(time.Now().UnixMilli())

			handleHostToRelayGameConnection(conn, roomID)

			return

		} else {
			fmt.Println("Joining as host, not client parallel connection")

			roomsMu.Lock()

			if room, exists := rooms[roomID]; exists && room != nil {
				if room.Host != nil {
					fmt.Println("Room already has a host! Disconnecting")
					roomsMu.Unlock()
					conn.Close()
					return
				}
				room.Host = conn
				room.HostIP = r.RemoteAddr
				room.HasHost = true
			} else {

				roomsMu.Unlock()
				fmt.Println("Room not found")
				conn.Close()
				return
			}

			rooms[roomID].Host = conn
			rooms[roomID].HostIP = r.RemoteAddr
			rooms[roomID].HasHost = true

			rooms[roomID].LastRoomFilledTime.Store(time.Now().UnixMilli())

			roomsMu.Unlock()
		}

	}

	numberOfClientsConnected.Add(1)

	if isHost != "" {
		fmt.Println("Joining as signal connection as host")
		signalRelayHandler(conn, roomID)
	} else {
		fmt.Println("Joining as client")
		clientRelayHandler(conn, roomID)
	}

	//relayForwardingLoop(conn, isHost != "", roomID)

}

func generateRandomId() string {

	n, _ := rand.Int(rand.Reader, big.NewInt(1000000))

	// Format with leading zeros to ensure it is always 6 digits
	str := fmt.Sprintf("%06d", n)

	return str

}

type MyClaims struct {
	Salt                 string `json:"salt"`
	Diff                 int    `json:"diff"`
	jwt.RegisteredClaims        // This adds 'exp', 'iat', etc.
}

func verifyAndGetPayload(tokenString string, secret []byte) (*MyClaims, error) {
	// 1. Parse the token
	token, err := jwt.ParseWithClaims(tokenString, &MyClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the algorithm is what you expect
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return secret, nil
	})

	// Check for errors (like expired or tampered tokens)
	if err != nil {
		return nil, err
	}

	// Extract the claims and check validity
	if claims, ok := token.Claims.(*MyClaims); ok && token.Valid {
		return claims, nil
	}

	return nil, fmt.Errorf("invalid token")
}

func isProofOfWorkValid(token string, nonce int64) bool {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	claims, err := verifyAndGetPayload(token, secretKey)

	if err != nil {
		fmt.Println("Invalid proof of work token: ", err.Error())
		return false
	}

	if isReplay(claims.Salt, claims.RegisteredClaims.ExpiresAt.Time.Unix()) {
		fmt.Println("Invalid proof of work token: replay attack")
		return false
	}

	hashedInt := fmt.Sprintf("%s%d", claims.Salt, nonce)

	hash := sha256.Sum256([]byte(hashedInt))
	hashString := hex.EncodeToString(hash[:])

	prefix := strings.Repeat("0", claims.Diff)

	isValid := strings.HasPrefix(hashString, prefix)

	if !isValid {
		fmt.Println("Proof of work invalid: hash does not start with prefix")
	}

	return isValid

}

func handleCreatePath(w http.ResponseWriter, r *http.Request) {
	gameId := ""
	attempts := 0

	params := r.URL.Query()
	token := params.Get("token")
	nonce := params.Get("nonce")

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

	workValid := isProofOfWorkValid(token, int64(nonceInt))

	if !workValid {
		fmt.Println("Rejected proof of work token")
		http.Error(w, "{\"ok\":false,\"message\":\"Invalid proof of work\"}", http.StatusBadRequest)
		return
	}

	atomic.AddUint64(&handshakeCounter, 1)

	roomsMu.Lock()
	defer roomsMu.Unlock()

	for {
		gameId = generateRandomId()
		if rooms[gameId] != nil {
			attempts++
		} else {
			break
		}
		if attempts >= 10000 {
			http.Error(w, "Failed to get a random game ID in reasonable time", http.StatusServiceUnavailable)
			return
		}
	}

	rooms[gameId] = &Room{
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
	}

	rooms[gameId].LastRoomFilledTime.Store(time.Now().UnixMilli())

	fmt.Fprintf(w, "{\"ok\":true,\"message\":%s}", gameId)
}

func mainPathHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Service is Online")
}

func statsPathHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "{\"ok\":true,\"packetsPerSecond\":%d,\"clientsConnected\":%d}", packetsPerSecond.Load(), numberOfClientsConnected.Load())
}

func pingPathHandler(w http.ResponseWriter, r *http.Request) {
	if !isHealthy.Load() {
		// Broken, kill me
		http.Error(w, "Instance unhealthy", http.StatusServiceUnavailable)
		return
	}

	// OK
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func generateProofOfWork() (string, string) {
	secretKey := []byte(os.Getenv("SECRET_KEY"))

	timeSalt := time.Now().UnixMilli()
	n, err := rand.Int(rand.Reader, big.NewInt(100000))
	if err != nil {
		panic(err)
	}

	salt := fmt.Sprintf("%d-%d", timeSalt, n.Int64())

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"salt": salt,
		"diff": currentDifficulty,
		"exp":  jwt.NewNumericDate(time.Now().Add(time.Minute * 5)),
	})

	signedToken, err := token.SignedString(secretKey)

	if err != nil {
		panic(err)
	}

	return signedToken, salt

}

func handleProofOfWorkEndpoint(w http.ResponseWriter, r *http.Request) {
	proofOfWork, salt := generateProofOfWork()

	fmt.Fprintf(w, "{\"ok\":true,\"token\":\"%s\",\"salt\":\"%s\"}", proofOfWork, salt)
}

func main() {

	port := os.Getenv("PORT")
	if port == "" {
		port = "7860" // Default for Hugging Face or Local testing
	}

	startCleanupLoop()
	go monitorTraffic()
	go cleanUnusedRooms()
	go cleanIdleRooms()

	isHealthy.Store(true)

	go deadlockWatchdog()

	http.HandleFunc("/", mainPathHandler)
	http.HandleFunc("/create", handleCreatePath)
	http.HandleFunc("/join", handleRelay)
	http.HandleFunc("/stats", statsPathHandler)
	http.HandleFunc("/pow", handleProofOfWorkEndpoint)
	http.HandleFunc("/ping", pingPathHandler)
	fmt.Println("Service: Server started")

	/*go func() {
		fmt.Println("Triggered test code")
		// TEST CODE
		roomsMu.Lock()
		roomsMu.Lock()
	}()*/

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		panic(err)
	}

}
