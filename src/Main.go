/*
StealthPipe Relay

*/

package main

import (
	"github.com/CariLT01/stealthPipeGoRelay/src/core"
	"os"
	"runtime"
)

// HANDLE CLEANUP NEEDS THE roomsMu LOCK! ALWAYS UNLOCK BEFORE CLEANING UP. ALSO, ALWAYS UNLOCK WHEN CALLING .CLOSE() ON A WEBSOCKET THAT HAS A RELAY LOOP ALREADY RUNNING, WHICH WILL TRIGGER HANDLECLEANUP, WHICH WILL NEED THE LOCK.
// IF NOT UNLOCKED BEFORE, A DEADLOCK WILL HAPPEN

/*func getCurrentBandwidth(gameId string) int64 {
	roomsMu.RLock()
	numberOfClients := len(rooms[gameId].ClientsToHostConnections)
	roomsMu.RUnlock()

	bandwidthCalculated := packetThrottlingLimitBaseline + int64(packetThrottlingLimitBonus)*int64(math.Sqrt(float64(numberOfClients)))

	return min(bandwidthCalculated, packetThrottlingLimitMaximum)

}*/

func main() {

	server := core.NewServer(false)

	if os.Getenv("LIMITED_COMPUTE_MODE") != "" {
		server.Logger.Warn("warn: Limited compute mode enabled, remove LIMITED_COMPUTE_MODE env to disable")
		server.Logger.Warn("warn: Setting GOMAXPROCS to 1")
		runtime.GOMAXPROCS(1)
	}

	if os.Getenv("SECRET_KEY") == "" {
		server.Logger.Warn("warn: Secret key is EMPTY, your relay is NOT SECURE!")
	}

	server.Serve()

}
