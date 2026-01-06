package common

import (
	"fmt"
)

type MessageHandler func(msg *Message)

type MessageDispatcher struct {
	handlers map[MessageType]MessageHandler
}

func NewMessageDispatcher() *MessageDispatcher {
	return &MessageDispatcher{
		handlers: make(map[MessageType]MessageHandler),
	}
}

func (md *MessageDispatcher) Register(messageType MessageType, handler MessageHandler) {
	md.handlers[messageType] = handler
}

func (md *MessageDispatcher) Dispatch(msg *Message) error {
	handler, exists := md.handlers[msg.Type]
	if !exists {
		return fmt.Errorf("unknown message type: %s", msg.Type)
	}

	handler(msg)
	return nil
}
