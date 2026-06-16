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
	"time"
)

const (
	obsWebSocketHost     = "127.0.0.1:4455"
	obsWebSocketPassword = "123456"

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

func (m *diagnosticManager) obsRecordingWorker() {
	defer m.workerWG.Done()

	backoff := time.Second
	for {
		if err := m.observeOBSRecording(); err == nil || m.shutdown.Load() {
			return
		}

		select {
		case <-time.After(backoff):
			if backoff < 10*time.Second {
				backoff *= 2
			}
		case <-m.done:
			return
		}
	}
}

func (m *diagnosticManager) observeOBSRecording() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		select {
		case <-m.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	conn, reader, err := dialOBSWebSocket(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()

	if err := identifyOBS(reader, conn); err != nil {
		return err
	}

	for {
		msg, err := readOBSMessage(reader)
		if err != nil {
			return err
		}
		if msg.Op != obsOpEvent {
			continue
		}
		var event obsEventData
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			continue
		}
		if event.EventType == obsEventRecordStateChanged {
			m.recordOBSRecordingMarker(context.Background(), event.EventData)
		}
	}
}

func dialOBSWebSocket(ctx context.Context) (net.Conn, *bufio.Reader, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", obsWebSocketHost)
	if err != nil {
		return nil, nil, err
	}

	key, err := webSocketKey()
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	req := "GET / HTTP/1.1\r\n" +
		"Host: " + obsWebSocketHost + "\r\n" +
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

func identifyOBS(reader *bufio.Reader, conn net.Conn) error {
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
		identify.Authentication = obsAuthString(obsWebSocketPassword, hello.Authentication.Salt, hello.Authentication.Challenge)
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

func (m *diagnosticManager) recordOBSRecordingMarker(ctx context.Context, raw json.RawMessage) {
	marker, ok := obsRecordingMarker(m.Now(), raw)
	if !ok {
		return
	}
	_, _ = m.RecordMarker(ctx, marker)
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
