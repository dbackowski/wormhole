package client

import "github.com/dbackowski/wormhole/common"

type RequestLogger struct {
	logger *common.Logger
}

func NewRequestLogger(logger *common.Logger) *RequestLogger {
	return &RequestLogger{logger: logger}
}

func (rl *RequestLogger) LogLocalRequest(method, url string, statusCode int) {
	rl.logger.Info("Local request processed",
		"method", method,
		"url", url,
		"status", statusCode,
	)
}

func (rl *RequestLogger) LogRequestError(operation string, err error) {
	rl.logger.Error("Request operation failed",
		"operation", operation,
		"error", err,
	)
}

func (rl *RequestLogger) LogConnectionError(err error) {
	rl.logger.Error("Connection error",
		"error", err,
	)
}
