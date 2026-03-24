package common

import (
	"github.com/gorilla/websocket"
)

func RunMessageLoop(conn *websocket.Conn, dispatcher *MessageDispatcher, onReadErr func(error), onDispatchErr func(*Message, error)) {
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
