package core

import (
	"fmt"
	"net/http"

	"github.com/Masterminds/semver/v3"
)

func (app *ServerData) MainPathHandler(w http.ResponseWriter, r *http.Request) {
	LogRequest(app, r)

	fmt.Fprintf(w, "Service is Online")
}

func (app *ServerData) StatsPathHandler(w http.ResponseWriter, r *http.Request) {
	LogRequest(app, r)

	fmt.Fprintf(w, "{\"ok\":true,\"packetsPerSecond\":%d,\"clientsConnected\":%d}", app.PacketsPerSecond.Load(), app.NumberOfClientsConnected.Load())
}

func (app *ServerData) HealthPathHandler(w http.ResponseWriter, r *http.Request) {

	// LogRequest(app, r)

	if !app.IsHealthy.Load() {
		// Broken, kill me
		http.Error(w, "Instance unhealthy", http.StatusServiceUnavailable)
		return
	}

	// OK
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (app *ServerData) PingPathHandler(w http.ResponseWriter, r *http.Request) {

	LogRequest(app, r)

	if !app.IsHealthy.Load() {
		// Broken, kill me
		http.Error(w, "Instance unhealthy", http.StatusServiceUnavailable)
		return
	}

	query := r.URL.Query() // url.Values

	clientVersion := query.Get("version") // returns "" if not present

	if clientVersion == "" {
		http.Error(w, "Outdated client! Please update to: "+app.Config.RelayVersion+" to connect to this relay.", http.StatusUpgradeRequired)
		return
	}

	v, err := semver.NewVersion(clientVersion)
	if err != nil {
		http.Error(w, "Invalid version provided", http.StatusBadRequest)
		return
	}

	relayVersion, err := semver.NewVersion(app.Config.RelayVersion)
	if err != nil {
		http.Error(w, "Server Error", http.StatusInternalServerError)
		return
	}

	if v.Major() > relayVersion.Major() {
		http.Error(w, "Your client version is incompatible with this relay's version. You have: "+clientVersion+", the relay you are trying to connect to has: "+app.Config.RelayVersion, http.StatusUpgradeRequired)
		return
	}

	if v.Major() < relayVersion.Major() {
		http.Error(w, "Outdated client! Client version & relay version are incompatible. You must update to keep using this relay! Your client version is: "+clientVersion+", the relay you are trying to connect to has: "+app.Config.RelayVersion+". Please update!", http.StatusUpgradeRequired)
		return
	}

	// OK
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func (app *ServerData) HandleProofOfWorkEndpoint(w http.ResponseWriter, r *http.Request) {

	LogRequest(app, r)

	proofOfWork, salt := generateProofOfWork(app)

	fmt.Fprintf(w, "{\"ok\":true,\"token\":\"%s\",\"salt\":\"%s\"}", proofOfWork, salt)
}
