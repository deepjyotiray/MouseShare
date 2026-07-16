package domain

import "time"

type DeviceInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	OS          string    `json:"os"`
	Addr        string    `json:"addr"`
	Port        int       `json:"port"`
	Fingerprint string    `json:"fingerprint"`
	PairCode    string    `json:"pairCode"`
	Version     string    `json:"version"`
	SeenAt      time.Time `json:"seenAt"`
}

type PeerStatus string

const (
	PeerStatusPending  PeerStatus = "pending"
	PeerStatusTrusted  PeerStatus = "trusted"
	PeerStatusRejected PeerStatus = "rejected"
	PeerStatusOffline  PeerStatus = "offline"
)

type PermissionState struct {
	Accessibility bool     `json:"accessibility"`
	InputCapture  bool     `json:"inputCapture"`
	ScreenAccess  bool     `json:"screenAccess"`
	Warnings      []string `json:"warnings"`
}

type LayoutNode struct {
	DeviceID string `json:"deviceId"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type PeerState struct {
	Device         DeviceInfo  `json:"device"`
	Status         PeerStatus  `json:"status"`
	ApprovedAt     *time.Time  `json:"approvedAt,omitempty"`
	LastError      string      `json:"lastError,omitempty"`
	Layout         *LayoutNode `json:"layout,omitempty"`
	LastTransferAt *time.Time  `json:"lastTransferAt,omitempty"`
}

type ControlSession struct {
	ActivePeerID string    `json:"activePeerId"`
	Mode         string    `json:"mode"`
	StartedAt    time.Time `json:"startedAt"`
}

type PairRequest struct {
	PeerID    string `json:"peerId"`
	PeerAddr  string `json:"peerAddr"`
	PairCode  string `json:"pairCode"`
	Approved  bool   `json:"approved"`
	Requested bool   `json:"requested"`
}

type TransferStatus string

const (
	TransferQueued     TransferStatus = "queued"
	TransferOffering   TransferStatus = "offering"
	TransferAwaiting   TransferStatus = "awaiting_acceptance"
	TransferInProgress TransferStatus = "in_progress"
	TransferComplete   TransferStatus = "complete"
	TransferRejected   TransferStatus = "rejected"
	TransferFailed     TransferStatus = "failed"
)

type TransferJob struct {
	ID          string         `json:"id"`
	PeerID      string         `json:"peerId"`
	Direction   string         `json:"direction"`
	FileName    string         `json:"fileName"`
	BytesTotal  int64          `json:"bytesTotal"`
	BytesDone   int64          `json:"bytesDone"`
	Status      TransferStatus `json:"status"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	DownloadDir string         `json:"downloadDir,omitempty"`
}

type AppState struct {
	Self          DeviceInfo        `json:"self"`
	Permissions   PermissionState   `json:"permissions"`
	Peers         []PeerState       `json:"peers"`
	Layout        []LayoutNode      `json:"layout"`
	Transfers     []TransferJob     `json:"transfers"`
	Control       *ControlSession   `json:"control,omitempty"`
	PendingPair   *PairRequest      `json:"pendingPair,omitempty"`
	TrustedPeers  map[string]string `json:"trustedPeers"`
	ListenAddr    string            `json:"listenAddr"`
	ManualPairURL string            `json:"manualPairUrl"`
}
