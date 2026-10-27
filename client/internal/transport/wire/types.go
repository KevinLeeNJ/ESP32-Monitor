package wire

type Error struct {
	ProtocolVersion int         `json:"protocol_version"`
	RequestID       *string     `json:"request_id"`
	Error           ErrorDetail `json:"error"`
}
type ErrorDetail struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMS *int   `json:"retry_after_ms"`
}

func Failure(code string, id *string) Error { return Error{1, id, ErrorDetail{code, code, false, nil}} }

type Discovery struct {
	ProtocolVersion int     `json:"protocol_version"`
	Type            string  `json:"type"`
	RequestID       string  `json:"request_id"`
	HostID          string  `json:"host_id"`
	DeviceID        *string `json:"device_id"`
	PairingID       *string `json:"pairing_id"`
	Nonce           string  `json:"nonce"`
	ResponseNonce   *string `json:"response_nonce"`
	HostPort        *int    `json:"host_port"`
	MQTTPort        *int    `json:"mqtt_port"`
	SessionID       *string `json:"session_id"`
	DeviceName      *string `json:"device_name"`
	FirmwareVersion *string `json:"firmware_version"`
	PairingOpen     *bool   `json:"pairing_open"`
	MAC             *string `json:"mac"`
}
type PairRequest struct {
	ProtocolVersion int    `json:"protocol_version"`
	Step            string `json:"step"`
	RequestID       string `json:"request_id"`
	DeviceID        string `json:"device_id,omitempty"`
	DeviceName      string `json:"device_name,omitempty"`
	DeviceNonce     string `json:"device_nonce,omitempty"`
	PairingID       string `json:"pairing_id,omitempty"`
	PairingTicket   string `json:"pairing_ticket,omitempty"`
	DeviceConfirmed bool   `json:"device_confirmed,omitempty"`
}
type PairResponse struct {
	ProtocolVersion int     `json:"protocol_version"`
	PairingID       string  `json:"pairing_id"`
	State           string  `json:"state"`
	HostID          string  `json:"host_id"`
	HostName        string  `json:"host_name"`
	HostNonce       string  `json:"host_nonce"`
	PairingTicket   string  `json:"pairing_ticket"`
	ExpiresInMS     int64   `json:"expires_in_ms"`
	DeviceToken     *string `json:"device_token"`
	SerialKey       *string `json:"serial_key"`
}
type Open struct {
	ProtocolVersion int    `json:"protocol_version"`
	RequestID       string `json:"request_id"`
	PairingID       string `json:"pairing_id"`
	DeviceID        string `json:"device_id"`
	DeviceName      string `json:"device_name"`
	FirmwareVersion string `json:"firmware_version"`
	Transport       string `json:"transport"`
}
type Session struct {
	ProtocolVersion     int    `json:"protocol_version"`
	HostID              string `json:"host_id"`
	DeviceID            string `json:"device_id"`
	PairingID           string `json:"pairing_id"`
	SessionID           string `json:"session_id"`
	Transport           string `json:"transport"`
	HeartbeatIntervalMS int    `json:"heartbeat_interval_ms"`
	DisconnectTimeoutMS int    `json:"disconnect_timeout_ms"`
	ConfigRequired      bool   `json:"config_required"`
}
type Control struct {
	ProtocolVersion int     `json:"protocol_version"`
	HostID          string  `json:"host_id"`
	DeviceID        string  `json:"device_id"`
	SessionID       string  `json:"session_id"`
	Seq             uint32  `json:"seq"`
	SentAt          int64   `json:"sent_at"`
	RequestID       *string `json:"request_id"`
	Data            []byte  `json:"data"`
}
type Config struct {
	ProtocolVersion int           `json:"protocol_version"`
	RequestID       string        `json:"request_id"`
	DeviceID        string        `json:"device_id"`
	SessionID       string        `json:"session_id"`
	Epoch           string        `json:"config_epoch"`
	Revision        uint32        `json:"config_revision"`
	Weather         WeatherConfig `json:"weather"`
	Devices         DevicesConfig `json:"devices"`
	Time            TimeConfig    `json:"time"`
}
type WeatherConfig struct {
	Enabled      bool     `json:"enabled"`
	LocationMode string   `json:"location_mode"`
	Latitude     *float64 `json:"latitude"`
	Longitude    *float64 `json:"longitude"`
	Host         *string  `json:"qweather_host"`
	Key          *string  `json:"qweather_key"`
}
type DevicesConfig struct {
	Enabled bool    `json:"enabled"`
	URL     *string `json:"devices_api_url"`
	Token   *string `json:"devices_token"`
}
type TimeConfig struct {
	Enabled bool `json:"enabled"`
	Offset  int  `json:"utc_offset_minutes"`
}
type ConfigAck struct {
	ProtocolVersion   int     `json:"protocol_version"`
	RequestID         *string `json:"request_id"`
	DeviceID          string  `json:"device_id"`
	SessionID         string  `json:"session_id"`
	Epoch             *string `json:"config_epoch"`
	Revision          *uint32 `json:"config_revision"`
	State             string  `json:"state"`
	WeatherConfigured bool    `json:"weather_configured"`
	DevicesConfigured bool    `json:"devices_configured"`
}
type Ack struct {
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	AckSeq          uint32 `json:"ack_seq"`
	Status          string `json:"status"`
}
type Goodbye struct {
	ProtocolVersion int    `json:"protocol_version"`
	SessionID       string `json:"session_id"`
	Reason          string `json:"reason"`
}
type Presence struct {
	ProtocolVersion int    `json:"protocol_version"`
	HostID          string `json:"host_id"`
	DeviceID        string `json:"device_id"`
	SessionID       string `json:"session_id"`
	State           string `json:"state"`
	Reason          string `json:"reason"`
}
type Resync struct {
	LastSnapshotSeq *uint32 `json:"last_snapshot_seq"`
	Reason          string  `json:"reason"`
}
type Health struct {
	State         string  `json:"state"`
	ObservedAt    *int64  `json:"observed_at"`
	LastSuccessAt *int64  `json:"last_success_at"`
	ErrorCode     *string `json:"error_code"`
	SourceVersion *string `json:"source_version"`
	AgeMS         *int64  `json:"age_ms"`
	StaleAfterMS  int64   `json:"stale_after_ms"`
	ExpireAfterMS int64   `json:"expire_after_ms"`
}
type Section struct {
	Health
	Data any `json:"data"`
}
type Sections struct {
	GitHub Section `json:"github"`
	Codex  Section `json:"codex"`
	Music  Section `json:"music"`
	Proxy  Section `json:"proxy"`
}
type Snapshot struct {
	ProtocolVersion int      `json:"protocol_version"`
	HostID          string   `json:"host_id"`
	DeviceID        string   `json:"device_id"`
	SessionID       string   `json:"session_id"`
	Seq             uint32   `json:"seq"`
	SentAt          int64    `json:"sent_at"`
	Epoch           string   `json:"config_epoch"`
	Revision        uint32   `json:"config_revision"`
	Sections        Sections `json:"sections"`
}
type TimeData struct {
	UnixMS  int64  `json:"utc_unix_ms"`
	Offset  int    `json:"utc_offset_minutes"`
	Trusted bool   `json:"trusted"`
	Source  string `json:"source"`
}
type ProxyData struct {
	Epoch    string  `json:"config_epoch"`
	Revision uint32  `json:"config_revision"`
	Time     Section `json:"time"`
	Weather  Section `json:"weather"`
	AQI      Section `json:"aqi"`
	Devices  Section `json:"devices"`
}
type SourceLease struct {
	Path          string `json:"path"`
	SnapshotSeq   uint32 `json:"snapshot_seq"`
	ObservedAt    int64  `json:"observed_at"`
	LastSuccessAt int64  `json:"last_success_at"`
	AgeMS         int64  `json:"age_ms"`
}
type Heartbeat struct {
	ProtocolVersion int            `json:"protocol_version"`
	HostID          string         `json:"host_id"`
	DeviceID        string         `json:"device_id"`
	SessionID       string         `json:"session_id"`
	Seq             uint32         `json:"seq"`
	SentAt          int64          `json:"sent_at"`
	LastSnapshotSeq *uint32        `json:"last_snapshot_seq"`
	Epoch           *string        `json:"config_epoch"`
	Revision        *uint32        `json:"config_revision"`
	Time            *HeartbeatTime `json:"time,omitempty"`
	SourceUpdates   []SourceLease  `json:"source_updates,omitempty"`
}
type HeartbeatTime struct {
	UnixMS  int64 `json:"utc_unix_ms"`
	Offset  int   `json:"utc_offset_minutes"`
	Trusted bool  `json:"trusted"`
}
type DeviceSummary struct {
	Count   int          `json:"device_count"`
	Devices []DeviceCard `json:"devices"`
}
type DeviceCard struct {
	Name        string   `json:"name"`
	Uptime      string   `json:"uptime"`
	MetricCount int      `json:"metric_count"`
	Metrics     []Metric `json:"metrics"`
}
type Metric struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Percent *int   `json:"percent"`
}

func Ptr[T any](v T) *T { return &v }
