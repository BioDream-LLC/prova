// Package web serves the built front end from inside the binary.
//
// The assets are embedded rather than shipped alongside, so deployment stays one file: copy the
// binary, run it, and the interface is there. The built output (dist/) is committed so that
// `go build` works on a checkout without Node installed. Rebuild it with `make web`.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// A pattern that matches nothing is a compile error, which is the behaviour worth having: if the
// front end has not been built, the build stops rather than producing a server that 404s its own
// interface. A committed placeholder in dist/assets keeps this compiling before the first web build.
//
//go:embed dist/index.html dist/favicon.svg dist/assets
var embedded embed.FS

// Assets returns the built front end as a filesystem rooted at dist/.
func Assets() (fs.FS, error) {
	return fs.Sub(embedded, "dist")
}

// Available reports whether a real front end was built into this binary, as opposed to the
// placeholder that keeps the package compiling.
func Available() bool {
	assets, err := Assets()
	if err != nil {
		return false
	}
	entries, err := fs.ReadDir(assets, "assets")
	if err != nil || len(entries) == 0 {
		return false
	}
	// A lone placeholder means the real interface has not been built yet.
	if len(entries) == 1 && strings.HasPrefix(entries[0].Name(), "placeholder") {
		return false
	}
	return true
}

// Handler serves the front end.
//
// Anything that is not a real file falls back to index.html, because the interface is a single page
// application: a browser reloaded on a deep link must get the app rather than a 404.
func Handler() (http.Handler, error) {
	assets, err := Assets()
	if err != nil {
		return nil, err
	}

	files := http.FileServer(http.FS(assets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		// Browsers ask for /favicon.ico on pages that name no icon, such as the manual; answer with the SVG.
		if clean == "favicon.ico" {
			clean = "favicon.svg"
			r = r.Clone(r.Context())
			r.URL.Path = "/favicon.svg"
		}
		if clean == "" || clean == "." {
			serveIndex(w, r, assets)
			return
		}

		f, err := assets.Open(clean)
		if err != nil {
			// A missing subresource is a 404, not the app: serving index.html for a script makes the
			// browser report a confusing MIME type error instead of the real problem.
			if isAssetPath(clean) {
				http.NotFound(w, r)
				return
			}
			serveIndex(w, r, assets)
			return
		}
		defer f.Close()

		if info, err := f.Stat(); err == nil && info.IsDir() {
			serveIndex(w, r, assets)
			return
		}

		// Hashed asset filenames change whenever their contents change, so they can be cached hard.
		// index.html must not be, or a browser keeps loading an old app against a new API.
		if strings.HasPrefix(clean, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	}), nil
}

func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "the web interface was not built into this binary", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// assetExtensions are the file types a browser fetches as a subresource rather than as a page.
var assetExtensions = map[string]bool{
	".js": true, ".mjs": true, ".css": true, ".wasm": true, ".map": true,
	".json": true, ".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".ico": true, ".webp": true, ".woff": true, ".woff2": true,
	".ttf": true, ".otf": true, ".eot": true, ".webmanifest": true,
}

// isAssetPath reports whether a path names a subresource rather than an application route.
func isAssetPath(clean string) bool {
	return assetExtensions[strings.ToLower(path.Ext(clean))]
}
