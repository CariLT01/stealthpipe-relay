/*func relayForwardingLoop(conn *websocket.Conn, isHost bool, gameId string) {
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
		roomsMu.RUnlock()
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
}*/

/*func handlePacket(packetData []byte, messageType int, ws *websocket.Conn, gameId string, isHost bool, allowanceLeftInBucket *int64) {

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
		if !exists || room == nil || room.Host == nil {
			roomsMu.RUnlock()
			return
		}
		// Capture the host pointer and the room's specific mutex
		host := room.Host
		hostMu := &room.HostMu
		roomsMu.RUnlock() // RELEASE GLOBAL LOCK IMMEDIATELY

		// Now do the heavy lifting
		hostMu.Lock()
		err := host.WriteMessage(websocket.BinaryMessage, packetData)
		hostMu.Unlock()

		if err != nil {
			fmt.Println("An error occurred while trying to forward the packet: ", err.Error())
			return
		}
		// fmt.Println("Forwarded packet as client")
	}

}*/