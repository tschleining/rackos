package webconsole

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const contentSecurityPolicy = "" +
	"default-src 'none'; " +
	"script-src 'self'; " +
	"style-src 'self'; " +
	"style-src-attr 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'none'"

var hashedAssetName = regexp.MustCompile(
	`^[A-Za-z0-9_.-]+-[A-Za-z0-9_-]{8,}\.(js|css|woff2|svg)$`,
)

type Handler struct {
	assets fs.FS
	index  []byte
}

func New(assets fs.FS) (*Handler, error) {
	if assets == nil {
		return nil, errors.New(
			"web console filesystem is required",
		)
	}

	index, err := fs.ReadFile(
		assets,
		"index.html",
	)
	if err != nil {
		return nil, fmt.Errorf(
			"read web console index: %w",
			err,
		)
	}

	if len(index) == 0 {
		return nil, errors.New(
			"web console index is empty",
		)
	}

	info, err := fs.Stat(assets, "assets")
	if err != nil {
		return nil, fmt.Errorf(
			"inspect web console assets: %w",
			err,
		)
	}

	if !info.IsDir() {
		return nil, errors.New(
			"web console assets must be a directory",
		)
	}

	return &Handler{
		assets: assets,
		index:  index,
	}, nil
}

func (h *Handler) ServeHTTP(
	w http.ResponseWriter,
	r *http.Request,
) {
	setSecurityHeaders(w)

	if r.Method != http.MethodGet &&
		r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")

		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	requestPath := r.URL.Path

	if !validRequestPath(requestPath) {
		http.NotFound(w, r)
		return
	}

	switch {
	case requestPath == "/",
		requestPath == "/index.html":

		h.serveIndex(w, r)

	case strings.HasPrefix(
		requestPath,
		"/assets/",
	):
		h.serveAsset(w, r)

	case requestPath == "/assets":

		http.NotFound(w, r)

	case path.Ext(requestPath) != "":

		// Missing files must not receive the SPA
		// index as an HTTP 200 response.
		http.NotFound(w, r)

	case acceptsHTML(r):

		// React Router handles application routes,
		// including its own not-found page.
		h.serveIndex(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) serveIndex(
	w http.ResponseWriter,
	r *http.Request,
) {
	w.Header().Set(
		"Content-Type",
		"text/html; charset=utf-8",
	)

	w.Header().Set(
		"Cache-Control",
		"no-store",
	)

	http.ServeContent(
		w,
		r,
		"index.html",
		time.Time{},
		bytes.NewReader(h.index),
	)
}

func (h *Handler) serveAsset(
	w http.ResponseWriter,
	r *http.Request,
) {
	name := strings.TrimPrefix(
		r.URL.Path,
		"/",
	)

	if !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}

	contentType := assetContentType(name)
	if contentType == "" {
		http.NotFound(w, r)
		return
	}

	info, err := fs.Stat(h.assets, name)

	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}

	if err != nil {
		http.Error(
			w,
			"web console asset unavailable",
			http.StatusInternalServerError,
		)
		return
	}

	if !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}

	content, err := fs.ReadFile(
		h.assets,
		name,
	)
	if err != nil {
		http.Error(
			w,
			"web console asset unavailable",
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		contentType,
	)

	if hashedAssetName.MatchString(
		path.Base(name),
	) {
		w.Header().Set(
			"Cache-Control",
			"private, max-age=31536000, immutable",
		)
	}

	http.ServeContent(
		w,
		r,
		path.Base(name),
		time.Time{},
		bytes.NewReader(content),
	)
}

func assetContentType(name string) string {
	switch path.Ext(name) {
	case ".js":
		return "text/javascript; charset=utf-8"

	case ".css":
		return "text/css; charset=utf-8"

	case ".woff2":
		return "font/woff2"

	case ".svg":
		return "image/svg+xml"

	default:
		return ""
	}
}

func validRequestPath(value string) bool {
	if !strings.HasPrefix(value, "/") ||
		strings.Contains(value, "//") ||
		strings.Contains(value, `\`) ||
		strings.ContainsRune(value, '\x00') {
		return false
	}

	clean := path.Clean(value)

	return value == clean ||
		value == clean+"/"
}

func acceptsHTML(r *http.Request) bool {
	for _, header := range r.Header.Values("Accept") {
		for _, entry := range strings.Split(header, ",") {
			mediaType, parameters, err :=
				mime.ParseMediaType(
					strings.TrimSpace(entry),
				)

			if err != nil ||
				mediaType != "text/html" {
				continue
			}

			quality, exists := parameters["q"]
			if !exists {
				return true
			}

			q, err := strconv.ParseFloat(
				quality,
				64,
			)

			if err == nil && q > 0 && q <= 1 {
				return true
			}
		}
	}

	return false
}

func setSecurityHeaders(w http.ResponseWriter) {
	headers := w.Header()

	headers.Set(
		"Content-Security-Policy",
		contentSecurityPolicy,
	)

	headers.Set(
		"X-Content-Type-Options",
		"nosniff",
	)

	headers.Set(
		"X-Frame-Options",
		"DENY",
	)

	headers.Set(
		"Referrer-Policy",
		"no-referrer",
	)

	headers.Set(
		"Cross-Origin-Resource-Policy",
		"same-origin",
	)

	headers.Set(
		"Permissions-Policy",
		"camera=(), microphone=(), geolocation=()",
	)

	headers.Set(
		"Cache-Control",
		"no-store",
	)
}
