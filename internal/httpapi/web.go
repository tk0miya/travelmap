package httpapi

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// pageTemplates holds every page, parsed once here rather than per request:
// a template that does not compile stops the server at startup instead of
// turning one route into a 500 nobody visits until later.
var pageTemplates = template.Must(template.ParseFS(templatesFS, "templates/*.html"))

// staticFiles is staticFS with its own "static" directory peeled off, so a
// request for "/static/style.css" serves the file embedded at "static/style.css"
// rather than needing that prefix repeated in every URL.
var staticFiles = func() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		// static is embedded above; a missing subdirectory would be a build-time
		// mistake, not something a request could ever trigger.
		panic(err)
	}

	return sub
}()

// index answers GET /, travelmap's own browser entry point. It needs no
// credential yet — a session is what a later step has it name the account
// it belongs to.
func (a *api) index(w http.ResponseWriter, r *http.Request) {
	var buf bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&buf, "base", nil); err != nil {
		a.logger.Error("rendering the index page failed", "error", err)
		a.writeError(w, r, http.StatusInternalServerError, "internal server error")

		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	if _, err := buf.WriteTo(w); err != nil {
		a.logger.Error("writing the response body failed",
			"method", r.Method,
			"path", r.URL.Path,
			"error", err,
		)
	}
}
