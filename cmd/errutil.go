package cmd

import "log/slog"

// warnIfErr logs a non-fatal error to the structured logger.
//
// Use this for audit / gazette / hansard writes in CLI handlers where the
// user-facing operation has already succeeded and we do not want to fail the
// command, but the failure must not disappear silently.
//
// op identifies the call site (e.g. "create gazette"); extra attrs are passed
// through to slog and followed by the error itself under the "err" key.
func warnIfErr(op string, err error, attrs ...any) {
	if err == nil {
		return
	}
	slog.Warn(op, append(attrs, "err", err)...)
}
