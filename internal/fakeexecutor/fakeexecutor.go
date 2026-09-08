package fakeexecutor

import (
	"context"
	"crypto/rand"
	"fmt"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/abn/relay/internal/protocol"
)

func generateUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type Options struct {
	URL                 string
	AppName             string
	ConductorKey        string
	ExecutorID          string
	ApplicationVersion  string
	DBOSVersion         string
	Language            string
	Hostname            string
	ExecutorMetadata    map[string]any
	IgnorePings         bool
	ResponseDelay       time.Duration
	DropRequests        bool
	CloseAfterHandshake bool
	CloseMidRequest     bool
	SendMalformedJSON   bool
}

type FakeExecutor struct {
	opts     Options
	conn     *websocket.Conn
	handlers map[protocol.MessageType]func(protocol.Message) (protocol.Message, error)
	mu       sync.RWMutex
}

func New(opts Options) *FakeExecutor {
	if opts.ExecutorID == "" {
		opts.ExecutorID = generateUUID()
	}
	if opts.ApplicationVersion == "" {
		opts.ApplicationVersion = "v1.0.0"
	}
	if opts.DBOSVersion == "" {
		opts.DBOSVersion = "0.1.0"
	}
	if opts.Language == "" {
		opts.Language = "go"
	}
	if opts.Hostname == "" {
		opts.Hostname = "localhost"
	}

	return &FakeExecutor{
		opts:     opts,
		handlers: make(map[protocol.MessageType]func(protocol.Message) (protocol.Message, error)),
	}
}

func (fe *FakeExecutor) SetHandler(msgType protocol.MessageType, handler func(protocol.Message) (protocol.Message, error)) {
	fe.mu.Lock()
	defer fe.mu.Unlock()
	fe.handlers[msgType] = handler
}

func (fe *FakeExecutor) Connect(ctx context.Context) error {
	dialURL := fmt.Sprintf("%s/websocket/%s/%s", fe.opts.URL, fe.opts.AppName, fe.opts.ConductorKey)

	dialOpts := &websocket.DialOptions{
		OnPingReceived: func(ctx context.Context, payload []byte) bool {
			return !fe.opts.IgnorePings
		},
	}
	conn, _, err := websocket.Dial(ctx, dialURL, dialOpts)
	if err != nil {
		return fmt.Errorf("failed to dial: %w", err)
	}
	fe.conn = conn

	// Send executor_info
	hostname := fe.opts.Hostname
	info := &protocol.ExecutorInfoResponse{
		Envelope: protocol.Envelope{
			Type:      protocol.MessageTypeExecutorInfo,
			RequestID: generateUUID(),
		},
		ExecutorID:         fe.opts.ExecutorID,
		ApplicationVersion: fe.opts.ApplicationVersion,
		Hostname:           &hostname,
		DBOSVersion:        fe.opts.DBOSVersion,
		Language:           fe.opts.Language,
		ExecutorMetadata:   fe.opts.ExecutorMetadata,
	}

	data, err := protocol.Encode(info)
	if err != nil {
		_ = fe.conn.Close(websocket.StatusInternalError, "encode error")
		return fmt.Errorf("failed to encode executor_info: %w", err)
	}

	if err := fe.conn.Write(ctx, websocket.MessageText, data); err != nil {
		_ = fe.conn.Close(websocket.StatusInternalError, "write error")
		return fmt.Errorf("failed to write executor_info: %w", err)
	}

	if fe.opts.CloseAfterHandshake {
		_ = fe.conn.Close(websocket.StatusNormalClosure, "close after handshake")
		return nil
	}

	return nil
}

func (fe *FakeExecutor) Run(ctx context.Context) error {
	if fe.conn == nil {
		return fmt.Errorf("not connected")
	}

	for {
		typ, data, err := fe.conn.Read(ctx)
		if err != nil {
			return err
		}

		if typ != websocket.MessageText {
			continue
		}

		if fe.opts.DropRequests {
			continue
		}

		if fe.opts.CloseMidRequest {
			_ = fe.conn.Close(websocket.StatusNormalClosure, "close mid request")
			return nil
		}

		msg, err := protocol.Decode(data)
		if err != nil {
			continue // ignore malformed messages from relay or log them
		}

		if fe.opts.ResponseDelay > 0 {
			time.Sleep(fe.opts.ResponseDelay)
		}

		fe.mu.RLock()
		handler, ok := fe.handlers[msg.GetMessageType()]
		fe.mu.RUnlock()

		var resp protocol.Message
		if ok {
			resp, err = handler(msg)
			if err != nil {
				// We don't have a generic error response, just continue or handle
				continue
			}
		} else {
			resp = fe.defaultResponse(msg)
		}

		if resp == nil {
			continue
		}

		if fe.opts.SendMalformedJSON {
			_ = fe.conn.Write(ctx, websocket.MessageText, []byte("{ malformed json }"))
			continue
		}

		respData, err := protocol.Encode(resp)
		if err != nil {
			continue
		}

		_ = fe.conn.Write(ctx, websocket.MessageText, respData)
	}
}

func (fe *FakeExecutor) defaultResponse(msg protocol.Message) protocol.Message {
	switch msg.GetMessageType() {
	case protocol.MessageTypeListWorkflows:
		return &protocol.ListWorkflowsResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeListWorkflows,
				RequestID: msg.GetRequestID(),
			},
			Output: []protocol.ListWorkflowsResponseBody{},
		}
	case protocol.MessageTypeGetWorkflow:
		return &protocol.GetWorkflowResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeGetWorkflow,
				RequestID: msg.GetRequestID(),
			},
		}
	case protocol.MessageTypeRecovery:
		return &protocol.RecoveryResponse{
			Envelope: protocol.Envelope{
				Type:      protocol.MessageTypeRecovery,
				RequestID: msg.GetRequestID(),
			},
			Success: true,
		}
	default:
		return nil
	}
}

func (fe *FakeExecutor) Close() error {
	if fe.conn != nil {
		return fe.conn.Close(websocket.StatusNormalClosure, "closing")
	}
	return nil
}
