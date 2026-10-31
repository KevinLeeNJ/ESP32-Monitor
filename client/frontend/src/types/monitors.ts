/** ESP32 Monitor device DTOs mirrored from client/internal/transport. */

export interface DeviceStatus {
  identity_verified: boolean;
  device_id: string;
  device_name: string;
  paired: boolean;
  enabled: boolean;
  address: string;
  confirmed_online: boolean;
  connection_state: string;
  session_id: string;
  last_seen: string;
}

export interface DiscoveredDevice {
  device_id: string;
  name: string;
  connection_type: string;
  address: string;
  port: string;
  vid: string;
  pid: string;
  usb_serial: string;
  identity_verified: boolean;
  paired: boolean;
  online: boolean;
  state: string;
}

export interface DiscoveryResult {
  devices: DiscoveredDevice[];
  lan_error?: string;
  serial_error?: string;
  overflow: boolean;
}

export interface PairingStatus {
  device_id: string;
  pairing_id: string;
  code: string;
  state: string;
  expires_in_ms: number;
}
