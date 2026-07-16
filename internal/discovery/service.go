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
	BroadcastPort  = 41090
	Protocol       = "udp4"
	MulticastGroup = "239.255.42.99"
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
	s.wg.Add(3)
	go s.listenBroadcast(ctx)
	go s.listenMulticast(ctx)
	go s.announce(ctx)
	return nil
}

func (s *Service) Wait() {
	s.wg.Wait()
}

func (s *Service) announce(ctx context.Context) {
	defer s.wg.Done()

	broadcastConn, err := net.ListenUDP(Protocol, nil)
	if err != nil {
		s.logf("discovery broadcast socket unavailable: %v", err)
		return
	}
	defer broadcastConn.Close()

	multicastAddr := &net.UDPAddr{IP: net.ParseIP(MulticastGroup), Port: BroadcastPort}
	multicastConn, err := net.DialUDP(Protocol, nil, multicastAddr)
	if err != nil {
		s.logf("discovery multicast socket unavailable: %v", err)
		return
	}
	defer multicastConn.Close()

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
			for _, addr := range directedBroadcastTargets() {
				if _, err := broadcastConn.WriteToUDP(payload, addr); err != nil {
					s.logf("discovery broadcast write failed to %s: %v", addr, err)
				}
			}
			if _, err := multicastConn.Write(payload); err != nil {
				s.logf("discovery multicast write failed: %v", err)
			}
		}
	}
}

func (s *Service) listenBroadcast(ctx context.Context) {
	defer s.wg.Done()

	addr := &net.UDPAddr{IP: net.IPv4zero, Port: BroadcastPort}
	conn, err := net.ListenUDP(Protocol, addr)
	if err != nil {
		s.logf("discovery broadcast listen unavailable: %v", err)
		return
	}
	defer conn.Close()

	s.readLoop(ctx, conn, "broadcast")
}

func (s *Service) listenMulticast(ctx context.Context) {
	defer s.wg.Done()

	group := &net.UDPAddr{IP: net.ParseIP(MulticastGroup), Port: BroadcastPort}
	conn, err := net.ListenMulticastUDP(Protocol, nil, group)
	if err != nil {
		s.logf("discovery multicast listen unavailable: %v", err)
		return
	}
	defer conn.Close()
	_ = conn.SetReadBuffer(256 * 1024)

	s.readLoop(ctx, conn, "multicast")
}

func (s *Service) readLoop(ctx context.Context, conn *net.UDPConn, source string) {
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
			s.logf("discovery %s read failed: %v", source, err)
			continue
		}
		var peer domain.DeviceInfo
		if err := json.Unmarshal(buf[:n], &peer); err != nil {
			s.logf("discovery %s decode failed from %s: %v", source, remote, err)
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

func directedBroadcastTargets() []*net.UDPAddr {
	targets := []*net.UDPAddr{{IP: net.IPv4bcast, Port: BroadcastPort}}
	ifaces, err := net.Interfaces()
	if err != nil {
		return targets
	}
	seen := map[string]bool{net.IPv4bcast.String(): true}
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
			if !ok || ipnet.IP == nil {
				continue
			}
			ip4 := ipnet.IP.To4()
			mask := net.IP(ipnet.Mask).To4()
			if ip4 == nil || mask == nil {
				continue
			}
			broadcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				broadcast[i] = ip4[i] | ^mask[i]
			}
			key := broadcast.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			targets = append(targets, &net.UDPAddr{IP: broadcast, Port: BroadcastPort})
		}
	}
	return targets
}

func FormatManualAddress(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}
