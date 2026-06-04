package common

import (
	"time"

	"github.com/gorilla/websocket"
)

func RunMessageLoop(conn *websocket.Conn, dispatcher *MessageDispatcher, hb Heartbeat, onReadErr func(error), onDispatchErr func(*Message, error)) {
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

func pingLoop(conn *websocket.Conn, hb Heartbeat, done <-chan struct{}) {
	ticker := time.NewTicker(hb.PingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			deadline := time.Now().Add(hb.WriteWait)
			if err := conn.WriteControl(websocket.PingMessage, nil, deadline); err != nil {
				return
			}
		}
	}
}
