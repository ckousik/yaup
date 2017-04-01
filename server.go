package yaup

import (
	"fmt"
	"github.com/hashicorp/yamux"
	// "io"
	"net/http"
)

var (
	// ErrNotUpgradeReq ...
	ErrNotUpgradeReq = fmt.Errorf("not an upgrade request")

	// ErrNoHijack ...
	ErrNoHijack = fmt.Errorf("webserver doesn't support hijacking")
)

// IsUpgradeRequest ...
func IsUpgradeRequest(req *http.Request) bool {
	if req.Method != http.MethodGet ||
		req.Header.Get("Upgrade") != "yamux" ||
		req.Header.Get("Connection") != "Upgrade" ||
		req.ProtoMajor != 1 || req.ProtoMinor != 1 {
		return false
	}
	return true
}

// Upgrade ...
func Upgrade(w http.ResponseWriter, r *http.Request) (*yamux.Session, error) {
	if !IsUpgradeRequest(r) {
		return nil, ErrNotUpgradeReq
	}
	// Hijack connection
	h, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "webserver doesn't support hijacking", http.StatusInternalServerError)
		return nil, ErrNoHijack
	}

	conn, brw, err := h.Hijack()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return nil, err
	}
	session, err := yamux.Client(conn, nil)
	if err != nil {
		return nil, err
	}

	_, err = brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: yamux\r\nConnection: Upgrade\r\n\r\n")
	_ = brw.Flush()
	if err != nil {
		_ = session.Close()
		return nil, err
	}

	return session, nil
}
