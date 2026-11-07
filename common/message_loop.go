package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

// Handlers maps each message type a side accepts to its handler.
type Handlers map[MessageType]func(*Message) error

func (h Handlers) Dispatch(msg *Message) error {
	handler, ok := h[msg.Type]
	if !ok {
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}
	if err := handler(msg); err != nil {
		return fmt.Errorf("handler for %s failed: %w", msg.Type, err)
	}
	return nil
}

func RunMessageLoop(conn *websocket.Conn, handlers Handlers, hb Heartbeat, onReadErr func(error), onDispatchErr func(*Message, error)) {
	conn.SetReadLimit(MaxWebSocketMessageSize)
	if err := conn.SetReadDeadline(time.Now().Add(hb.PongWait)); err != nil {
		onReadErr(err)
		return
	}
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(hb.PongWait))
	})

	done := make(chan struct{})
	defer close(done)
	go pingLoop(conn, hb, done)

	for {
		var message Message
		if err := conn.ReadJSON(&message); err != nil {
			onReadErr(err)
			return
		}

		if err := handlers.Dispatch(&message); err != nil {
			onDispatchErr(&message, err)
		}
	}
}

// WriteJSON writes v as one text message. Messages stay small, a body chunk
// at most, so one deadline fits all.
// Callers must serialize writes.
func WriteJSON(conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(WriteWait)); err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

func pingLoop(conn *websocket.Conn, hb Heartbeat, done <-chan struct{}) {
	ticker := time.NewTicker(hb.PingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(hb.WriteWait))
			if err == nil {
				continue
			}

			// The connection is already closing gracefully.
			if errors.Is(err, websocket.ErrCloseSent) {
				return
			}

			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}

			conn.Close()
			return
		}
	}
}
