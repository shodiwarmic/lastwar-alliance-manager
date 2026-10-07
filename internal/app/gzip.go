package app

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/klauspost/compress/gzhttp"
)

// compressibleTypes is an ALLOWLIST, not gzhttp's default deny-list: choosing by content
// type must never reach a ZIP-based Office download (application/zip from the files and
// WOPI GetFile handlers), an image or a PDF, all of which are already compressed. A type
// without parameters matches it with any charset.
var compressibleTypes = []string{
	"text/html",
	"text/css",
	"application/javascript",
	"text/javascript",
	"application/json",
	"image/svg+xml",
	"text/plain",
	"text/csv",
}

// gzipMinSize is the floor below which a response goes out as-is: under ~1 KiB the gzip
// framing and the CPU cost outweigh the bytes saved.
const gzipMinSize = 1024

// gzipMiddleware compresses responses the client accepts gzip for. It wraps the whole
// server handler once, so pages, static files, API JSON, the setup page and error bodies
// are all covered whether or not a reverse proxy sits in front. gzhttp sets
// Vary: Accept-Encoding, passes through a response that already has a Content-Encoding or
// a Content-Range, and keeps the ETag unchanged — the static handler's If-None-Match
// round trip still yields 304 (the body is never re-sent, so the representation question
// a suffixed ETag answers does not arise, and Vary keeps shared caches apart).
//
// zstd is off: one coding keeps the behaviour behind Caddy (which passes an encoded
// response through) and the tests to one shape. Compressed request bodies stay refused.
func gzipMiddleware(next http.Handler) http.Handler {
	wrap, err := gzhttp.NewWrapper(
		gzhttp.ContentTypes(compressibleTypes),
		gzhttp.MinSize(gzipMinSize),
		gzhttp.EnableZstd(false),
	)
	if err != nil {
		// Only reachable through an invalid option above — a programming error.
		slog.Error("Failed to build the compression middleware", "error", err)
		os.Exit(1)
	}
	return wrap(next)
}
