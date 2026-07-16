package transport

import "time"

type MessageEnvelope struct {
	Type      string      `json:"type"`
	Version   int         `json:"version"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

type PairPayload struct {
	DeviceID     string `json:"deviceId"`
	DeviceName   string `json:"deviceName"`
	ScreenWidth  int    `json:"screenWidth"`
	ScreenHeight int    `json:"screenHeight"`
	Fingerprint  string `json:"fingerprint"`
	PairCode     string `json:"pairCode"`
}

type TransferOffer struct {
	ID           string `json:"id"`
	FileName     string `json:"fileName"`
	BytesTotal   int64  `json:"bytesTotal"`
	Archive      bool   `json:"archive"`
	Directory    bool   `json:"directory"`
	OriginalName string `json:"originalName"`
}

type TransferDecision struct {
	ID       string `json:"id"`
	Accepted bool   `json:"accepted"`
	Reason   string `json:"reason,omitempty"`
}

type TransferChunk struct {
	ID    string `json:"id"`
	Index int    `json:"index"`
	Data  []byte `json:"data"`
	Done  bool   `json:"done"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}

type Heartbeat struct {
	DeviceID string `json:"deviceId"`
}

type LayoutUpdate struct {
	Nodes []LayoutNodePayload `json:"nodes"`
}

type LayoutNodePayload struct {
	DeviceID string `json:"deviceId"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
}

type ControlEnter struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
}

type ControlLeave struct {
	DeviceID string `json:"deviceId"`
}
