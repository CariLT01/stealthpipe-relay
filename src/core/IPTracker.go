package core

import (
	"net"
	"net/http"
)

/*
Telemetry for knowing where users come from (which region)
*/
func GetIPFromRequest(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		// 2. Fall back to X-Real-IP (another common proxy header)
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		// 3. Fall back to the remote address
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}

	return ip
}

func LogRequest(app *ServerData, r *http.Request) {
	endpoint := r.URL.Path

	clientIp := GetIPFromRequest(r)

	app.Logger.Info("incoming request", "endpoint", endpoint, "ip", clientIp)

}
