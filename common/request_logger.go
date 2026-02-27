package common

type RequestLogger struct {
	logger *Logger
}

func NewRequestLogger(logger *Logger) *RequestLogger {
	return &RequestLogger{logger: logger}
}

func (rl *RequestLogger) logWithDebugDetail(infoMsg string, infoArgs []any, debugMsg string, debugArgs []any) {
	rl.logger.Info(infoMsg, infoArgs...)
	if rl.logger.IsDebug() {
		rl.logger.Debug(debugMsg, debugArgs...)
	}
}

func (rl *RequestLogger) LogHTTPRequest(domain, uuid, method, url string, headers map[string][]string, body []byte) {
	rl.logWithDebugDetail(
		"HTTP request forwarded", []any{"domain", domain, "uuid", uuid, "method", method, "url", url},
		"HTTP request details", []any{"uuid", uuid, "headers", headers, "body", body},
	)
}

func (rl *RequestLogger) LogHTTPResponse(domain, uuid string, status int, body []byte) {
	rl.logWithDebugDetail(
		"HTTP response received", []any{"domain", domain, "uuid", uuid, "status", status},
		"HTTP response details", []any{"uuid", uuid, "body", body},
	)
}

func (rl *RequestLogger) LogRequestError(operation string, err error) {
	rl.logger.Error("Request operation failed",
		"operation", operation,
		"error", err,
	)
}

func (rl *RequestLogger) LogConnectionError(msg string, err error, fields ...any) {
	args := []any{"error", err}
	args = append(args, fields...)
	rl.logger.Error(msg, args...)
}

func (rl *RequestLogger) LogLocalRequest(method, url string, statusCode int) {
	rl.logger.Info("Local request processed",
		"method", method,
		"url", url,
		"status", statusCode,
	)
}

func (rl *RequestLogger) LogClientConnected(domain, remoteAddr string) {
	rl.logger.Info("Client connected",
		"domain", domain,
		"remote_addr", remoteAddr,
	)
}

func (rl *RequestLogger) LogClientDisconnected(domain, remoteAddr, reason string) {
	rl.logger.Info("Client disconnected",
		"domain", domain,
		"remote_addr", remoteAddr,
		"reason", reason,
	)
}

func (rl *RequestLogger) LogDispatchError(uuid string, err error) {
	rl.logger.Error("Message dispatch failed",
		"uuid", uuid,
		"error", err,
	)
}
