package ui

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"mouseshare/internal/app"
	"mouseshare/internal/domain"
)

//go:embed static/*
var assets embed.FS

type Server struct {
	app *app.Service
}

func New(app *app.Service) *Server {
	return &Server{app: app}
}

func (s *Server) Handler() (http.Handler, error) {
	mux := http.NewServeMux()
	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}

	mux.Handle("/", http.FileServer(http.FS(staticFS)))
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/approve", s.handleApprove)
	mux.HandleFunc("/api/reject", s.handleReject)
	mux.HandleFunc("/api/layout", s.handleLayout)
	mux.HandleFunc("/api/manual-pair", s.handleManualPair)
	mux.HandleFunc("/api/send", s.handleSend)
	mux.HandleFunc("/api/control/start", s.handleControlStart)
	mux.HandleFunc("/api/control/stop", s.handleControlStop)
	return mux, nil
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.app.State())
}

func (s *Server) handleApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID string `json:"peerId"`
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.ApprovePeer(req.PeerID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleReject(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID string `json:"peerId"`
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.RejectPeer(req.PeerID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLayout(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, s.app.State().Layout)
		return
	}
	var layout []domain.LayoutNode
	if err := decode(r, &layout); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.SaveLayout(layout); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleManualPair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Addr string `json:"addr"`
		Code string `json:"code"`
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.ManualPair(strings.TrimSpace(req.Addr), strings.TrimSpace(req.Code)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	peerID := strings.TrimSpace(r.URL.Query().Get("peerId"))
	if peerID == "" {
		http.Error(w, "missing peerId", http.StatusBadRequest)
		return
	}
	if err := r.ParseMultipartForm(128 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	files := r.MultipartForm.File["files"]
	if err := s.app.SendUpload(peerID, files); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleControlStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PeerID string `json:"peerId"`
	}
	if err := decode(r, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.app.StartControl(strings.TrimSpace(req.PeerID)); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleControlStop(w http.ResponseWriter, r *http.Request) {
	if err := s.app.StopControl(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func decode(r *http.Request, out interface{}) error {
	defer r.Body.Close()
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("request body required")
	}
	return json.Unmarshal(body, out)
}

func writeJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
