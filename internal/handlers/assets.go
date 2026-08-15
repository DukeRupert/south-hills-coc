package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Static assets are fingerprinted: the URL a template emits carries a hash of
// the file's contents, so main.css becomes /static/css/main.7f3a91c4.css. A
// changed file gets a changed URL, which lets us mark every asset immutable
// and cache it for a year.
//
// This exists because the site sits behind Cloudflare. With a stable filename,
// an edge cache keeps serving the previous build for the length of its TTL,
// and a CSS fix is invisible to visitors for hours after a successful deploy.
// Fingerprinting makes a deploy self-invalidating: new bytes, new URL, no
// purge required.

const (
	// Hashed assets can never be stale — the hash is derived from the bytes —
	// so they are cached for a year and marked immutable so browsers do not
	// even revalidate.
	cacheControlImmutable = "public, max-age=31536000, immutable"

	// Anything requested by its plain name might change under that name, so it
	// gets a short TTL. Root-level files (favicon.ico, robots.txt) and any
	// reference not yet migrated to the asset helper land here.
	cacheControlShort = "public, max-age=300"
)

// assetIndex maps between logical asset paths ("css/main.css") and their
// fingerprinted equivalents ("css/main.7f3a91c4.css"). It is built once at
// startup and read-only thereafter.
type assetIndex struct {
	dir      string
	toHashed map[string]string
	toPlain  map[string]string
}

// newAssetIndex walks dir and fingerprints every file it finds. A file that
// cannot be read is skipped and logged rather than fatal: an unreadable image
// should not take the site down, and the helper falls back to the plain URL.
func newAssetIndex(dir string) *assetIndex {
	idx := &assetIndex{
		dir:      dir,
		toHashed: map[string]string{},
		toPlain:  map[string]string{},
	}

	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		sum, err := hashFile(p)
		if err != nil {
			log.Printf("assets: skipping %s: %v", p, err)
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return nil
		}
		logical := filepath.ToSlash(rel)
		hashed := insertHash(logical, sum)
		idx.toHashed[logical] = hashed
		idx.toPlain[hashed] = logical
		return nil
	})
	if err != nil {
		log.Printf("assets: failed to index %s: %v", dir, err)
	}
	return idx
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
	// 8 hex characters is 32 bits of hash. At a few dozen assets the odds of a
	// collision are far below the odds of the deploy failing outright.
	return hex.EncodeToString(h.Sum(nil))[:8], nil
}

// insertHash turns "css/main.css" into "css/main.7f3a91c4.css". An extensionless
// file gets the hash appended.
func insertHash(logical, sum string) string {
	ext := path.Ext(logical)
	return strings.TrimSuffix(logical, ext) + "." + sum + ext
}

// url returns the public URL for a logical asset path. Callers may pass the
// path with or without a leading slash and with or without a "static/" prefix,
// so template data that already stores "/images/foo.png" works unchanged.
//
// An unknown path returns the plain URL. A typo therefore shows up as a 404 on
// one asset rather than a failed render.
func (idx *assetIndex) url(logical string) string {
	logical = strings.TrimPrefix(logical, "/")
	logical = strings.TrimPrefix(logical, "static/")
	if hashed, ok := idx.toHashed[logical]; ok {
		return "/static/" + hashed
	}
	return "/static/" + logical
}

// resolve maps a requested path back to the file on disk, reporting whether the
// request used a fingerprinted name.
func (idx *assetIndex) resolve(requested string) (logical string, hashed bool) {
	requested = strings.TrimPrefix(requested, "/")
	if plain, ok := idx.toPlain[requested]; ok {
		return plain, true
	}
	return requested, false
}

// AssetURL returns the fingerprinted URL for a static asset. It is exposed to
// templates as the "asset" function.
//
// In development it returns the plain URL: there is no CDN in front of a local
// server, and the index is built at startup, so a hashed URL would go stale the
// moment `npm run css:dev` rewrote main.css.
func (h *Handler) AssetURL(logical string) string {
	if h.isDev || h.assets == nil {
		logical = strings.TrimPrefix(logical, "/")
		logical = strings.TrimPrefix(logical, "static/")
		return "/static/" + logical
	}
	return h.assets.url(logical)
}

// StaticHandler serves /static/ from disk, resolving fingerprinted names back
// to the underlying file and setting Cache-Control accordingly. Both the hashed
// and the plain name for a file are served, so a stale HTML page holding an old
// hashed URL keeps working until the next build changes it.
func (h *Handler) StaticHandler() http.Handler {
	files := http.FileServer(http.Dir(h.staticDir))

	return http.StripPrefix("/static", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// http.FileServer renders a browsable index for a directory path. That
		// published the whole asset inventory, including the uncompiled
		// input.css sitting next to the built stylesheet.
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}

		if h.isDev || h.assets == nil {
			w.Header().Set("Cache-Control", "no-store")
			files.ServeHTTP(w, r)
			return
		}

		logical, hashed := h.assets.resolve(r.URL.Path)
		if !hashed {
			w.Header().Set("Cache-Control", cacheControlShort)
			files.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", cacheControlImmutable)

		// Rewrite to the real filename. Copy the request rather than mutating
		// the caller's — the handler chain above us may still read it.
		rewritten := *r
		u := *r.URL
		u.Path = "/" + logical
		rewritten.URL = &u
		files.ServeHTTP(w, &rewritten)
	}))
}
