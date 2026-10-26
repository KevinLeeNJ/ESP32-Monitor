package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"esp32-monitor/client/internal/platform/proxy"
	"io"
	"os/exec"
	"sync"
	"time"
)

// RPCError deliberately excludes upstream text, which can contain account or task data.
type RPCError struct{ Code int }

func (e *RPCError) Error() string { return "codex_rpc_error" }

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}
type Notification struct {
	Method string
	Params json.RawMessage
}

// RPC multiplexes bounded JSONL responses and notifications. Overflow invalidates
// the session instead of silently losing task completion/approval events.
type RPC struct {
	mu      sync.Mutex
	writeMu sync.Mutex
	next    uint64
	pending map[uint64]chan message
	writer  io.WriteCloser
	reader  io.Closer
	events  chan Notification
	done    chan struct{}
	once    sync.Once
}

func NewRPC(reader io.ReadCloser, writer io.WriteCloser) *RPC {
	r := &RPC{pending: map[uint64]chan message{}, reader: reader, writer: writer, events: make(chan Notification, 128), done: make(chan struct{})}
	go r.read(reader)
	return r
}
func (r *RPC) read(reader io.Reader) {
	defer r.Close()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var m message
		if json.Unmarshal(scanner.Bytes(), &m) != nil {
			return
		}
		if m.Method != "" {
			// This observer never handles approval or credential-refresh requests.
			if len(m.ID) > 0 && string(m.ID) != "null" {
				if r.send(map[string]any{"id": m.ID, "error": map[string]any{"code": -32601, "message": "Read-only observer"}}) != nil {
					return
				}
				continue
			}
			select {
			case r.events <- Notification{m.Method, m.Params}:
			default:
				return
			}
		} else {
			var id uint64
			if json.Unmarshal(m.ID, &id) != nil {
				return
			}
			r.mu.Lock()
			ch := r.pending[id]
			r.mu.Unlock()
			if ch != nil {
				select {
				case ch <- m:
				default:
				}
			}
		}
	}
}
func (r *RPC) send(v any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	select {
	case <-r.done:
		return errors.New("codex_disconnected")
	default:
	}
	if err := json.NewEncoder(r.writer).Encode(v); err != nil {
		r.Close()
		return errors.New("codex_disconnected")
	}
	return nil
}
func (r *RPC) Call(ctx context.Context, method string, params any, out any) error {
	stop := context.AfterFunc(ctx, r.Close)
	defer stop()
	r.mu.Lock()
	r.next++
	id := r.next
	ch := make(chan message, 1)
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	if err := r.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		r.Close()
		return ctx.Err()
	case <-r.done:
		return errors.New("codex_disconnected")
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if len(m.Result) == 0 {
			return errors.New("codex_invalid_response")
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(m.Result, out)
	}
}
func (r *RPC) Initialize(ctx context.Context) (string, error) {
	var out struct {
		UserAgent string `json:"userAgent"`
	}
	err := r.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "esp32_monitor", "title": "ESP32 Monitor", "version": "0.1.0"}}, &out)
	if err != nil {
		return "", err
	}
	if err = r.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		return "", err
	}
	return out.UserAgent, nil
}
func (r *RPC) Events() <-chan Notification { return r.events }
func (r *RPC) Done() <-chan struct{}       { return r.done }
func (r *RPC) Close() {
	r.once.Do(func() { close(r.done); _ = r.reader.Close(); _ = r.writer.Close() })
}

type Connection struct {
	RPC   *RPC
	close func()
}

func (c *Connection) Close() { c.close() }

// Start launches a quota-only App Server. It does NOT attach to the desktop's
// process, list its tasks, resume threads or start any model turns.
func Start(ctx context.Context, executable string) (*Connection, error) {
	env, err := proxy.Default().Environment(ctx)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, executable, "app-server", "--listen", "stdio://")
	cmd.Env = env
	cmd.Stderr = io.Discard // Never forward raw account/config diagnostics to snapshots.
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, errors.New("codex_start_failed")
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		return nil, errors.New("codex_start_failed")
	}
	if err = cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, errors.New("codex_start_failed")
	}
	rpc := NewRPC(out, in)
	done := make(chan struct{})
	// Wait only after the reader has reached EOF (or the connection was closed).
	go func() {
		<-rpc.Done()
		exited := make(chan struct{})
		go func() { _ = cmd.Wait(); close(exited) }()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		select {
		case <-exited:
		case <-timer.C:
			_ = cmd.Process.Kill()
			<-exited
		}
		close(done)
	}()
	var once sync.Once
	return &Connection{rpc, func() { once.Do(func() { rpc.Close(); <-done }) }}, nil
}
