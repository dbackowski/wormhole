package common

import (
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/gorilla/websocket"
)

func RunMessageLoop(conn *websocket.Conn, dispatcher *MessageDispatcher, hb Heartbeat, onReadErr func(error), onDispatchErr func(*Message, error)) {
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

		if err := dispatcher.Dispatch(&message); err != nil {
			onDispatchErr(&message, err)
		}
	}
}

// MinWriteRate is the slowest uplink a tunnel write is allowed, in bytes per
// second. A timed-out write leaves a gorilla connection unusable, so a flat
// WriteWait would kill the whole tunnel whenever a large body crosses a slow
// link.
// ponytail: one rate for every link; measure throughput if 2 Mbit/s is too
// generous to catch a dead peer quickly or too tight for real uplinks.
const MinWriteRate = 256 << 10

func writeTimeout(size int) time.Duration {
	return WriteWait + time.Duration(size)*time.Second/MinWriteRate
}

// WriteJSON writes v as one text message, with a deadline sized to fit it.
// Callers must serialize writes.
func WriteJSON(conn *websocket.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout(len(data)))); err != nil {
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
