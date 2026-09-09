package httpapi

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// frontendDistFS is frontend/dist, Vite's build output (see vite.config.ts's
// build.outDir) — go:embed cannot reach outside its own package directory,
// which is why the frontend builds directly into a subdirectory here rather
// than its own default frontend/dist. It holds the built index.html and JS
// bundle, plus whatever frontend/public copied over verbatim, style.css
// among them.
//
//go:embed frontend/dist
var frontendDistFS embed.FS

// frontendFiles is frontendDistFS with its own "frontend/dist" prefix peeled
// off, so a request path maps directly onto it.
var frontendFiles = func() fs.FS {
	sub, err := fs.Sub(frontendDistFS, "frontend/dist")
	if err != nil {
		// frontend/dist is embedded above; a missing subdirectory would be a
		// build-time mistake, not something a request could ever trigger.
		panic(err)
	}

	return sub
}()

// frontendFileServer serves frontendFiles as-is, for the paths serveFrontend
// confirms exist there.
var frontendFileServer = http.FileServerFS(frontendFiles)

// serveFrontend answers a request out of the frontend build: a path that
// exists there — the JS bundle, style.css, a future vendored map library —
// is served as itself, and anything else falls back to index.html, so
// client-side routing owns every such path. [api.browserNotFound] is what
// calls this only for a path no other route claims.
func serveFrontend(w http.ResponseWriter, r *http.Request) {
	if name := strings.TrimPrefix(path.Clean(r.URL.Path), "/"); name != "" {
		if f, err := frontendFiles.Open(name); err == nil {
			_ = f.Close()
		} else {
			// "/", not "/index.html" directly: http.FileServer serves a
			// directory's index.html on its own, and redirects a path
			// literally ending in "/index.html" to "./" instead of serving
			// it, to keep that URL out of a browser's address bar.
			shell := r.Clone(r.Context())
			shell.URL.Path = "/"
			r = shell
		}
	}

	frontendFileServer.ServeHTTP(w, r)
}

// browserNotFound is the router's own [chi.Mux.NotFound] handler, reached
// only once nothing else has matched the request at all (a wrong method on
// a known path is a 405, not this) — see where it is wired in for why that
// distinction matters here. A GET or HEAD, the shapes a browser navigation
// takes, gets the frontend build's shell, since a route this server has
// never heard of may still be one the client-side router owns; anything
// else gets the same plain JSON 404 every other unmatched request gets.
func (a *api) browserNotFound(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet || r.Method == http.MethodHead {
		serveFrontend(w, r)

		return
	}

	a.notFound(w, r)
}
