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

// pageTemplate parses base.html plus one page, as its own *template.Template
// rather than one set holding every page: each page defines a "content"
// block of its own, and parsing them all into one set would collide on that
// name. Parsed once here rather than per request, so a template that does
// not compile stops the server at startup instead of turning one route into
// a 500 nobody visits until later.
func pageTemplate(name string) *template.Template {
	return template.Must(template.ParseFS(templatesFS, "templates/base.html", "templates/"+name))
}

var (
	indexTemplate = pageTemplate("index.html")
	loginTemplate = pageTemplate("login.html")
)

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

// renderPage renders tmpl's "base" template with data as a 200 response.
func (a *api) renderPage(w http.ResponseWriter, r *http.Request, tmpl *template.Template, data any) {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", data); err != nil {
		a.logger.Error("rendering the page failed", "path", r.URL.Path, "error", err)
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

// indexData is what the index page's template renders.
type indexData struct {
	// Email is the signed-in account's address, or "" when no session names
	// one.
	Email string
}

// index answers GET /, travelmap's own browser entry point. It takes no
// credential of its own; loadSessionUser is what puts a user on the request
// context for it to name.
func (a *api) index(w http.ResponseWriter, r *http.Request) {
	var data indexData
	if user, ok := userFrom(r.Context()); ok {
		data.Email = user.Email
	}

	a.renderPage(w, r, indexTemplate, data)
}
