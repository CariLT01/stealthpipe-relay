package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/websocket"
)

type Room struct {
	Host              *websocket.Conn
	HostMu            sync.Mutex
	HostIP            string
	Clients           map[string]*websocket.Conn
	ClientsReverseMap map[*websocket.Conn]string
	PendingClients    []*websocket.Conn
	CreatedTime       int64
	HasHost           bool
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

var (
	// HandshakeCounter tracks requests in the current second
	handshakeCounter uint64
	// CurrentDifficulty is what we tell new clients to use
	currentDifficulty int32     = 5
	lastUpped         time.Time // Tracks when we last moved difficulty UP
	cooldownDuration  = 30 * time.Second
)

var (
	// usedSalts stores salts that have already been verified
	usedSalts = make(map[string]int64)
	// mu protects the map from concurrent access
	usedSaltsMutex sync.Mutex
)

func monitorTraffic() {
	ticker := time.NewTicker(1 * time.Second)
	for range ticker.C {
		rps := atomic.SwapUint64(&handshakeCounter, 0)

		// 1. Determine what the difficulty SHOULD be based on RPS
		var targetDiff int32
		switch {
		case rps > 100:
			targetDiff = 7 // Panic
		case rps > 20:
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
			if time.Since(lastUpped) > cooldownDuration {
				atomic.StoreInt32(&currentDifficulty, targetDiff)
			}
		}
	}
}

func cleanUnusedRooms() {
	ticker := time.NewTicker(15 * time.Second)

	for range ticker.C {

		roomsMu.Lock()

		for code, room := range rooms {
			if !room.HasHost && time.Now().UnixMilli()-room.CreatedTime > 60*1000 {

				delete(rooms, code)

			}
		}

		roomsMu.Unlock()
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
		rooms[roomID].HasHost = true

		roomsMu.Unlock()
	}

	numberOfClientsConnected++
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
		Host:              nil,
		HostIP:            "",
		Clients:           make(map[string]*websocket.Conn),
		ClientsReverseMap: make(map[*websocket.Conn]string),
		PendingClients:    make([]*websocket.Conn, 0),
		HostMu:            sync.Mutex{},
		CreatedTime:       time.Now().UnixMilli(),
		HasHost:           false,
	}

	fmt.Fprintf(w, "{\"ok\":true,\"message\":%s}", gameId)
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

	http.HandleFunc("/", mainPathHandler)
	http.HandleFunc("/create", handleCreatePath)
	http.HandleFunc("/join", handleRelay)
	http.HandleFunc("/stats", statsPathHandler)
	http.HandleFunc("/pow", handleProofOfWorkEndpoint)
	http.HandleFunc("/ping", pingPathHandler)
	fmt.Println("StealthPipeRelay: Server started on :7860")
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		panic(err)
	}
}
