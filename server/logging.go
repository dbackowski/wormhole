package server

import "github.com/dbackowski/wormhole/common"

type RequestLogger struct {
	logger *common.Logger
}

func NewRequestLogger(logger *common.Logger) *RequestLogger {
	return &RequestLogger{logger: logger}
}

func (rl *RequestLogger) LogHTTPRequest(domain, uuid, method, url string, headers map[string][]string, body []byte) {
	rl.logger.Info("HTTP request forwarded",
		"domain", domain,
		"uuid", uuid,
		"method", method,
		"url", url,
	)

	if rl.logger.IsDebug() {
		rl.logger.Debug("HTTP request details",
			"uuid", uuid,
			"headers", headers,
			"body", body,
		)
	}
}

func (rl *RequestLogger) LogHTTPResponse(domain, uuid string, status int, body []byte) {
	rl.logger.Info("HTTP response received",
		"domain", domain,
		"uuid", uuid,
		"status", status,
	)

	if rl.logger.IsDebug() {
		rl.logger.Debug("HTTP response details",
			"uuid", uuid,
			"body", body,
		)
	}
}

func (rl *RequestLogger) LogClientRegistered(domain, remoteAddr string) {
	rl.logger.Info("Client connected",
		"domain", domain,
		"remote_addr", remoteAddr,
	)
}

func (rl *RequestLogger) LogClientDisconnected(domain, remoteAddr string, reason string) {
	rl.logger.Info("Client disconnected",
		"domain", domain,
		"remote_addr", remoteAddr,
		"reason", reason,
	)
}

func (rl *RequestLogger) LogConnectionError(msg string, err error, context map[string]interface{}) {
	args := []interface{}{"error", err}
	for k, v := range context {
		args = append(args, k, v)
	}
	rl.logger.Error(msg, args...)
}

func (rl *RequestLogger) LogDispatchError(uuid string, err error) {
	rl.logger.Error("Message dispatch failed",
		"uuid", uuid,
		"error", err,
	)
}
