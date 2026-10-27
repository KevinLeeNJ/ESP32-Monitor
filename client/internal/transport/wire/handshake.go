package wire

type Hello struct {
	ProtocolVersion   int     `json:"protocol_version"`
	Step              string  `json:"step"`
	HostID            string  `json:"host_id"`
	HostName          string  `json:"host_name"`
	HostNonce         string  `json:"host_nonce"`
	SupportedVersions []int   `json:"supported_versions"`
	DeviceID          string  `json:"device_id,omitempty"`
	DeviceName        string  `json:"device_name,omitempty"`
	FirmwareVersion   string  `json:"firmware_version,omitempty"`
	DeviceNonce       string  `json:"device_nonce,omitempty"`
	PairingID         *string `json:"pairing_id,omitempty"`
	MaxPayloadBytes   int     `json:"max_payload_bytes,omitempty"`
}
type Auth struct {
	ProtocolVersion int     `json:"protocol_version"`
	Step            string  `json:"step"`
	HostID          string  `json:"host_id"`
	DeviceID        string  `json:"device_id"`
	PairingID       string  `json:"pairing_id"`
	SessionID       string  `json:"session_id"`
	HostNonce       string  `json:"host_nonce"`
	DeviceNonce     string  `json:"device_nonce"`
	Proof           *string `json:"proof"`
	Accepted        *bool   `json:"accepted"`
}
type EncryptedConfig struct {
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	RequestID       string `json:"request_id"`
	Algorithm       string `json:"algorithm"`
	Nonce           []byte `json:"nonce"`
	Ciphertext      []byte `json:"ciphertext"`
	Tag             []byte `json:"tag"`
}

type Capabilities struct {
	ProtocolVersion   int              `json:"protocol_version"`
	SupportedVersions []int            `json:"supported_versions"`
	HostID            string           `json:"host_id"`
	HostName          string           `json:"host_name"`
	AppVersion        string           `json:"app_version"`
	PairingOpen       bool             `json:"pairing_open"`
	Transports        []string         `json:"transports"`
	Features          []string         `json:"features"`
	MaxSnapshotBytes  int              `json:"max_snapshot_bytes"`
	MQTT              MQTTCapabilities `json:"mqtt"`
}
type MQTTCapabilities struct {
	Protocol            string `json:"protocol"`
	Port                int    `json:"port"`
	TLS                 bool   `json:"tls"`
	KeepAliveSeconds    int    `json:"keep_alive_seconds"`
	IdleHeartbeatMS     int    `json:"idle_heartbeat_ms"`
	LiveHeartbeatMS     int    `json:"live_heartbeat_ms"`
	DisconnectTimeoutMS int    `json:"disconnect_timeout_ms"`
}
