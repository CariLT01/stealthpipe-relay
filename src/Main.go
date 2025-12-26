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
	Host               *websocket.Conn
	HostMu             sync.Mutex
	HostIP             string
	LastRoomFilledTime atomic.Int64
	Clients            map[string]*websocket.Conn
	ClientsReverseMap  map[*websocket.Conn]string
	ClientsMutexes     map[*websocket.Conn]*sync.Mutex
	PendingClients     []*websocket.Conn
	CreatedTime        int64
	HasHost            bool
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
			fmt.Println("CRITICAL: Deadlock detected on roomsMu!")
			isHealthy.Store(false)

			// OPTIONAL: Dump goroutines to logs to see WHERE the deadlock is
			// pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)

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
			if now-room.LastRoomFilledTime.Load() > roomIdleNoClientDelay {
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
func handleCleanup(conn *websocket.Conn, gameId string) {
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
		for _, ws := range room.Clients {
			toClose = append(toClose, ws)
		}
		delete(rooms, gameId)
		fmt.Println("Host left, room deleted")
	} else {
		// If client leaves, just remove them from the map
		uuid := room.ClientsReverseMap[conn]
		delete(room.Clients, uuid)
		delete(room.ClientsReverseMap, conn)
		delete(room.ClientsMutexes, conn)
		toClose = append(toClose, conn) // Add the client to the close list
		fmt.Println("Client left:", uuid)
	}

	// Update global counters

	// RELEASE THE LOCK before doing any network I/O (closing)
	roomsMu.Unlock()

	// NOW close the connections safely
	for _, ws := range toClose {
		numberOfClientsConnected.Add(-1)
		ws.Close()
	}
}

func handlePacket(packetData []byte, messageType int, ws *websocket.Conn, gameId string, isHost bool, allowanceLeftInBucket *int64) {

	if messageType != websocket.BinaryMessage {
		return
	}

	// Check CLIENTUUID_ prefix

	// fmt.Println("Received packet: ", string(packetData))

	roomsMu.RLock()
	room, exists := rooms[gameId]
	roomsMu.RUnlock()
	if !exists || room == nil {
		// fmt.Println("Room not found")
		return
	}

	packetCounter.Add(1)
	if time.Now().UnixMilli()-lastTick > 1000 {
		packetsPerSecond.Store(packetCounter.Load())
		packetCounter.Store(0)
		lastTick = time.Now().UnixMilli()
	}

	clientUuidPrefix := []byte("CLIENTUUID_")
	clientUuidPrefixLen := len(clientUuidPrefix)
	uuidLen := 36

	if len(packetData) >= clientUuidPrefixLen+uuidLen {
		if bytes.HasPrefix(packetData, clientUuidPrefix) {
			uuid := packetData[clientUuidPrefixLen : clientUuidPrefixLen+uuidLen]

			roomsMu.RLock()
			room, exists := rooms[gameId]

			if !exists || room == nil {
				// fmt.Println("Room not found")
				roomsMu.RUnlock()
				return
			}

			client, clientExists := room.Clients[string(uuid)]
			if clientExists || client != nil {
				roomsMu.RUnlock()
				ws.Close()
				return
			}

			roomsMu.RUnlock()
			roomsMu.Lock()

			// Another check....
			if _, stillExists := room.Clients[string(uuid)]; stillExists {
				roomsMu.Unlock()
				ws.Close()
				return
			}

			room.Clients[string(uuid)] = ws
			room.ClientsReverseMap[ws] = string(uuid)
			room.ClientsMutexes[ws] = &sync.Mutex{}

			roomsMu.Unlock()
			fmt.Println("Registered client with UUID: ", string(uuid))

			// Don't forget to forward the packet

			host := room.Host

			if host != nil {

				roomsMu.RLock()
				room, exists := rooms[gameId]
				roomsMu.RUnlock()
				if !exists || room == nil {
					// fmt.Println("Room not found")
					return
				}

				room.HostMu.Lock()

				err := host.WriteMessage(websocket.BinaryMessage, packetData)

				room.HostMu.Unlock()

				if err != nil {
					fmt.Println("An error occurred while trying to forward packet: ", err.Error())
					return
				}
			}

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
				room, exists := rooms[gameId]

				if !exists || room == nil {
					// fmt.Println("Room not found")
					roomsMu.RUnlock()
					return
				}

				targetClient := room.Clients[string(uuid)]

				roomsMu.RUnlock()

				if targetClient != nil {
					//room.ClientsMutexes[targetClient].Lock()
					targetClient.Close()
					//room.ClientsMutexes[targetClient].Unlock()
					//handleCleanup(targetClient, gameId)
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

	payloadLength := len(packetData)

	if isHost {
		payloadLength = payloadLength - 36
	}

	*allowanceLeftInBucket -= int64(payloadLength)

	// Forwarding logic

	if isHost {
		uuidBytes := packetData[:36]
		uuidKey := string(uuidBytes)
		roomsMu.RLock()
		room, exists := rooms[gameId]

		if !exists || room == nil {
			roomsMu.RUnlock()
			return
		}

		targetConn, exists := rooms[gameId].Clients[uuidKey]
		roomsMu.RUnlock()

		if !exists {
			// fmt.Println("Connection not found: ", uuidKey)
			return
		}

		payload := packetData[36:]

		mu, exists := room.ClientsMutexes[targetConn]
		if !exists {
			return
		}

		mu.Lock()
		err := targetConn.WriteMessage(websocket.BinaryMessage, payload)
		mu.Unlock()
		if err != nil {
			fmt.Println("An error occurred while trying to forward the packet: ", err.Error())
			return
		}

		// fmt.Println("Forwarded packet")
	} else {

		roomsMu.RLock()
		room, exists := rooms[gameId]

		if !exists || room == nil {
			roomsMu.RUnlock()
			return
		}
		if rooms[gameId].Host == nil {
			fmt.Println("Room has no host, cancelled forwarding")
			return
		}
		roomsMu.RUnlock()
		room.HostMu.Lock()

		err := rooms[gameId].Host.WriteMessage(websocket.BinaryMessage, packetData)
		room.HostMu.Unlock()

		if err != nil {
			fmt.Println("An error occurred while trying to forward the packet: ", err.Error())
			return
		}
		// fmt.Println("Forwarded packet as client")
	}

}

func getCurrentBandwidth(gameId string) int64 {
	roomsMu.RLock()
	numberOfClients := len(rooms[gameId].Clients)
	roomsMu.RUnlock()

	bandwidthCalculated := packetThrottlingLimitBaseline + int64(packetThrottlingLimitBonus)*int64(math.Sqrt(float64(numberOfClients)))

	return min(bandwidthCalculated, packetThrottlingLimitMaximum)

}

func relayForwardingLoop(conn *websocket.Conn, isHost bool, gameId string) {
	fmt.Println("Start new relay loop")
	mainBuf := make([]byte, 65536)

	lastFillTime := time.Now()
	throttleActive := false
	throttleStartedAt := time.Now()
	currentBurstRemaining := packetThrottlingAllowance

	lastCountCheck := time.Now()
	cachedClientCount := 0

	for {

		// Idle delay logic, only check every 2 second to prevent frequent locking
		if isHost && time.Since(lastCountCheck) > 2*time.Second {
			roomsMu.RLock()
			if room, exists := rooms[gameId]; exists {
				cachedClientCount = len(room.Clients)
			}

			lastCountCheck = time.Now()

			roomsMu.RUnlock()

			if cachedClientCount > 0 {
				roomsMu.RLock()
				if room, exists := rooms[gameId]; exists { // Re-verify inside the lock
					room.LastRoomFilledTime.Store(time.Now().UnixMilli())
				}

				roomsMu.RUnlock()
			}

		}
		if isHost && cachedClientCount == 0 {
			time.Sleep(time.Duration(receiveIdleDelay) * time.Millisecond) // Slight pause for each packet if no one is in the room
		}

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

		// --- Client Bandwidth Throttling - prevent abuse

		now := time.Now()
		elapsed := now.Sub(lastFillTime).Seconds()
		lastFillTime = now

		limitPerSecond := float64(getCurrentBandwidth(gameId))
		currentBurstRemaining += int64(elapsed * limitPerSecond)

		if currentBurstRemaining > packetThrottlingAllowance {
			currentBurstRemaining = packetThrottlingAllowance
		}

		currentBurstRemaining -= int64(len(packet))

		// --- Handle packet and subtract from burst left

		handlePacket(packet, messageType, conn, gameId, isHost, &currentBurstRemaining)

		if currentBurstRemaining < 0 {
			// Force them to wait for the bucket to refill
			time.Sleep(time.Duration(packetThrottlingMs) * time.Millisecond)

			// Safety: Don't let the debt go to negative infinity
			if currentBurstRemaining < int64(-limitPerSecond) {
				currentBurstRemaining = int64(-limitPerSecond)
			}

			if !throttleActive {
				throttleActive = true
				throttleStartedAt = time.Now()
			}

			limitDuration := time.Duration(packetThrottlingDisconnectDelayClient) * time.Millisecond
			if isHost {
				limitDuration = time.Duration(packetThrottlingDisconnectDelayHost) * time.Millisecond
			}

			if time.Since(throttleStartedAt) > time.Duration(limitDuration) {
				fmt.Println("Sustained high bandwidth detected, closing connection")

				roomsMu.RLock()
				room, exists := rooms[gameId]
				roomsMu.RUnlock()
				if !exists || room == nil {
					// fmt.Println("Room not found")
					return
				}

				if !isHost {
					conn.Close()
				} else {
					conn.Close()
				}

				return
			}

		} else {
			throttleActive = false
		}
	}

	// Before cleaning up, send a disonnect packet to the server

	roomsMu.RLock()
	_, exists := rooms[gameId]
	if !exists {
		// Someone already deleted the room
		return
	}
	roomsMu.RUnlock()

	if !isHost {

		roomsMu.RLock()
		room, exists := rooms[gameId]
		if !exists || room == nil {
			roomsMu.RUnlock()
			handleCleanup(conn, gameId)
			return
		}

		host := room.Host
		uuid := room.ClientsReverseMap[conn]
		roomsMu.RUnlock()

		packetString := "CLOSECONNECTION_" + string(uuid)
		packet := []byte(packetString)

		if host != nil {

			rooms[gameId].HostMu.Lock()

			err := host.WriteMessage(websocket.BinaryMessage, packet)

			rooms[gameId].HostMu.Unlock()

			if err != nil {
				fmt.Println("An error occurred while trying to write disconnect signal: ", err.Error())
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

	if rooms[roomID].HasHost == true && isHost != "" {
		roomsMu.RUnlock()
		http.Error(w, "{\"ok\":false,\"message\":\"Room already has a host\"}", http.StatusConflict)
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

		if room, exists := rooms[roomID]; exists && room != nil {
			room.Host = conn
			room.HostIP = r.RemoteAddr
			room.HasHost = true
		} else {
			roomsMu.Unlock()

			conn.Close()
			return
		}

		rooms[roomID].Host = conn
		rooms[roomID].HostIP = r.RemoteAddr
		rooms[roomID].HasHost = true

		roomsMu.Unlock()
	}

	numberOfClientsConnected.Add(1)
	relayForwardingLoop(conn, isHost != "", roomID)

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
		Host:               nil,
		HostIP:             "",
		Clients:            make(map[string]*websocket.Conn),
		ClientsReverseMap:  make(map[*websocket.Conn]string),
		PendingClients:     make([]*websocket.Conn, 0),
		HostMu:             sync.Mutex{},
		CreatedTime:        time.Now().UnixMilli(),
		HasHost:            false,
		LastRoomFilledTime: atomic.Int64{},
		ClientsMutexes:     make(map[*websocket.Conn]*sync.Mutex),
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
