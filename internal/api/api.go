// Package api exposes the service's own REST API used by ERP / POS systems
// to issue and query electronic fiscal documents.
package api

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/emision"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/seguimiento"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// API serves /api/v1.
type API struct {
	Emision *emision.Service
	DGII    *dgii.Client         // nil when no certificate is configured
	Tracker *seguimiento.Tracker // nil when submission is disabled
	Store   store.Store
	APIKey  string // when set, required in the X-API-Key header
	Logger  *slog.Logger
}

// Register mounts the routes on mux (lower-case paths).
func (a *API) Register(mux *http.ServeMux) {
	h := func(pattern string, f http.HandlerFunc) { mux.Handle(pattern, a.auth(f)) }

	// Issuance.
	h("POST /api/v1/ecf/validar", a.validar)
	h("POST /api/v1/ecf/xml", a.generarXML)
	h("POST /api/v1/ecf/emitir", a.emitir)
	h("GET /api/v1/ecf/{archivo}", a.obtener)
	h("GET /api/v1/ecf/{archivo}/pdf", a.pdfEmitido)
	h("GET /api/v1/ecf/{archivo}/estado", a.seguimiento)
	h("POST /api/v1/ecf/{archivo}/reenviar", a.reenviar)
	h("POST /api/v1/ecf/{archivo}/entregar", a.entregar)
	h("GET /api/v1/ecf/{archivo}/acuse", a.acuse)
	h("GET /api/v1/envios", a.envios)
	h("POST /api/v1/anulacion", a.anulacion)

	// Received documents.
	h("GET /api/v1/recibidos", a.recibidos)
	h("GET /api/v1/recibidos/{archivo}", a.recibido)
	h("GET /api/v1/recibidos/{archivo}/pdf", a.pdfRecibido)
	h("POST /api/v1/recibidos/{archivo}/aprobacion", a.aprobarRecibido)
	h("POST /api/v1/aprobacion-comercial", a.aprobacion)

	// DGII queries.
	h("GET /api/v1/dgii/resultado/{trackid}", a.resultado)
	h("GET /api/v1/dgii/estado", a.estado)
	h("GET /api/v1/dgii/trackids", a.trackIDs)
	h("GET /api/v1/dgii/rfce", a.consultaRFCE)
	h("GET /api/v1/dgii/directorio", a.directorio)

	// Utilities.
	h("POST /api/v1/xml/firmar", a.firmar)
	h("POST /api/v1/xml/verificar", a.verificar)
	h("POST /api/v1/xml/validar/{esquema}", a.validarXML)
	h("GET /api/v1/tablas/{tabla}", a.tablas)
}

func (a *API) auth(next http.Handler) http.Handler {
	if a.APIKey == "" {
		return next
	}
	key := []byte(a.APIKey)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-API-Key")
		if got == "" {
			if h := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(h), "bearer ") {
				got = h[7:]
			}
		}
		if subtle.ConstantTimeCompare([]byte(got), key) != 1 {
			httpx.Error(w, http.StatusUnauthorized, "API key inválida o ausente")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func boolParam(r *http.Request, name string, def bool) bool {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}
	return b
}

func (a *API) decodeECF(w http.ResponseWriter, r *http.Request) (*ecf.ECF, bool) {
	var e ecf.ECF
	if err := httpx.DecodeJSON(r, &e); err != nil {
		httpx.Error(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return nil, false
	}
	return &e, true
}

func intParam(r *http.Request, name string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(name)); err == nil {
		return v
	}
	return def
}

func options(r *http.Request) emision.Options {
	return emision.Options{
		Completar:       boolParam(r, "completar", true),
		Estricto:        boolParam(r, "estricto", true),
		Enviar:          boolParam(r, "enviar", false),
		Esperar:         time.Duration(intParam(r, "esperar", 0)) * time.Second,
		LineasPorPagina: intParam(r, "lineasporpagina", 0),
	}
}

func writeIssuanceError(w http.ResponseWriter, err error) {
	var ve *emision.ValidationError
	switch {
	case errors.As(err, &ve):
		httpx.ErrorDetail(w, http.StatusUnprocessableEntity, "el documento no es válido", ve.Issues)
	case errors.Is(err, emision.ErrDuplicado):
		httpx.Error(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		httpx.Error(w, http.StatusNotFound, "documento no encontrado")
	default:
		httpx.Error(w, http.StatusInternalServerError, err.Error())
	}
}

// validar completes (unless ?completar=false) and validates an e-CF.
func (a *API) validar(w http.ResponseWriter, r *http.Request) {
	e, ok := a.decodeECF(w, r)
	if !ok {
		return
	}
	issues, err := a.Emision.Preparar(r.Context(), e, options(r))
	var ve *emision.ValidationError
	if err != nil && !errors.As(err, &ve) {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	status := http.StatusOK
	if issues.HasErrors() {
		status = http.StatusUnprocessableEntity
	}
	if issues == nil {
		issues = ecf.Issues{}
	}
	httpx.JSON(w, status, map[string]any{
		"valido":    !issues.HasErrors(),
		"resumen":   emision.EsResumen(e),
		"hallazgos": issues,
		"ecf":       e,
	})
}

// generarXML returns the unsigned XML of an e-CF.
func (a *API) generarXML(w http.ResponseWriter, r *http.Request) {
	e, ok := a.decodeECF(w, r)
	if !ok {
		return
	}
	if issues, err := a.Emision.Preparar(r.Context(), e, options(r)); err != nil {
		httpx.ErrorDetail(w, http.StatusUnprocessableEntity, err.Error(), issues)
		return
	}
	b, err := ecf.Marshal(e)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	httpx.XML(w, http.StatusOK, b)
}

// emitir completes, validates, signs and (with ?enviar=true) submits.
func (a *API) emitir(w http.ResponseWriter, r *http.Request) {
	e, ok := a.decodeECF(w, r)
	if !ok {
		return
	}
	res, err := a.Emision.Emitir(r.Context(), e, options(r))
	if err != nil {
		writeIssuanceError(w, err)
		return
	}
	if httpx.WantsXML(r) {
		httpx.XML(w, http.StatusCreated, []byte(res.XML))
		return
	}
	httpx.JSON(w, http.StatusCreated, res)
}

// fileName normalizes RNC+eNCF[.xml].
func fileName(s string) string {
	s = strings.TrimSuffix(strings.TrimSuffix(s, ".pdf"), ".xml")
	return strings.ToUpper(s) + ".xml"
}

// obtener returns a stored issued e-CF by file name (RNC+eNCF.xml).
func (a *API) obtener(w http.ResponseWriter, r *http.Request) {
	a.serveStored(w, r, store.EmitidoECF)
}

func (a *API) serveStored(w http.ResponseWriter, r *http.Request, kind store.Kind) {
	b, err := a.Store.Get(r.Context(), kind, fileName(r.PathValue("archivo")))
	if err != nil {
		writeIssuanceError(w, err)
		return
	}
	httpx.XML(w, http.StatusOK, b)
}

func (a *API) needDGII(w http.ResponseWriter) bool {
	if a.DGII == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "la conexión con DGII no está configurada (falta certificado)")
		return false
	}
	return true
}

func writeDGII(w http.ResponseWriter, v any, err error) {
	var apiErr *dgii.APIError
	switch {
	case errors.As(err, &apiErr):
		httpx.ErrorDetail(w, http.StatusBadGateway, apiErr.Error(), v)
	case err != nil:
		httpx.Error(w, http.StatusBadGateway, err.Error())
	default:
		httpx.JSON(w, http.StatusOK, v)
	}
}

func (a *API) resultado(w http.ResponseWriter, r *http.Request) {
	if !a.needDGII(w) {
		return
	}
	res, err := a.DGII.ConsultarResultado(r.Context(), r.PathValue("trackid"))
	writeDGII(w, res, err)
}

func (a *API) estado(w http.ResponseWriter, r *http.Request) {
	if !a.needDGII(w) {
		return
	}
	q := r.URL.Query()
	if q.Get("rncemisor") == "" || q.Get("encf") == "" {
		httpx.Error(w, http.StatusBadRequest, "rncemisor y encf son obligatorios")
		return
	}
	res, err := a.DGII.ConsultarEstado(r.Context(), q.Get("rncemisor"), q.Get("encf"), q.Get("rnccomprador"), q.Get("codigoseguridad"))
	writeDGII(w, res, err)
}

func (a *API) trackIDs(w http.ResponseWriter, r *http.Request) {
	if !a.needDGII(w) {
		return
	}
	q := r.URL.Query()
	res, err := a.DGII.ConsultarTrackIDs(r.Context(), q.Get("rncemisor"), q.Get("encf"))
	writeDGII(w, res, err)
}

func (a *API) consultaRFCE(w http.ResponseWriter, r *http.Request) {
	if !a.needDGII(w) {
		return
	}
	q := r.URL.Query()
	res, err := a.DGII.ConsultarRFCE(r.Context(), q.Get("rncemisor"), q.Get("encf"), q.Get("codigoseguridad"))
	writeDGII(w, res, err)
}

func (a *API) directorio(w http.ResponseWriter, r *http.Request) {
	if !a.needDGII(w) {
		return
	}
	if rnc := r.URL.Query().Get("rnc"); rnc != "" {
		res, err := a.DGII.DirectorioPorRNC(r.Context(), rnc)
		writeDGII(w, res, err)
		return
	}
	res, err := a.DGII.Directorio(r.Context())
	writeDGII(w, res, err)
}

func (a *API) aprobacion(w http.ResponseWriter, r *http.Request) {
	var ac ecf.ACECF
	if err := httpx.DecodeJSON(r, &ac); err != nil {
		httpx.Error(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	signed, env, err := a.Emision.FirmarAprobacion(r.Context(), &ac, boolParam(r, "enviar", false))
	if err != nil {
		writeIssuanceError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"xml": string(signed), "envio": env})
}

func (a *API) anulacion(w http.ResponseWriter, r *http.Request) {
	var an ecf.ANECF
	if err := httpx.DecodeJSON(r, &an); err != nil {
		httpx.Error(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	signed, env, err := a.Emision.Anular(r.Context(), &an, boolParam(r, "enviar", false))
	if err != nil {
		var ve *emision.ValidationError
		if errors.As(err, &ve) {
			writeIssuanceError(w, err)
			return
		}
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"xml": string(signed), "envio": env})
}

// firmar signs an arbitrary DGII XML document (raw body or multipart "xml").
func (a *API) firmar(w http.ResponseWriter, r *http.Request) {
	if a.Emision.Signer == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "no hay certificado digital configurado")
		return
	}
	data, _, err := httpx.ReadXMLUpload(r)
	if err != nil || len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "XML requerido")
		return
	}
	signed, err := xmldsig.Sign(data, a.Emision.Signer)
	if err != nil {
		httpx.Error(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	httpx.XML(w, http.StatusOK, signed)
}

// verificar checks the signature of an XML document.
func (a *API) verificar(w http.ResponseWriter, r *http.Request) {
	data, _, err := httpx.ReadXMLUpload(r)
	if err != nil || len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "XML requerido")
		return
	}
	cert, err := xmldsig.Verify(data)
	if err != nil {
		httpx.JSON(w, http.StatusOK, map[string]any{"valido": false, "error": err.Error()})
		return
	}
	code, _ := xmldsig.SecurityCode(data)
	httpx.JSON(w, http.StatusOK, map[string]any{
		"valido":          true,
		"firmante":        cert.Subject.String(),
		"identificacion":  xmldsig.SubjectID(cert),
		"emisorCert":      cert.Issuer.String(),
		"validoDesde":     cert.NotBefore,
		"validoHasta":     cert.NotAfter,
		"codigoSeguridad": code,
	})
}
