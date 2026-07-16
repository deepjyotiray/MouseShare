package app

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"mouseshare/internal/config"
	"mouseshare/internal/discovery"
	"mouseshare/internal/domain"
	"mouseshare/internal/platform"
	"mouseshare/internal/security"
	"mouseshare/internal/transport"
)

const (
	Version          = "0.1.0"
	DefaultPort      = 41091
	chunkSize        = 32 * 1024
	protoVersion     = 1
	edgeMargin       = 2.0
	panicWindow      = 700 * time.Millisecond
	escapeKeyCodeMac = 53
	escapeKeyCodeWin = 0x1B
)

type Service struct {
	ctx         context.Context
	cancel      context.CancelFunc
	baseDir     string
	log         *log.Logger
	store       *config.Store
	bridge      platform.Bridge
	certificate tls.Certificate

	self        domain.DeviceInfo
	listenAddr  string
	tlsConfig   *tls.Config
	httpBaseURL string

	mu            sync.RWMutex
	settings      config.Settings
	peers         map[string]domain.PeerState
	transfers     map[string]domain.TransferJob
	pendingPair   *domain.PairRequest
	control       *domain.ControlSession
	controlState  *controlRuntime
	controlConn   net.Conn
	localBounds   platform.Rect
	recvTransfers map[string]*incomingTransfer
	lastEscapeAt  time.Time
}

type incomingTransfer struct {
	file   *os.File
	writer *zip.Writer
	job    domain.TransferJob
}

type controlRuntime struct {
	PeerID       string
	PeerBounds   platform.Rect
	EnteredFrom  string
	RemoteCursor platform.Point
}

func New(baseDir string, logger *log.Logger) (*Service, error) {
	ctx, cancel := context.WithCancel(context.Background())
	store, err := config.NewStore(baseDir)
	if err != nil {
		cancel()
		return nil, err
	}
	settings, err := store.Load()
	if err != nil {
		cancel()
		return nil, err
	}
	name := settings.DeviceName
	if name == "" {
		host, _ := os.Hostname()
		name = host
		settings.DeviceName = host
	}
	cert, fingerprint, pairCode, err := security.EnsureCertificate(baseDir, name)
	if err != nil {
		cancel()
		return nil, err
	}
	selfID := fingerprint[:16]
	self := domain.DeviceInfo{
		ID:          selfID,
		Name:        name,
		OS:          runtime.GOOS,
		Port:        DefaultPort,
		Fingerprint: fingerprint,
		PairCode:    pairCode,
		Version:     Version,
	}
	svc := &Service{
		ctx:           ctx,
		cancel:        cancel,
		baseDir:       baseDir,
		log:           logger,
		store:         store,
		bridge:        platform.Current(),
		certificate:   cert,
		self:          self,
		settings:      settings,
		peers:         map[string]domain.PeerState{},
		transfers:     map[string]domain.TransferJob{},
		recvTransfers: map[string]*incomingTransfer{},
	}
	svc.tlsConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
		ClientAuth:   tls.RequireAnyClientCert,
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return errors.New("peer certificate missing")
			}
			fp := fingerprintHex(cs.PeerCertificates[0].Raw)
			svc.mu.RLock()
			defer svc.mu.RUnlock()
			for _, trusted := range svc.settings.TrustedPeers {
				if trusted == fp {
					return nil
				}
			}
			if svc.pendingPair != nil && svc.pendingPair.PeerID != "" {
				return nil
			}
			return fmt.Errorf("untrusted peer %s", fp[:12])
		},
	}
	return svc, nil
}

func (s *Service) Start() error {
	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", DefaultPort), s.tlsConfig)
	if err != nil {
		return err
	}
	s.listenAddr = ln.Addr().String()
	go s.serveTLS(ln)

	addr, err := localIPv4()
	if err == nil {
		s.self.Addr = addr
	}
	if bounds, err := s.bridge.Bounds(s.ctx); err == nil {
		s.localBounds = bounds
		s.self.ScreenWidth = int(bounds.Width)
		s.self.ScreenHeight = int(bounds.Height)
		s.ensureLocalLayout(bounds)
	}

	disco := discovery.New(s.self, s.upsertPeer, s.log.Printf)
	if err := disco.Start(s.ctx); err != nil {
		return err
	}
	events := make(chan platform.Event, 512)
	if err := s.bridge.StartCapture(s.ctx, events); err != nil {
		s.log.Printf("input capture unavailable: %v", err)
	} else {
		go s.consumeLocalEvents(events)
	}
	go func() {
		<-s.ctx.Done()
		disco.Wait()
	}()
	return nil
}

func (s *Service) Close() error {
	_ = s.stopControlSession()
	_ = s.bridge.StopCapture()
	s.cancel()
	return nil
}

func (s *Service) State() domain.AppState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	peers := make([]domain.PeerState, 0, len(s.peers))
	for _, peer := range s.peers {
		if time.Since(peer.Device.SeenAt) > 10*time.Second {
			peer.Status = domain.PeerStatusOffline
		}
		peers = append(peers, peer)
	}
	sort.Slice(peers, func(i, j int) bool { return peers[i].Device.Name < peers[j].Device.Name })

	transfers := make([]domain.TransferJob, 0, len(s.transfers))
	for _, job := range s.transfers {
		transfers = append(transfers, job)
	}
	sort.Slice(transfers, func(i, j int) bool { return transfers[i].CreatedAt.After(transfers[j].CreatedAt) })

	return domain.AppState{
		Self:          s.self,
		Permissions:   s.bridge.Permissions(s.ctx),
		Peers:         peers,
		Layout:        slices.Clone(s.settings.Layout),
		Transfers:     transfers,
		Control:       s.control,
		PendingPair:   s.pendingPair,
		TrustedPeers:  mapsClone(s.settings.TrustedPeers),
		ListenAddr:    s.listenAddr,
		ManualPairURL: fmt.Sprintf("%s/api/manual-pair", s.httpBaseURL),
	}
}

func (s *Service) SetHTTPBaseURL(url string) {
	s.httpBaseURL = url
}

func (s *Service) StartControl(peerID string) error {
	start := platform.Point{
		X: s.localBounds.MinX + (s.localBounds.Width / 2),
		Y: s.localBounds.MinY + (s.localBounds.Height / 2),
	}
	return s.beginControl(peerID, "", start)
}

func (s *Service) StopControl() error {
	return s.stopControlSession()
}

func (s *Service) ApprovePeer(peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[peerID]
	if !ok {
		return fmt.Errorf("peer %s not found", peerID)
	}
	now := time.Now().UTC()
	peer.Status = domain.PeerStatusTrusted
	peer.ApprovedAt = &now
	s.peers[peerID] = peer
	s.settings.TrustedPeers[peerID] = peer.Device.Fingerprint
	s.ensurePeerLayoutLocked(peer.Device)
	return s.store.Save(s.settings)
}

func (s *Service) RejectPeer(peerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[peerID]
	if !ok {
		return fmt.Errorf("peer %s not found", peerID)
	}
	peer.Status = domain.PeerStatusRejected
	s.peers[peerID] = peer
	return nil
}

func (s *Service) SaveLayout(layout []domain.LayoutNode) error {
	s.mu.Lock()
	s.settings.Layout = layout
	s.ensureLocalLayout(s.localBounds)
	err := s.store.Save(s.settings)
	layoutCopy := slices.Clone(s.settings.Layout)
	s.mu.Unlock()
	if err != nil {
		return err
	}
	go s.broadcastLayout(layoutCopy)
	return nil
}

func (s *Service) ManualPair(addr, code string) error {
	conn, peer, fp, err := s.connectPeer(addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if len(fp) < 6 || !strings.EqualFold(fp[:6], strings.TrimSpace(code)) {
		return fmt.Errorf("pair code mismatch")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	peer.Status = domain.PeerStatusTrusted
	peer.ApprovedAt = &now
	s.peers[peer.Device.ID] = peer
	s.settings.TrustedPeers[peer.Device.ID] = peer.Device.Fingerprint
	s.ensurePeerLayoutLocked(peer.Device)
	return s.store.Save(s.settings)
}

func (s *Service) SendUpload(peerID string, files []*multipart.FileHeader) error {
	if len(files) == 0 {
		return fmt.Errorf("no files selected")
	}
	peer, ok := s.lookupTrustedPeer(peerID)
	if !ok {
		return fmt.Errorf("peer %s not trusted", peerID)
	}
	archivePath, displayName, err := s.buildArchive(files)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	info, err := os.Stat(archivePath)
	if err != nil {
		return err
	}
	job := domain.TransferJob{
		ID:         newID(),
		PeerID:     peerID,
		Direction:  "outgoing",
		FileName:   displayName,
		BytesTotal: info.Size(),
		Status:     domain.TransferOffering,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	s.putTransfer(job)

	conn, _, _, err := s.connectPeer(discovery.FormatManualAddress(peer.Device.Addr, peer.Device.Port))
	if err != nil {
		s.failTransfer(job.ID, err)
		return err
	}
	defer conn.Close()

	if err := writeMessage(conn, "transfer.offer", transport.TransferOffer{
		ID:           job.ID,
		FileName:     filepath.Base(archivePath),
		BytesTotal:   info.Size(),
		Archive:      true,
		Directory:    len(files) > 1,
		OriginalName: displayName,
	}); err != nil {
		s.failTransfer(job.ID, err)
		return err
	}

	var decision transport.TransferDecision
	if err := readPayload(conn, &decision); err != nil {
		s.failTransfer(job.ID, err)
		return err
	}
	if !decision.Accepted {
		s.updateTransferStatus(job.ID, domain.TransferRejected, decision.Reason)
		return fmt.Errorf(decision.Reason)
	}

	s.updateTransferStatus(job.ID, domain.TransferInProgress, "")
	file, err := os.Open(archivePath)
	if err != nil {
		s.failTransfer(job.ID, err)
		return err
	}
	defer file.Close()

	buf := make([]byte, chunkSize)
	index := 0
	for {
		n, err := file.Read(buf)
		if err != nil && !errors.Is(err, io.EOF) {
			s.failTransfer(job.ID, err)
			return err
		}
		if n > 0 {
			if err := writeMessage(conn, "transfer.chunk", transport.TransferChunk{
				ID:    job.ID,
				Index: index,
				Data:  append([]byte(nil), buf[:n]...),
			}); err != nil {
				s.failTransfer(job.ID, err)
				return err
			}
			index++
			s.addTransferProgress(job.ID, int64(n))
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	if err := writeMessage(conn, "transfer.chunk", transport.TransferChunk{ID: job.ID, Done: true}); err != nil {
		s.failTransfer(job.ID, err)
		return err
	}
	s.updateTransferStatus(job.ID, domain.TransferComplete, "")
	return nil
}

func (s *Service) upsertPeer(peer domain.DeviceInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.peers[peer.ID]
	state.Device = peer
	if trustedFP, trusted := s.settings.TrustedPeers[peer.ID]; trusted && trustedFP == peer.Fingerprint {
		state.Status = domain.PeerStatusTrusted
	} else if !ok || state.Status == "" {
		state.Status = domain.PeerStatusPending
	}
	if state.Layout == nil {
		for i := range s.settings.Layout {
			if s.settings.Layout[i].DeviceID == peer.ID {
				node := s.settings.Layout[i]
				state.Layout = &node
				break
			}
		}
		if state.Layout == nil && peer.ScreenWidth > 0 && peer.ScreenHeight > 0 {
			node := s.defaultPeerLayoutLocked(peer)
			state.Layout = &node
		}
	}
	s.peers[peer.ID] = state
}

func (s *Service) serveTLS(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return
			default:
				s.log.Printf("accept failed: %v", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Service) handleConn(conn net.Conn) {
	defer conn.Close()

	tlsConn, ok := conn.(*tls.Conn)
	if ok {
		if err := tlsConn.Handshake(); err != nil {
			s.log.Printf("tls handshake failed: %v", err)
			return
		}
	}

	for {
		var raw struct {
			Type      string          `json:"type"`
			Version   int             `json:"version"`
			Timestamp time.Time       `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if err := json.NewDecoder(conn).Decode(&raw); err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Printf("read message failed: %v", err)
			}
			return
		}
		switch raw.Type {
		case "pair.hello":
			if err := s.handlePairHello(conn, raw.Payload); err != nil {
				s.log.Printf("pair hello failed: %v", err)
				return
			}
		case "control.enter":
			if err := s.handleControlEnter(raw.Payload); err != nil {
				s.log.Printf("control enter failed: %v", err)
				return
			}
		case "control.leave":
			if err := s.handleControlLeave(raw.Payload); err != nil {
				s.log.Printf("control leave failed: %v", err)
				return
			}
		case "control.event":
			if err := s.handleControlEvent(raw.Payload); err != nil {
				s.log.Printf("control event failed: %v", err)
				return
			}
		case "layout.update":
			if err := s.handleLayoutUpdate(raw.Payload); err != nil {
				s.log.Printf("layout update failed: %v", err)
				return
			}
		case "transfer.offer":
			if err := s.handleTransferOffer(conn, raw.Payload); err != nil {
				s.log.Printf("transfer offer failed: %v", err)
				return
			}
		case "transfer.chunk":
			if err := s.handleTransferChunk(raw.Payload); err != nil {
				s.log.Printf("transfer chunk failed: %v", err)
				return
			}
		case "heartbeat":
			continue
		default:
			_ = writeMessage(conn, "system.error", transport.ErrorPayload{Message: "unsupported message"})
		}
	}
}

func (s *Service) handlePairHello(conn net.Conn, payload json.RawMessage) error {
	var pair transport.PairPayload
	if err := json.Unmarshal(payload, &pair); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	peer, ok := s.peers[pair.DeviceID]
	if !ok {
		peer = domain.PeerState{
			Device: domain.DeviceInfo{
				ID:           pair.DeviceID,
				Name:         pair.DeviceName,
				ScreenWidth:  pair.ScreenWidth,
				ScreenHeight: pair.ScreenHeight,
				Fingerprint:  pair.Fingerprint,
				PairCode:     pair.PairCode,
				SeenAt:       time.Now().UTC(),
			},
			Status: domain.PeerStatusPending,
		}
	}
	s.pendingPair = &domain.PairRequest{
		PeerID:    pair.DeviceID,
		PairCode:  pair.PairCode,
		Requested: true,
	}
	s.peers[pair.DeviceID] = peer
	return writeMessage(conn, "pair.ack", transport.PairPayload{
		DeviceID:     s.self.ID,
		DeviceName:   s.self.Name,
		ScreenWidth:  s.self.ScreenWidth,
		ScreenHeight: s.self.ScreenHeight,
		Fingerprint:  s.self.Fingerprint,
		PairCode:     s.self.PairCode,
	})
}

func (s *Service) handleTransferOffer(conn net.Conn, payload json.RawMessage) error {
	var offer transport.TransferOffer
	if err := json.Unmarshal(payload, &offer); err != nil {
		return err
	}
	downloads := s.settings.DownloadsDir
	if downloads == "" {
		downloads = defaultDownloadsDir()
	}
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		return err
	}
	target := filepath.Join(downloads, fmt.Sprintf("%s-%s", time.Now().Format("20060102-150405"), offer.FileName))
	file, err := os.Create(target)
	if err != nil {
		return err
	}
	job := domain.TransferJob{
		ID:          offer.ID,
		PeerID:      "",
		Direction:   "incoming",
		FileName:    offer.OriginalName,
		BytesTotal:  offer.BytesTotal,
		Status:      domain.TransferInProgress,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		DownloadDir: target,
	}
	s.mu.Lock()
	s.transfers[job.ID] = job
	s.recvTransfers[job.ID] = &incomingTransfer{file: file, job: job}
	s.mu.Unlock()

	return writeMessage(conn, "transfer.decision", transport.TransferDecision{
		ID:       offer.ID,
		Accepted: true,
	})
}

func (s *Service) handleTransferChunk(payload json.RawMessage) error {
	var chunk transport.TransferChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return err
	}
	s.mu.Lock()
	transfer := s.recvTransfers[chunk.ID]
	s.mu.Unlock()
	if transfer == nil {
		return fmt.Errorf("unknown transfer %s", chunk.ID)
	}
	if chunk.Done {
		if err := transfer.file.Close(); err != nil {
			return err
		}
		s.mu.Lock()
		job := s.transfers[chunk.ID]
		job.Status = domain.TransferComplete
		job.BytesDone = job.BytesTotal
		job.UpdatedAt = time.Now().UTC()
		s.transfers[chunk.ID] = job
		delete(s.recvTransfers, chunk.ID)
		s.mu.Unlock()
		return nil
	}
	n, err := transfer.file.Write(chunk.Data)
	if err != nil {
		return err
	}
	s.addTransferProgress(chunk.ID, int64(n))
	return nil
}

func (s *Service) connectPeer(addr string) (*tls.Conn, domain.PeerState, string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
		port = fmt.Sprintf("%d", DefaultPort)
		addr = net.JoinHostPort(host, port)
	}
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates:       []tls.Certificate{s.certificate},
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
	})
	if err != nil {
		return nil, domain.PeerState{}, "", err
	}
	state := conn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		conn.Close()
		return nil, domain.PeerState{}, "", errors.New("peer certificate missing")
	}
	fp := fingerprintHex(state.PeerCertificates[0].Raw)
	hello := transport.PairPayload{
		DeviceID:     s.self.ID,
		DeviceName:   s.self.Name,
		ScreenWidth:  s.self.ScreenWidth,
		ScreenHeight: s.self.ScreenHeight,
		Fingerprint:  s.self.Fingerprint,
		PairCode:     s.self.PairCode,
	}
	if err := writeMessage(conn, "pair.hello", hello); err != nil {
		conn.Close()
		return nil, domain.PeerState{}, "", err
	}
	var ack transport.PairPayload
	if err := readPayload(conn, &ack); err != nil {
		conn.Close()
		return nil, domain.PeerState{}, "", err
	}
	peer := domain.PeerState{
		Device: domain.DeviceInfo{
			ID:           ack.DeviceID,
			Name:         ack.DeviceName,
			Addr:         host,
			Port:         DefaultPort,
			ScreenWidth:  ack.ScreenWidth,
			ScreenHeight: ack.ScreenHeight,
			Fingerprint:  ack.Fingerprint,
			PairCode:     ack.PairCode,
			SeenAt:       time.Now().UTC(),
		},
		Status: domain.PeerStatusPending,
	}
	s.mu.Lock()
	existing := s.peers[ack.DeviceID]
	if existing.Device.ID != "" {
		peer = existing
		peer.Device = existing.Device
		peer.Device.Addr = host
		peer.Device.Port = DefaultPort
		peer.Device.SeenAt = time.Now().UTC()
	}
	s.peers[ack.DeviceID] = peer
	s.mu.Unlock()
	return conn, peer, fp, nil
}

func (s *Service) handleControlEnter(payload json.RawMessage) error {
	var msg transport.ControlEnter
	if err := json.Unmarshal(payload, &msg); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.control = &domain.ControlSession{
		ActivePeerID: msg.DeviceID,
		Mode:         "receiving",
		StartedAt:    time.Now().UTC(),
	}
	return nil
}

func (s *Service) handleControlLeave(payload json.RawMessage) error {
	var msg transport.ControlLeave
	if err := json.Unmarshal(payload, &msg); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.control != nil && s.control.ActivePeerID == msg.DeviceID {
		s.control = nil
	}
	return nil
}

func (s *Service) handleControlEvent(payload json.RawMessage) error {
	var event platform.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	return s.bridge.Inject(s.ctx, event)
}

func (s *Service) handleLayoutUpdate(payload json.RawMessage) error {
	var update transport.LayoutUpdate
	if err := json.Unmarshal(payload, &update); err != nil {
		return err
	}
	layout := make([]domain.LayoutNode, 0, len(update.Nodes))
	for _, node := range update.Nodes {
		layout = append(layout, domain.LayoutNode{
			DeviceID: node.DeviceID,
			X:        node.X,
			Y:        node.Y,
			Width:    node.Width,
			Height:   node.Height,
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings.Layout = layout
	s.ensureLocalLayout(s.localBounds)
	return s.store.Save(s.settings)
}

func (s *Service) lookupTrustedPeer(peerID string) (domain.PeerState, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[peerID]
	return peer, ok && peer.Status == domain.PeerStatusTrusted
}

func (s *Service) buildArchive(files []*multipart.FileHeader) (string, string, error) {
	path := filepath.Join(os.TempDir(), fmt.Sprintf("mouseshare-%s.zip", newID()))
	out, err := os.Create(path)
	if err != nil {
		return "", "", err
	}
	defer out.Close()

	archive := zip.NewWriter(out)
	defer archive.Close()

	names := make([]string, 0, len(files))
	for _, fh := range files {
		names = append(names, fh.Filename)
		src, err := fh.Open()
		if err != nil {
			return "", "", err
		}
		writer, err := archive.Create(fh.Filename)
		if err != nil {
			src.Close()
			return "", "", err
		}
		if _, err := io.Copy(writer, src); err != nil {
			src.Close()
			return "", "", err
		}
		src.Close()
	}
	displayName := names[0]
	if len(names) > 1 {
		displayName = fmt.Sprintf("%s and %d more", names[0], len(names)-1)
	}
	return path, displayName, archive.Close()
}

func (s *Service) putTransfer(job domain.TransferJob) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.transfers[job.ID] = job
}

func (s *Service) failTransfer(id string, err error) {
	s.updateTransferStatus(id, domain.TransferFailed, err.Error())
}

func (s *Service) updateTransferStatus(id string, status domain.TransferStatus, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.transfers[id]
	job.Status = status
	job.Error = reason
	job.UpdatedAt = time.Now().UTC()
	s.transfers[id] = job
}

func (s *Service) addTransferProgress(id string, bytes int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.transfers[id]
	job.BytesDone += bytes
	job.UpdatedAt = time.Now().UTC()
	s.transfers[id] = job
}

func writeMessage(conn net.Conn, kind string, payload interface{}) error {
	msg := transport.MessageEnvelope{
		Type:      kind,
		Version:   protoVersion,
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	return json.NewEncoder(conn).Encode(msg)
}

func readPayload(conn net.Conn, out interface{}) error {
	var raw struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(conn).Decode(&raw); err != nil {
		return err
	}
	return json.Unmarshal(raw.Payload, out)
}

func fingerprintHex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func localIPv4() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.IsLoopback() {
				continue
			}
			if ip4 := ipnet.IP.To4(); ip4 != nil {
				return ip4.String(), nil
			}
		}
	}
	return "", fmt.Errorf("no active IPv4 address found")
}

func defaultDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "Downloads")
	}
	return filepath.Join(home, "Downloads", "MouseShare")
}

func newID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func mapsClone(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (s *Service) stopControlSession() error {
	s.mu.Lock()
	conn := s.controlConn
	control := s.control
	s.controlConn = nil
	s.control = nil
	s.controlState = nil
	s.mu.Unlock()

	if control != nil && conn != nil {
		_ = writeMessage(conn, "control.leave", transport.ControlLeave{DeviceID: s.self.ID})
	}
	_ = s.bridge.ExitControl(s.ctx)
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (s *Service) ensureLocalLayout(bounds platform.Rect) {
	if bounds.Empty() {
		return
	}
	for i := range s.settings.Layout {
		if s.settings.Layout[i].DeviceID == s.self.ID {
			s.settings.Layout[i].Width = int(bounds.Width)
			s.settings.Layout[i].Height = int(bounds.Height)
			return
		}
	}
	s.settings.Layout = append(s.settings.Layout, domain.LayoutNode{
		DeviceID: s.self.ID,
		X:        0,
		Y:        0,
		Width:    int(bounds.Width),
		Height:   int(bounds.Height),
	})
}

func (s *Service) consumeLocalEvents(events <-chan platform.Event) {
	for {
		select {
		case <-s.ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			s.handleLocalEvent(event)
		}
	}
}

func (s *Service) handleLocalEvent(event platform.Event) {
	s.mu.RLock()
	active := s.controlState != nil
	s.mu.RUnlock()
	if active && s.isPanicEscape(event) {
		if s.registerEscapeTap() {
			s.log.Printf("panic stop via double Escape")
			_ = s.stopControlSession()
		}
		return
	}
	if active && s.isStopHotkey(event) {
		if active {
			s.log.Printf("remote control stopped with emergency hotkey")
			_ = s.stopControlSession()
		}
		return
	}
	if active {
		if err := s.forwardControlEvent(event); err != nil {
			s.log.Printf("control forwarding failed: %v", err)
			_ = s.stopControlSession()
		}
		return
	}
	if event.Kind == "mouse_move" {
		s.maybeStartControlFromEdge(event)
	}
}

func (s *Service) maybeStartControlFromEdge(event platform.Event) {
	if s.localBounds.Empty() {
		return
	}
	direction := ""
	switch {
	case event.X <= s.localBounds.MinX+edgeMargin:
		direction = "left"
	case event.X >= s.localBounds.MaxX()-1-edgeMargin:
		direction = "right"
	case event.Y <= s.localBounds.MinY+edgeMargin:
		direction = "up"
	case event.Y >= s.localBounds.MaxY()-1-edgeMargin:
		direction = "down"
	default:
		return
	}
	peerID, ok := s.adjacentPeer(direction)
	if !ok {
		return
	}
	if err := s.beginControl(peerID, direction, platform.Point{X: event.X, Y: event.Y}); err != nil {
		s.log.Printf("auto control start failed: %v", err)
	}
}

func (s *Service) beginControl(peerID, direction string, localCursor platform.Point) error {
	peer, ok := s.lookupTrustedPeer(peerID)
	if !ok {
		return fmt.Errorf("peer %s not trusted", peerID)
	}
	peerBounds, err := s.peerBounds(peerID)
	if err != nil {
		return err
	}
	if err := s.stopControlSession(); err != nil {
		return err
	}
	conn, _, _, err := s.connectPeer(discovery.FormatManualAddress(peer.Device.Addr, peer.Device.Port))
	if err != nil {
		return err
	}
	if err := writeMessage(conn, "control.enter", transport.ControlEnter{
		DeviceID:   s.self.ID,
		DeviceName: s.self.Name,
	}); err != nil {
		conn.Close()
		return err
	}
	remoteCursor := s.entryPointFor(direction, peerBounds, localCursor)
	if err := s.bridge.EnterControl(s.ctx, localCursor); err != nil {
		conn.Close()
		return err
	}
	s.mu.Lock()
	s.controlConn = conn
	s.control = &domain.ControlSession{
		ActivePeerID: peerID,
		Mode:         "controlling",
		StartedAt:    time.Now().UTC(),
	}
	s.controlState = &controlRuntime{
		PeerID:       peerID,
		PeerBounds:   peerBounds,
		EnteredFrom:  direction,
		RemoteCursor: remoteCursor,
	}
	s.mu.Unlock()
	return writeMessage(conn, "control.event", platform.Event{
		Kind: "mouse_move",
		X:    remoteCursor.X,
		Y:    remoteCursor.Y,
	})
}

func (s *Service) forwardControlEvent(event platform.Event) error {
	s.mu.RLock()
	conn := s.controlConn
	state := s.controlState
	s.mu.RUnlock()
	if conn == nil || state == nil {
		return nil
	}

	out := event
	if event.Kind == "mouse_move" {
		next := platform.Point{
			X: state.RemoteCursor.X + event.DeltaX,
			Y: state.RemoteCursor.Y + event.DeltaY,
		}
		if next == state.RemoteCursor {
			return nil
		}
		if s.shouldReturnToLocal(state, next) {
			return s.stopControlSession()
		}
		next = state.PeerBounds.Clamp(next)
		out.X = next.X
		out.Y = next.Y
		s.mu.Lock()
		if s.controlState != nil {
			s.controlState.RemoteCursor = next
		}
		s.mu.Unlock()
	}
	return writeMessage(conn, "control.event", out)
}

func (s *Service) shouldReturnToLocal(state *controlRuntime, next platform.Point) bool {
	switch state.EnteredFrom {
	case "left":
		return next.X < state.PeerBounds.MinX
	case "right":
		return next.X > state.PeerBounds.MaxX()-1
	case "up":
		return next.Y < state.PeerBounds.MinY
	case "down":
		return next.Y > state.PeerBounds.MaxY()-1
	default:
		return false
	}
}

func (s *Service) adjacentPeer(direction string) (string, bool) {
	selfNode, ok := s.layoutNode(s.self.ID)
	if !ok {
		return "", false
	}
	s.mu.RLock()
	peers := make([]domain.PeerState, 0, len(s.peers))
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}
	s.mu.RUnlock()
	for _, peer := range peers {
		if peer.Status != domain.PeerStatusTrusted {
			continue
		}
		node, ok := s.layoutNode(peer.Device.ID)
		if !ok {
			continue
		}
		switch direction {
		case "left":
			if node.X+node.Width == selfNode.X && overlap(node.Y, node.Y+node.Height, selfNode.Y, selfNode.Y+selfNode.Height) {
				return peer.Device.ID, true
			}
		case "right":
			if selfNode.X+selfNode.Width == node.X && overlap(node.Y, node.Y+node.Height, selfNode.Y, selfNode.Y+selfNode.Height) {
				return peer.Device.ID, true
			}
		case "up":
			if node.Y+node.Height == selfNode.Y && overlap(node.X, node.X+node.Width, selfNode.X, selfNode.X+selfNode.Width) {
				return peer.Device.ID, true
			}
		case "down":
			if selfNode.Y+selfNode.Height == node.Y && overlap(node.X, node.X+node.Width, selfNode.X, selfNode.X+selfNode.Width) {
				return peer.Device.ID, true
			}
		}
	}
	return "", false
}

func (s *Service) peerBounds(peerID string) (platform.Rect, error) {
	node, ok := s.layoutNode(peerID)
	if !ok {
		return platform.Rect{}, fmt.Errorf("missing layout for peer %s", peerID)
	}
	return platform.Rect{
		MinX:   float64(node.X),
		MinY:   float64(node.Y),
		Width:  float64(node.Width),
		Height: float64(node.Height),
	}, nil
}

func (s *Service) layoutNode(deviceID string) (domain.LayoutNode, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, node := range s.settings.Layout {
		if node.DeviceID == deviceID {
			return node, true
		}
	}
	return domain.LayoutNode{}, false
}

func (s *Service) entryPointFor(direction string, peerBounds platform.Rect, localCursor platform.Point) platform.Point {
	point := platform.Point{
		X: peerBounds.MinX + (peerBounds.Width / 2),
		Y: peerBounds.MinY + (peerBounds.Height / 2),
	}
	if s.localBounds.Width > 0 {
		normalizedX := (localCursor.X - s.localBounds.MinX) / s.localBounds.Width
		point.X = peerBounds.MinX + normalizedX*peerBounds.Width
	}
	if s.localBounds.Height > 0 {
		normalizedY := (localCursor.Y - s.localBounds.MinY) / s.localBounds.Height
		point.Y = peerBounds.MinY + normalizedY*peerBounds.Height
	}
	switch direction {
	case "left":
		point.X = peerBounds.MaxX() - 2
	case "right":
		point.X = peerBounds.MinX + 2
	case "up":
		point.Y = peerBounds.MaxY() - 2
	case "down":
		point.Y = peerBounds.MinY + 2
	}
	return peerBounds.Clamp(point)
}

func overlap(aStart, aEnd, bStart, bEnd int) bool {
	return aStart < bEnd && bStart < aEnd
}

func (s *Service) ensurePeerLayoutLocked(device domain.DeviceInfo) {
	for i := range s.settings.Layout {
		if s.settings.Layout[i].DeviceID == device.ID {
			if device.ScreenWidth > 0 {
				s.settings.Layout[i].Width = device.ScreenWidth
			}
			if device.ScreenHeight > 0 {
				s.settings.Layout[i].Height = device.ScreenHeight
			}
			return
		}
	}
	s.settings.Layout = append(s.settings.Layout, s.defaultPeerLayoutLocked(device))
}

func (s *Service) defaultPeerLayoutLocked(device domain.DeviceInfo) domain.LayoutNode {
	selfNode := domain.LayoutNode{
		DeviceID: s.self.ID,
		X:        0,
		Y:        0,
		Width:    maxInt(1, s.self.ScreenWidth),
		Height:   maxInt(1, s.self.ScreenHeight),
	}
	for _, node := range s.settings.Layout {
		if node.DeviceID == s.self.ID {
			selfNode = node
			break
		}
	}
	maxRight := selfNode.X + selfNode.Width
	for _, node := range s.settings.Layout {
		if node.X+node.Width > maxRight {
			maxRight = node.X + node.Width
		}
	}
	return domain.LayoutNode{
		DeviceID: device.ID,
		X:        maxRight,
		Y:        selfNode.Y,
		Width:    maxInt(1, device.ScreenWidth),
		Height:   maxInt(1, device.ScreenHeight),
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (s *Service) broadcastLayout(layout []domain.LayoutNode) {
	s.mu.RLock()
	peers := make([]domain.PeerState, 0, len(s.peers))
	for _, peer := range s.peers {
		if peer.Status == domain.PeerStatusTrusted {
			peers = append(peers, peer)
		}
	}
	s.mu.RUnlock()

	update := transport.LayoutUpdate{Nodes: make([]transport.LayoutNodePayload, 0, len(layout))}
	for _, node := range layout {
		update.Nodes = append(update.Nodes, transport.LayoutNodePayload{
			DeviceID: node.DeviceID,
			X:        node.X,
			Y:        node.Y,
			Width:    node.Width,
			Height:   node.Height,
		})
	}

	for _, peer := range peers {
		conn, _, _, err := s.connectPeer(discovery.FormatManualAddress(peer.Device.Addr, peer.Device.Port))
		if err != nil {
			s.log.Printf("layout sync connect failed for %s: %v", peer.Device.Name, err)
			continue
		}
		if err := writeMessage(conn, "layout.update", update); err != nil {
			s.log.Printf("layout sync write failed for %s: %v", peer.Device.Name, err)
		}
		_ = conn.Close()
	}
}

func (s *Service) isStopHotkey(event platform.Event) bool {
	return false
}

func (s *Service) isPanicEscape(event platform.Event) bool {
	if event.Kind != "key_down" {
		return false
	}
	switch runtime.GOOS {
	case "darwin":
		return event.KeyCode == escapeKeyCodeMac
	case "windows":
		return event.KeyCode == escapeKeyCodeWin
	default:
		return false
	}
}

func (s *Service) registerEscapeTap() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if now.Sub(s.lastEscapeAt) <= panicWindow {
		s.lastEscapeAt = time.Time{}
		return true
	}
	s.lastEscapeAt = now
	return false
}
