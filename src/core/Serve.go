package core

import (
	"net/http"
	"os"
)

func (server *ServerData) DoInit() {

	StartCleanupLoop(server)
	go MonitorTraffic(server)
	go CleanUnusedRooms(server)
	go CleanIdleRooms(server)

	go server.StatsPPSTicker()

	server.IsHealthy.Store(true)

	go DeadlockWatchdog(server)

	server.Mux.HandleFunc("/", server.MainPathHandler)
	server.Mux.HandleFunc("/create", server.HandleCreatePath)
	server.Mux.HandleFunc("/join", server.HandleRelay)
	server.Mux.HandleFunc("/stats", server.StatsPathHandler)
	server.Mux.HandleFunc("/pow", server.HandleProofOfWorkEndpoint)
	server.Mux.HandleFunc("/ping", server.PingPathHandler)
	server.Mux.HandleFunc("/health", server.HealthPathHandler)
	server.Logger.Info("Service: Server started")
}

func getPort() string {
	port := os.Getenv("PORT")
	if port == "" {
		port = "7860" // Default for Hugging Face or Local testing
	}
	return port

}

func (server *ServerData) Serve() {
	port := getPort()
	server.Logger.Info("Using port", "port", 7860)

	server.DoInit()

	/*go func() {
		fmt.Println("Triggered test code")
		// TEST CODE
		roomsMu.Lock()
		roomsMu.Lock()
	}()*/

	if err := http.ListenAndServe(":"+port, server.Mux); err != nil {
		panic(err)
	}
}

func (server *ServerData) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	server.DoInit()
	server.Logger.Info("using internal serverHTTP func")
	server.Mux.ServeHTTP(w, r)
}

func (server *ServerData) InitHttp() {
	server.DoInit()
	server.Logger.Info("using internal serverHTTP func")
}

func (server *ServerData) CallHttp(w http.ResponseWriter, r *http.Request) {
	server.Mux.ServeHTTP(w, r)
}
