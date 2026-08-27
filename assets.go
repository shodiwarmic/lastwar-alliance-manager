// assets.go - Content-addressed URLs for the files under static/.
//
// Every same-origin .js/.css reference in a template goes through
// {{asset "/foo.js"}}, which appends ?v=<first 8 hex of the file's SHA-256>.
// The URL therefore changes if and only if the BYTES change: a deploy that
// touches one file re-downloads one file, and a restart that changes nothing
// re-downloads nothing.
//
// That "iff" is the whole point, and it is why this is not a single global
// token stamped at boot. A boot timestamp would bust all 70 files on every
// crash-loop, every .env edit and every `docker compose restart` — a restart is
// not a new file version. A git SHA would at least survive restarts, but
// .dockerignore excludes .git, so it would have to be threaded through the
// Dockerfile, docker-publish.yml and update.sh as a build-arg plus an -ldflags
// -X variable, and it would still bust all 70 files for a docs-only commit.
// Hashing the bytes at runtime needs none of that plumbing.

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// assetHashes maps a URL path ("/styles.css") to its content hash.
//
// CONCURRENCY: written exactly once, by buildAssetHashes() from main(), before
// any request-serving goroutine exists; read-only from then on. That ordering
// is the entire synchronisation story — there is no lock, and none is needed,
// because goroutine creation supplies the happens-before edge.
//
// If you ever add a runtime rebuild (a SIGHUP handler, a file watcher), this
// MUST become an atomic.Pointer first. Mutating it in place while handlers read
// it is a data race.
var assetHashes map[string]string

// buildAssetHashes walks static/ once at startup. ~70 files / 1.6 MB, so this
// is a few milliseconds on boot.
//
// It never fails fatally. A per-file read error is logged and skipped, and a
// failed walk leaves a partial or empty map — in which case assetPath emits
// bare, unversioned URLs, which the static handler still serves correctly (with
// no-cache). Degraded, never broken.
func buildAssetHashes() {
	m := make(map[string]string, 96)

	err := filepath.WalkDir(staticDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		h, herr := hashFile(p)
		if herr != nil {
			// One unreadable file must not blank the whole manifest.
			slog.Warn("Asset hash failed; file will be served unversioned", "path", p, "error", herr)
			return nil
		}
		rel, rerr := filepath.Rel(staticDir, p)
		if rerr != nil {
			return nil
		}
		m["/"+filepath.ToSlash(rel)] = h
		return nil
	})
	if err != nil {
		slog.Error("Static asset walk failed; assets fall back to no-cache", "error", err)
	}

	assetHashes = m
	slog.Info("Static asset manifest built", "files", len(m))
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	// 8 hex chars = 32 bits. This is not a birthday problem: a token is only
	// ever compared against the previous content of the SAME URL, so a missed
	// bust needs a 1-in-4-billion collision on one file's specific edit.
	return hex.EncodeToString(h.Sum(nil))[:8], nil
}

// assetPath is the {{asset "..."}} template function.
//
// An unknown file (a typo, a file added after boot, a failed walk) yields the
// bare path with no token. That is deliberate and safe: the static handler
// answers an unversioned request with Cache-Control: no-cache, so the worst
// case is one revalidation round-trip — never a stale file.
func assetPath(urlPath string) string {
	if !strings.HasPrefix(urlPath, "/") {
		urlPath = "/" + urlPath
	}
	if tok := assetToken(urlPath); tok != "" {
		return urlPath + "?v=" + tok
	}
	return urlPath
}

func assetToken(urlPath string) string {
	if isProduction() {
		// Reading a nil map is legal and yields "".
		return assetHashes[urlPath]
	}

	// Dev bind-mounts ./static read-only into the container, so files change
	// after boot and a boot-time manifest would go stale while you are staring
	// at it mid-debug. One os.Stat per reference (a handful per page), against
	// a handler that already runs several SQL queries and re-parses two HTML
	// files from disk. Re-hashing instead would read 127 KB of styles.css on
	// every render.
	//
	// Dev also sends no-store, so the token is not load-bearing there — it just
	// keeps dev honest and on the same code path as production.
	st, err := os.Stat(filepath.Join(staticDir, filepath.Clean(urlPath)))
	if err != nil || st.IsDir() {
		return ""
	}
	return strconv.FormatInt(st.ModTime().UnixNano(), 36)
}

// assetHashFor looks up a hash by an already-cleaned URL path, for the static
// file handler's cache-header decision.
func assetHashFor(cleanURLPath string) (string, bool) {
	h, ok := assetHashes[filepath.ToSlash(cleanURLPath)]
	return h, ok
}

// templateFuncs is the FuncMap that EVERY template parse must carry.
//
// html/template rejects an unknown function when it PARSES, not when it
// executes, so a parse site that forgets this fails loudly on its first request
// rather than silently rendering a broken page. Use parseTemplates() and it is
// handled for you.
func templateFuncs() template.FuncMap {
	return template.FuncMap{"asset": assetPath}
}

// parseTemplates parses templates with the shared FuncMap. Use this instead of
// template.ParseFiles anywhere in this codebase.
//
// The returned template is NAMED after filepath.Base(files[0]), which is not
// cosmetic. text/template's ParseFiles only makes t itself the parsed content
// when t's name already equals that file's base name; otherwise t stays empty
// and t.Execute fails with "incomplete or empty template". Naming it this way
// keeps both existing call styles working unchanged:
//
//	parseTemplates("templates/layout.html", "templates/vs.html")
//	  -> t.ExecuteTemplate(w, "layout.html", data)
//	parseTemplates("templates/login.html")
//	  -> t.Execute(w, data)
func parseTemplates(files ...string) (*template.Template, error) {
	if len(files) == 0 {
		return nil, errors.New("parseTemplates: no files given")
	}
	return template.New(filepath.Base(files[0])).Funcs(templateFuncs()).ParseFiles(files...)
}
