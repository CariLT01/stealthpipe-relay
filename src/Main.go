/*
StealthPipe Relay

*/

package main

import (
	"github.com/CariLT01/stealthPipeGoRelay/src/core"
	"github.com/CariLT01/stealthPipeGoRelay/src/maintenance"
	"github.com/CariLT01/stealthPipeGoRelay/src/statistics"
	"net/http"
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

	server := core.NewServer()

	if os.Getenv("LIMITED_COMPUTE_MODE") != "" {
		server.Logger.Warn("warn: Limited compute mode enabled, remove LIMITED_COMPUTE_MODE env to disable")
		server.Logger.Warn("warn: Setting GOMAXPROCS to 1")
		runtime.GOMAXPROCS(1)
	}

	if os.Getenv("SECRET_KEY") == "" {
		server.Logger.Warn("warn: Secret key is EMPTY, your relay is NOT SECURE!")
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "7860" // Default for Hugging Face or Local testing
	}

	core.StartCleanupLoop(server)
	go statistics.MonitorTraffic(server)
	go maintenance.CleanUnusedRooms(server)
	go maintenance.CleanIdleRooms(server)

	server.IsHealthy.Store(true)

	go core.DeadlockWatchdog(server)

	http.HandleFunc("/", server.MainPathHandler)
	http.HandleFunc("/create", server.HandleCreatePath)
	http.HandleFunc("/join", server.HandleRelay)
	http.HandleFunc("/stats", server.StatsPathHandler)
	http.HandleFunc("/pow", server.HandleProofOfWorkEndpoint)
	http.HandleFunc("/ping", server.PingPathHandler)
	http.HandleFunc("/health", server.HealthPathHandler)
	server.Logger.Info("Service: Server started")

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
