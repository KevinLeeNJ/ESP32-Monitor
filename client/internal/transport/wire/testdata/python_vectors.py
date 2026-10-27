"""Reproduce vectors independently of Go (protobuf 6.33.6, protoc 31.1).

From client/ with Python protobuf installed:
  protoc -I . --python_out=/tmp internal/transport/wire/companion.proto
  PYTHONPATH=/tmp python3 internal/transport/wire/testdata/python_vectors.py
"""
from internal.transport.wire import companion_pb2 as p

ack = p.Envelope(wire_version=2, ack=p.Ack(
    protocol_version=1, session_id="session", ack_seq=42, status="accepted"))
heartbeat = p.Envelope(wire_version=2, heartbeat=p.Heartbeat(
    protocol_version=1, host_id="host", device_id="device", session_id="session",
    seq=42, sent_at=1792972800, config_revision=0,
    time=p.HeartbeatTime(utc_unix_ms=1792972800123, utc_offset_minutes=-480,
                         trusted=False)))
for message in (ack, heartbeat):
    print(message.SerializeToString(deterministic=True).hex())
