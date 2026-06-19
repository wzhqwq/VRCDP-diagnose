package diagnose

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const (
	obsRPCVersion               = 1
	obsOpHello                  = 0
	obsOpIdentify               = 1
	obsOpIdentified             = 2
	obsOpEvent                  = 5
	obsEventSubscriptionOutputs = 1 << 6

	obsEventRecordStateChanged = "RecordStateChanged"
	obsOutputStarted           = "OBS_WEBSOCKET_OUTPUT_STARTED"
	obsOutputStopped           = "OBS_WEBSOCKET_OUTPUT_STOPPED"
	obsOutputPaused            = "OBS_WEBSOCKET_OUTPUT_PAUSED"
	obsOutputResumed           = "OBS_WEBSOCKET_OUTPUT_RESUMED"
)

type obsRuntime struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// OBSWorker owns an obs-websocket connection and records OBS recording state
// markers into whichever diagnostics manager is currently attached.
type OBSWorker struct {
	startMu sync.Mutex

	mu      sync.RWMutex
	manager Manager
	runtime *obsRuntime
}

// NewOBSWorker creates an OBS worker. The manager may be nil and can be changed
// later with SetManager.
func NewOBSWorker(manager Manager) *OBSWorker {
	return &OBSWorker{manager: manager}
}

// SetManager changes the diagnostics manager that receives OBS markers. Passing
// nil keeps the OBS connection alive but drops future OBS markers until another
// manager is attached.
func (w *OBSWorker) SetManager(manager Manager) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.manager = manager
}

type obsMessage struct {
	Op   int             `json:"op"`
	Data json.RawMessage `json:"d"`
}

type obsHelloData struct {
	RPCVersion     int                    `json:"rpcVersion"`
	Authentication *obsAuthenticationData `json:"authentication,omitempty"`
}

type obsAuthenticationData struct {
	Challenge string `json:"challenge"`
	Salt      string `json:"salt"`
}

type obsIdentifyData struct {
	RPCVersion         int    `json:"rpcVersion"`
	Authentication     string `json:"authentication,omitempty"`
	EventSubscriptions int    `json:"eventSubscriptions"`
}

type obsEventData struct {
	EventType string          `json:"eventType"`
	EventData json.RawMessage `json:"eventData,omitempty"`
}

type obsRecordStateData struct {
	OutputActive bool    `json:"outputActive"`
	OutputState  string  `json:"outputState"`
	OutputPath   *string `json:"outputPath"`
}

func (w *OBSWorker) Start(ctx context.Context, cfg OBSConnectionConfig) error {
	if w == nil {
		return errors.New("obs worker is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cfg.Host = strings.TrimSpace(cfg.Host)
	if cfg.Host == "" {
		return errors.New("obs websocket host is required")
	}

	w.startMu.Lock()
	defer w.startMu.Unlock()

	w.mu.RLock()
	if w.runtime != nil {
		w.mu.RUnlock()
		return nil
	}
	w.mu.RUnlock()

	conn, reader, err := dialOBSWebSocket(ctx, cfg.Host)
	if err != nil {
		return err
	}

	if err := identifyOBS(reader, conn, cfg.Password); err != nil {
		_ = conn.Close()
		return err
	}

	obsCtx, cancel := context.WithCancel(context.Background())
	rt := &obsRuntime{cancel: cancel, done: make(chan struct{})}

	w.mu.Lock()
	if w.runtime != nil {
		w.mu.Unlock()
		cancel()
		_ = conn.Close()
		return nil
	}
	w.runtime = rt
	w.mu.Unlock()

	go w.run(obsCtx, rt, conn, reader)
	return nil
}

func (w *OBSWorker) Stop(ctx context.Context) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	w.startMu.Lock()
	defer w.startMu.Unlock()

	w.mu.Lock()
	rt := w.runtime
	if rt == nil {
		w.mu.Unlock()
		return nil
	}
	w.runtime = nil
	w.mu.Unlock()

	rt.cancel()
	select {
	case <-rt.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *OBSWorker) run(ctx context.Context, rt *obsRuntime, conn net.Conn, reader *bufio.Reader) {
	defer close(rt.done)
	defer conn.Close()
	defer func() {
		w.mu.Lock()
		if w.runtime == rt {
			w.runtime = nil
		}
		w.mu.Unlock()
	}()

	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	for {
		msg, err := readOBSMessage(reader)
		if err != nil {
			return
		}
		if msg.Op != obsOpEvent {
			continue
		}
		var event obsEventData
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			continue
		}
		if event.EventType == obsEventRecordStateChanged {
			w.recordOBSRecordingMarker(context.Background(), event.EventData)
		}
	}
}

func dialOBSWebSocket(ctx context.Context, host string) (net.Conn, *bufio.Reader, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, err
	}
	stopCloseOnCancel := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopCloseOnCancel:
		}
	}()
	defer close(stopCloseOnCancel)

	key, err := webSocketKey()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	req := "GET / HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + key + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: obswebsocket.json\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		conn.Close()
		return nil, nil, err
	}

	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, nil, fmt.Errorf("obs websocket upgrade returned %s", res.Status)
	}
	if got := res.Header.Get("Sec-WebSocket-Accept"); got != webSocketAccept(key) {
		conn.Close()
		return nil, nil, errors.New("obs websocket returned invalid accept key")
	}
	return conn, reader, nil
}

func identifyOBS(reader *bufio.Reader, conn net.Conn, password string) error {
	msg, err := readOBSMessage(reader)
	if err != nil {
		return err
	}
	if msg.Op != obsOpHello {
		return fmt.Errorf("obs websocket first op = %d, want hello", msg.Op)
	}

	var hello obsHelloData
	if err := json.Unmarshal(msg.Data, &hello); err != nil {
		return err
	}

	identify := obsIdentifyData{
		RPCVersion:         obsRPCVersion,
		EventSubscriptions: obsEventSubscriptionOutputs,
	}
	if hello.Authentication != nil {
		identify.Authentication = obsAuthString(password, hello.Authentication.Salt, hello.Authentication.Challenge)
	}
	if err := writeOBSJSON(conn, obsMessage{Op: obsOpIdentify, Data: mustRawJSON(identify)}); err != nil {
		return err
	}

	msg, err = readOBSMessage(reader)
	if err != nil {
		return err
	}
	if msg.Op != obsOpIdentified {
		return fmt.Errorf("obs websocket identify op = %d, want identified", msg.Op)
	}
	return nil
}

func (w *OBSWorker) recordOBSRecordingMarker(ctx context.Context, raw json.RawMessage) {
	w.mu.RLock()
	manager := w.manager
	w.mu.RUnlock()
	if manager == nil {
		return
	}
	marker, ok := obsRecordingMarker(manager.Now(), raw)
	if !ok {
		return
	}
	_, _ = manager.RecordMarker(ctx, marker)
}

func obsRecordingMarker(now TimePoint, raw json.RawMessage) (MarkerEvent, bool) {
	var event obsRecordStateData
	if err := json.Unmarshal(raw, &event); err != nil {
		return MarkerEvent{}, false
	}

	label := ""
	switch event.OutputState {
	case obsOutputStarted:
		label = "obs_recording_started"
	case obsOutputStopped:
		label = "obs_recording_stopped"
	case obsOutputPaused:
		label = "obs_recording_paused"
	case obsOutputResumed:
		label = "obs_recording_resumed"
	default:
		return MarkerEvent{}, false
	}

	note := "state=" + event.OutputState
	if event.OutputPath != nil && *event.OutputPath != "" {
		note += " path=" + *event.OutputPath
	}
	return MarkerEvent{
		Time:   now,
		Label:  label,
		Note:   note,
		Source: "obs-websocket",
	}, true
}

func obsAuthString(password, salt, challenge string) string {
	secretHash := sha256.Sum256([]byte(password + salt))
	secret := base64.StdEncoding.EncodeToString(secretHash[:])
	authHash := sha256.Sum256([]byte(secret + challenge))
	return base64.StdEncoding.EncodeToString(authHash[:])
}

func readOBSMessage(reader *bufio.Reader) (obsMessage, error) {
	payload, err := readWebSocketFrame(reader)
	if err != nil {
		return obsMessage{}, err
	}
	var msg obsMessage
	if err := json.Unmarshal(payload, &msg); err != nil {
		return obsMessage{}, err
	}
	return msg, nil
}

func writeOBSJSON(w io.Writer, msg obsMessage) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return writeWebSocketText(w, payload)
}

func mustRawJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func readWebSocketFrame(reader *bufio.Reader) ([]byte, error) {
	for {
		var header [2]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			return nil, err
		}
		opcode := header[0] & 0x0f
		masked := header[1]&0x80 != 0
		length := uint64(header[1] & 0x7f)
		switch length {
		case 126:
			var extended [2]byte
			if _, err := io.ReadFull(reader, extended[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(extended[:]))
		case 127:
			var extended [8]byte
			if _, err := io.ReadFull(reader, extended[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(extended[:])
		}

		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(reader, mask[:]); err != nil {
				return nil, err
			}
		}
		if length > 8*1024*1024 {
			return nil, errors.New("obs websocket frame too large")
		}
		payload := make([]byte, int(length))
		if _, err := io.ReadFull(reader, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}

		switch opcode {
		case 0x1:
			return payload, nil
		case 0x8:
			return nil, io.EOF
		case 0x9:
			continue
		default:
			continue
		}
	}
}

func writeWebSocketText(w io.Writer, payload []byte) error {
	var mask [4]byte
	if _, err := rand.Read(mask[:]); err != nil {
		return err
	}

	var frame bytes.Buffer
	frame.WriteByte(0x81)
	length := len(payload)
	switch {
	case length < 126:
		frame.WriteByte(0x80 | byte(length))
	case length <= 65535:
		frame.WriteByte(0x80 | 126)
		_ = binary.Write(&frame, binary.BigEndian, uint16(length))
	default:
		frame.WriteByte(0x80 | 127)
		_ = binary.Write(&frame, binary.BigEndian, uint64(length))
	}
	frame.Write(mask[:])
	for i, b := range payload {
		frame.WriteByte(b ^ mask[i%4])
	}
	_, err := w.Write(frame.Bytes())
	return err
}

func webSocketKey() (string, error) {
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key[:]), nil
}

func webSocketAccept(key string) string {
	sum := sha1.Sum([]byte(strings.TrimSpace(key) + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(sum[:])
}
