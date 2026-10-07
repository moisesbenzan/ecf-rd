// Package httpx contains small HTTP helpers shared by the handlers.
package httpx

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"
)

// MaxUpload bounds request bodies (an e-CF with 1000 lines is well below).
const MaxUpload = 10 << 20

// JSON writes v as JSON with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// XML writes raw XML bytes with the given status.
func XML(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}

// Negotiate writes v as XML when the client prefers XML, JSON otherwise.
func Negotiate(w http.ResponseWriter, r *http.Request, status int, v any) {
	if WantsXML(r) {
		b, err := xml.Marshal(v)
		if err != nil {
			Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		XML(w, status, append([]byte(xml.Header), b...))
		return
	}
	JSON(w, status, v)
}

// WantsXML reports whether the Accept header prefers XML over JSON.
func WantsXML(r *http.Request) bool {
	a := strings.ToLower(r.Header.Get("Accept"))
	return strings.Contains(a, "xml") && !strings.Contains(a, "json")
}

// ErrorBody is the JSON error envelope.
type ErrorBody struct {
	Error   string `json:"error"`
	Detalle any    `json:"detalle,omitempty"`
}

// Error writes a JSON error.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, ErrorBody{Error: msg})
}

// ErrorDetail writes a JSON error with details.
func ErrorDetail(w http.ResponseWriter, status int, msg string, detail any) {
	JSON(w, status, ErrorBody{Error: msg, Detalle: detail})
}

// ReadXMLUpload returns the XML document from a request: either the "xml"
// field of a multipart form (the DGII convention) or the raw body.
func ReadXMLUpload(r *http.Request) ([]byte, string, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, MaxUpload)
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct == "multipart/form-data" {
		if err := r.ParseMultipartForm(MaxUpload); err != nil {
			return nil, "", err
		}
		f, hdr, err := r.FormFile("xml")
		if err != nil {
			// Accept any single file field for robustness.
			if r.MultipartForm != nil {
				for _, files := range r.MultipartForm.File {
					if len(files) > 0 {
						fh := files[0]
						ff, err := fh.Open()
						if err != nil {
							return nil, "", err
						}
						defer ff.Close()
						b, err := io.ReadAll(ff)
						return b, fh.Filename, err
					}
				}
			}
			return nil, "", errors.New("falta el archivo 'xml' en el formulario")
		}
		defer f.Close()
		b, err := io.ReadAll(f)
		return b, hdr.Filename, err
	}
	b, err := io.ReadAll(r.Body)
	return b, "", err
}

// DecodeJSON decodes a JSON request body into v, rejecting unknown fields so
// that misspelled DGII tag names are reported instead of silently dropped.
func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, MaxUpload))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// CaseInsensitive lower-cases request paths before routing. DGII requires
// receiver services to be case-insensitive.
func CaseInsensitive(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.URL.Path = strings.ToLower(r.URL.Path)
		r.URL.RawPath = ""
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Logging logs one line per request.
func Logging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", rec.status,
			"duration_ms", float64(time.Since(start).Microseconds())/1000, "remote", r.RemoteAddr)
	})
}

// Recover converts panics into 500 responses.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Error("panic", "error", v, "path", r.URL.Path)
				Error(w, http.StatusInternalServerError, "error interno")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
