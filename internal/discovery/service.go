package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"runtime"
	"sync"
	"time"

	"mouseshare/internal/domain"
)

const (
	BroadcastPort = 41090
	Protocol      = "udp4"
)

type Service struct {
	self   domain.DeviceInfo
	onPeer func(domain.DeviceInfo)
	logf   func(string, ...any)
	wg     sync.WaitGroup
}

func New(self domain.DeviceInfo, onPeer func(domain.DeviceInfo), logf func(string, ...any)) *Service {
	return &Service{self: self, onPeer: onPeer, logf: logf}
}

func (s *Service) Start(ctx context.Context) error {
	s.wg.Add(2)
	go s.listen(ctx)
	go s.broadcast(ctx)
	return nil
}

func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) broadcast(ctx context.Context) {
	defer s.wg.Done()

	addr := &net.UDPAddr{IP: net.IPv4bcast, Port: BroadcastPort}
	conn, err := net.ListenUDP(Protocol, nil)
	if err != nil {
		s.logf("discovery broadcast unavailable: %v", err)
		return
	}
	defer conn.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.self.SeenAt = time.Now().UTC()
			s.self.OS = runtime.GOOS
			payload, err := json.Marshal(s.self)
			if err != nil {
				continue
			}
			_, _ = conn.WriteToUDP(payload, addr)
		}
	}
}

func (s *Service) listen(ctx context.Context) {
	defer s.wg.Done()

	addr := &net.UDPAddr{IP: net.IPv4zero, Port: BroadcastPort}
	conn, err := net.ListenUDP(Protocol, addr)
	if err != nil {
		s.logf("discovery listen unavailable: %v", err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 64*1024)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					return
				default:
					continue
				}
			}
			s.logf("discovery read failed: %v", err)
			continue
		}
		var peer domain.DeviceInfo
		if err := json.Unmarshal(buf[:n], &peer); err != nil {
			continue
		}
		if peer.ID == "" || peer.ID == s.self.ID {
			continue
		}
		if peer.Addr == "" {
			peer.Addr = remote.IP.String()
		}
		if peer.Port == 0 {
			peer.Port = 41091
		}
		peer.SeenAt = time.Now().UTC()
		s.onPeer(peer)
	}
}

func FormatManualAddress(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
