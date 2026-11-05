// Package windowshelper owns the shared, supervised music/sensors JSONL process.
// Consumers never own the process: cancelling a read or music subscription must
// not tear down the other channel.
package windowshelper

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

var (
	ErrMissing  = errors.New("windows_helper_missing")
	ErrClosed   = errors.New("windows_helper_closed")
	ErrExited   = errors.New("windows_helper_exited")
	ErrProtocol = errors.New("windows_helper_protocol_error")
	ErrTimeout  = errors.New("sensors_timeout")
	ErrBusy     = errors.New("sensors_busy")
)

// Command must return exec.CommandContext bound to the supplied context.
type Command func(context.Context) (*exec.Cmd, error)
type result struct {
	data json.RawMessage
	err  error
}
type pending struct {
	reply chan result
	timer *time.Timer
}
type session struct {
	cancel  context.CancelFunc
	writes  chan []byte
	pending map[string]pending
}

type Client struct {
	mu                 sync.Mutex
	once               sync.Once
	ctx                context.Context
	cancel             context.CancelFunc
	done               chan struct{}
	command            Command
	session            *session
	changed            chan struct{}
	subs               map[chan json.RawMessage]struct{}
	latest             json.RawMessage
	lastErr            error
	nextID             uint64
	requestTimeout     time.Duration
	retryMin, retryMax time.Duration
}

// New is lazy; the first sensor request or music subscription starts the helper.
// Close belongs to the Application (or to a standalone consumer that created it).
func New(command Command) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{ctx: ctx, cancel: cancel, done: make(chan struct{}), command: command,
		changed: make(chan struct{}), subs: make(map[chan json.RawMessage]struct{}),
		requestTimeout: 12 * time.Second, retryMin: 2 * time.Second, retryMax: time.Minute}
}

func (c *Client) start() { c.once.Do(func() { go c.run() }) }
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.cancel()
	c.start()
	<-c.done
}

// SubscribeMusic returns latest-value delivery, not an unbounded event queue.
// A slow subscriber cannot delay sensor responses. Cancel unregisters only it.
func (c *Client) SubscribeMusic() (<-chan json.RawMessage, func()) {
	ch := make(chan json.RawMessage, 1)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	if c.latest != nil {
		ch <- append(json.RawMessage(nil), c.latest...)
	}
	if len(c.subs) == 1 && c.session != nil {
		c.controlLocked("music.subscribe")
	}
	c.mu.Unlock()
	c.start()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			delete(c.subs, ch)
			if len(c.subs) == 0 {
				c.latest = nil
				if c.session != nil {
					c.controlLocked("music.unsubscribe")
				}
			}
		})
	}
}

func (c *Client) nextLocked() string { c.nextID++; return strconv.FormatUint(c.nextID, 10) }
func (c *Client) controlLocked(method string) {
	line, _ := json.Marshal(map[string]string{"channel": "music", "id": c.nextLocked(), "method": method})
	select {
	case c.session.writes <- line:
	default:
		c.session.cancel()
	}
}
func (c *Client) publishLocked(line json.RawMessage) {
	c.latest = append(json.RawMessage(nil), line...)
	for ch := range c.subs {
		select {
		case <-ch:
		default:
		}
		ch <- append(json.RawMessage(nil), line...)
	}
}
func (c *Client) signalLocked() { close(c.changed); c.changed = make(chan struct{}) }

func (c *Client) ReadSensors(ctx context.Context) (json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, c.requestTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.start()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		if c.ctx.Err() != nil {
			c.mu.Unlock()
			return nil, ErrClosed
		}
		s := c.session
		if s == nil {
			if c.lastErr != nil {
				err := c.lastErr
				c.mu.Unlock()
				return nil, err
			}
			changed := c.changed
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-c.ctx.Done():
				return nil, ErrClosed
			case <-changed:
			}
			continue
		}
		if len(s.pending) >= 64 {
			c.mu.Unlock()
			return nil, ErrBusy
		}
		id := c.nextLocked()
		reply := make(chan result, 1)
		line, _ := json.Marshal(map[string]string{"channel": "sensors", "id": id, "method": "sensors.read"})
		select {
		case s.writes <- line:
			// Keep the deadline even if the caller cancels. A wedged helper
			// must recover, but a cancelled short-lived collector generation
			// must not kill a healthy shared process.
			timer := time.AfterFunc(c.requestTimeout, func() {
				c.mu.Lock()
				defer c.mu.Unlock()
				if c.session == s {
					if p, ok := s.pending[id]; ok {
						delete(s.pending, id)
						p.reply <- result{err: ErrTimeout}
						s.cancel()
					}
				}
			})
			s.pending[id] = pending{reply: reply, timer: timer}
			c.mu.Unlock()
		default:
			c.mu.Unlock()
			return nil, ErrBusy
		}
		select {
		case r := <-reply:
			return r.data, r.err
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.ctx.Done():
			return nil, ErrClosed
		}
	}
}

func (c *Client) run() {
	defer close(c.done)
	delay := c.retryMin
	for c.ctx.Err() == nil {
		started := time.Now()
		err := c.runSession()
		c.mu.Lock()
		if s := c.session; s != nil {
			for _, p := range s.pending {
				p.timer.Stop()
				p.reply <- result{err: err}
			}
			c.session = nil
		}
		c.lastErr = err
		code := "music_bridge_exited"
		if errors.Is(err, ErrMissing) {
			code = "music_bridge_missing"
		}
		if errors.Is(err, ErrProtocol) {
			code = "music_protocol_error"
		}
		if c.ctx.Err() != nil {
			code = "music_cancelled"
		}
		line, _ := json.Marshal(map[string]any{"channel": "music", "id": "", "current": nil, "error_code": code})
		c.publishLocked(line)
		c.signalLocked()
		c.mu.Unlock()
		if time.Since(started) >= time.Minute {
			delay = c.retryMin
		}
		timer := time.NewTimer(delay)
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, c.retryMax)
	}
}

func (c *Client) runSession() error {
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	cmd, err := c.command(ctx)
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stdout.Close()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer stdin.Close()
	// EOF allows Computer.Close on normal shutdown; a stuck native worker is
	// still forcibly reaped by CommandContext's bounded WaitDelay.
	cmd.Cancel = func() error { return stdin.Close() }
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	s := &session{cancel: cancel, writes: make(chan []byte, 64), pending: make(map[string]pending)}
	c.mu.Lock()
	c.session = s
	c.lastErr = nil
	if len(c.subs) > 0 {
		c.controlLocked("music.subscribe")
	}
	c.signalLocked()
	c.mu.Unlock()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			select {
			case <-ctx.Done():
				return
			case line := <-s.writes:
				if _, err := stdin.Write(append(line, '\n')); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	readDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			if !c.dispatch(s, scanner.Bytes()) {
				readDone <- ErrProtocol
				return
			}
		}
		if scanner.Err() != nil {
			readDone <- ErrProtocol
		} else {
			readDone <- ErrExited
		}
	}()
	select {
	case err = <-readDone:
		cancel()
	case <-ctx.Done():
		err = ErrExited
		_ = stdout.Close()
		<-readDone
	}
	cancel()
	_ = cmd.Wait()
	_ = stdin.Close()
	<-writerDone
	return err
}

func (c *Client) dispatch(s *session, line []byte) bool {
	var m struct {
		Channel string          `json:"channel"`
		ID      *string         `json:"id"`
		Current json.RawMessage `json:"current"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error_code"`
	}
	if json.Unmarshal(line, &m) != nil || m.ID == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session != s {
		return true
	}
	switch m.Channel {
	case "music":
		if *m.ID != "" || (m.Current == nil && m.Error == "") {
			return false
		}
		if len(c.subs) > 0 {
			c.publishLocked(line)
		}
	case "sensors":
		if *m.ID == "" || ((m.Data == nil || string(m.Data) == "null") && m.Error == "") {
			return false
		}
		if p, ok := s.pending[*m.ID]; ok {
			delete(s.pending, *m.ID)
			p.timer.Stop()
			var err error
			if m.Error != "" {
				err = errors.New(m.Error)
			}
			if m.Error == "sensors_timeout" {
				err = ErrTimeout
				s.cancel()
			}
			p.reply <- result{data: append(json.RawMessage(nil), m.Data...), err: err}
		}
	default:
		return false
	}
	return true
}
