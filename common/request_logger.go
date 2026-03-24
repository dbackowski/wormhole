package common

type RequestLogger struct {
	logger *Logger
}

func NewRequestLogger(logger *Logger) *RequestLogger {
	return &RequestLogger{logger: logger}
}

func (rl *RequestLogger) logWithDebugDetail(infoMsg string, infoArgs []any, debugMsg string, debugArgs []any) {
	rl.logger.Info(infoMsg, infoArgs...)
	rl.logger.Debug(debugMsg, debugArgs...)
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

