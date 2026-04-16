package util

import (
	"io"
	"log/slog"
)

func CloseCleanly(closer io.Closer) {
	err := closer.Close()
	if err != nil {
		slog.Error("failed to close", "err", err)
	}
}
