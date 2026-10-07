// Package receptor implements the web services every electronic issuer must
// expose to receive documents from other taxpayers, as standardized in
// "Descripción Técnica Servicios Emisores Electrónicos":
//
//	GET  /fe/autenticacion/api/semilla               (optional authentication)
//	POST /fe/autenticacion/api/validacioncertificado (optional authentication)
//	POST /fe/recepcion/api/ecf                       (mandatory, returns signed ARECF)
//	POST /fe/aprobacioncomercial/api/ecf             (mandatory, HTTP 200/400)
package receptor

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
	"github.com/moisesbenzan/ecf-rd/internal/xsd"
)

const (
	seedTTL  = 5 * time.Minute
	tokenTTL = time.Hour
)

// Notifier is called after a document has been received and stored, e.g. to
// enqueue it for the accounting system. It must not block for long.
type Notifier func(ctx context.Context, kind store.Kind, name string, data []byte)

// Config configures the receiver.
type Config struct {
	// RNC of the taxpayer operating this service. Documents addressed to a
	// different RNCComprador are acknowledged as "no recibido" (motivo 4).
	RNC string
	// Signer signs the ARECF acknowledgements.
	Signer *xmldsig.Signer
	// Store persists received documents and the acknowledgements sent.
	Store store.Store
	// RequireAuth enforces the optional bearer-token authentication on the
	// reception endpoints. Enable it only if the authentication URL is
	// registered with DGII; otherwise issuers will not authenticate.
	RequireAuth bool
	// Notify is invoked for every stored document (optional).
	Notify Notifier
	// OnAprobacion is invoked for every valid commercial approval received
	// for an e-CF issued by this taxpayer (optional).
	OnAprobacion func(ctx context.Context, a *ecf.ACECF)
	Logger       *slog.Logger
}

// Receptor serves the inbound endpoints.
type Receptor struct {
	cfg Config
	log *slog.Logger

	mu     sync.Mutex
	seeds  map[string]time.Time
	tokens map[string]session
}

type session struct {
	subject string
	expires time.Time
}

// New creates a Receptor.
func New(cfg Config) *Receptor {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Receptor{cfg: cfg, log: log, seeds: map[string]time.Time{}, tokens: map[string]session{}}
}

// Register mounts the endpoints on mux. Paths are lower-case; wrap the
// final handler with httpx.CaseInsensitive.
func (rc *Receptor) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET "+dgii.PathSemilla, rc.semilla)
	mux.HandleFunc("POST "+dgii.PathValidacionCertificado, rc.validacionCertificado)
	mux.HandleFunc("POST "+dgii.PathRecepcion, rc.recepcion)
	mux.HandleFunc("POST "+dgii.PathAprobacionComercial, rc.aprobacionComercial)
}

type semillaModel struct {
	XMLName xml.Name `xml:"SemillaModel"`
	XSI     string   `xml:"xmlns:xsi,attr"`
	XSD     string   `xml:"xmlns:xsd,attr"`
	Valor   string   `xml:"valor"`
	Fecha   string   `xml:"fecha"`
}

func (rc *Receptor) semilla(w http.ResponseWriter, r *http.Request) {
	buf := make([]byte, 64)
	if _, err := rand.Read(buf); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no fue posible generar la semilla")
		return
	}
	valor := base64.StdEncoding.EncodeToString(buf)
	now := time.Now()
	rc.mu.Lock()
	rc.gc(now)
	rc.seeds[valor] = now.Add(seedTTL)
	rc.mu.Unlock()

	out, _ := xml.Marshal(semillaModel{
		XSI:   "http://www.w3.org/2001/XMLSchema-instance",
		XSD:   "http://www.w3.org/2001/XMLSchema",
		Valor: valor,
		Fecha: now.In(ecf.Location).Format("2006-01-02T15:04:05.0000000-07:00"),
	})
	httpx.XML(w, http.StatusOK, append([]byte(`<?xml version="1.0" encoding="utf-8"?>`), out...))
}

type respuestaAutenticacion struct {
	XMLName  xml.Name `xml:"RespuestaAutenticacion" json:"-"`
	Token    string   `xml:"token" json:"token"`
	Expira   string   `xml:"expira" json:"expira"`
	Expedido string   `xml:"expedido" json:"expedido"`
}

func (rc *Receptor) validacionCertificado(w http.ResponseWriter, r *http.Request) {
	data, _, err := httpx.ReadXMLUpload(r)
	if err != nil || len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "archivo no válido")
		return
	}
	cert, err := xmldsig.Verify(data)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "la firma del XML no es válida")
		return
	}
	if err := xmldsig.CheckValidity(cert, time.Now()); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	var seed semillaModel
	if is := ecf.ValidateXML(xsd.Semilla, data); is.HasErrors() {
		httpx.ErrorDetail(w, http.StatusBadRequest, "la estructura del archivo XML no es válida", is)
		return
	}
	if err := xml.Unmarshal(data, &seed); err != nil || seed.Valor == "" {
		httpx.Error(w, http.StatusBadRequest, "la estructura del archivo XML no es válida")
		return
	}
	now := time.Now()
	rc.mu.Lock()
	exp, ok := rc.seeds[seed.Valor]
	delete(rc.seeds, seed.Valor) // single use
	rc.mu.Unlock()
	if !ok || now.After(exp) {
		httpx.Error(w, http.StatusBadRequest, "semilla inválida o expirada")
		return
	}

	tb := make([]byte, 32)
	if _, err := rand.Read(tb); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no fue posible generar el token")
		return
	}
	token := hex.EncodeToString(tb)
	expires := now.Add(tokenTTL)
	rc.mu.Lock()
	rc.tokens[token] = session{subject: xmldsig.SubjectID(cert), expires: expires}
	rc.mu.Unlock()

	httpx.Negotiate(w, r, http.StatusOK, respuestaAutenticacion{
		Token:    token,
		Expira:   expires.UTC().Format("2006-01-02T15:04:05Z"),
		Expedido: now.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (rc *Receptor) authorized(r *http.Request) bool {
	if !rc.cfg.RequireAuth {
		return true
	}
	h := r.Header.Get("Authorization")
	if len(h) < 7 || !strings.EqualFold(h[:7], "bearer ") {
		return false
	}
	tok := strings.TrimSpace(h[7:])
	rc.mu.Lock()
	s, ok := rc.tokens[tok]
	rc.mu.Unlock()
	return ok && time.Now().Before(s.expires)
}

// recepcion receives an e-CF and answers with a signed ARECF.
func (rc *Receptor) recepcion(w http.ResponseWriter, r *http.Request) {
	if !rc.authorized(r) {
		httpx.Error(w, http.StatusUnauthorized, "token inválido o ausente")
		return
	}
	data, _, err := httpx.ReadXMLUpload(r)
	if err != nil || len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "archivo no válido")
		return
	}
	doc, parseErr := ecf.Unmarshal(data)
	acuse := ecf.ARECF{DetalleAcusedeRecibo: ecf.DetalleAcusedeRecibo{
		Version:              ecf.Version,
		RNCComprador:         rc.cfg.RNC,
		Estado:               ecf.AcuseRecibido,
		FechaHoraAcuseRecibo: ecf.FormatDateTime(time.Now()),
	}}
	d := &acuse.DetalleAcusedeRecibo
	if parseErr == nil {
		d.RNCEmisor = doc.Encabezado.Emisor.RNCEmisor
		d.ENCF = doc.Encabezado.IdDoc.ENCF
		if c := doc.Encabezado.Comprador; c != nil && c.RNCComprador != "" {
			d.RNCComprador = c.RNCComprador
		}
	}
	name := ecf.FileName(d.RNCEmisor, d.ENCF)
	switch {
	case parseErr != nil || d.RNCEmisor == "" || d.ENCF == "" || !conformsToSchema(doc, data):
		d.Estado, d.CodigoMotivoNoRecibido = ecf.AcuseNoRecibido, ecf.MotivoErrorEspecificacion
	case !verifies(data):
		d.Estado, d.CodigoMotivoNoRecibido = ecf.AcuseNoRecibido, ecf.MotivoErrorFirma
	case rc.cfg.RNC != "" && d.RNCComprador != rc.cfg.RNC:
		d.Estado, d.CodigoMotivoNoRecibido = ecf.AcuseNoRecibido, ecf.MotivoRNCNoCorresponde
	default:
		if dup, _ := rc.cfg.Store.Exists(r.Context(), store.RecibidoECF, name); dup {
			d.Estado, d.CodigoMotivoNoRecibido = ecf.AcuseNoRecibido, ecf.MotivoEnvioDuplicado
		} else if err := rc.save(r.Context(), store.RecibidoECF, name, data); err != nil {
			rc.log.Error("guardando e-CF recibido", "error", err, "archivo", name)
			httpx.Error(w, http.StatusInternalServerError, "no fue posible almacenar el e-CF")
			return
		}
	}
	if d.RNCComprador == "" {
		d.RNCComprador = rc.cfg.RNC
	}

	out, err := ecf.Marshal(&acuse)
	if err == nil {
		out, err = xmldsig.Sign(out, rc.cfg.Signer)
	}
	if err != nil {
		rc.log.Error("firmando acuse de recibo", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "no fue posible generar el acuse de recibo")
		return
	}
	if d.RNCEmisor != "" && d.ENCF != "" {
		_ = rc.cfg.Store.Put(r.Context(), store.AcuseEnviado, name, out)
	}
	rc.log.Info("e-CF recibido", "archivo", name, "estado", d.Estado, "motivo", d.CodigoMotivoNoRecibido)
	httpx.XML(w, http.StatusOK, out)
}

// aprobacionComercial receives an ACECF for an e-CF this taxpayer issued.
func (rc *Receptor) aprobacionComercial(w http.ResponseWriter, r *http.Request) {
	if !rc.authorized(r) {
		httpx.Error(w, http.StatusUnauthorized, "token inválido o ausente")
		return
	}
	data, _, err := httpx.ReadXMLUpload(r)
	if err != nil || len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "archivo no válido")
		return
	}
	var ac ecf.ACECF
	if err := xml.Unmarshal(data, &ac); err != nil || ac.DetalleAprobacionComercial.ENCF == "" {
		httpx.Error(w, http.StatusBadRequest, "la estructura del archivo XML no es válida")
		return
	}
	if is := ecf.ValidateXML(xsd.ACECF, data); is.HasErrors() {
		httpx.ErrorDetail(w, http.StatusBadRequest, "la estructura del archivo XML no es válida, verificar el XSD correspondiente", is)
		return
	}
	if !verifies(data) {
		httpx.Error(w, http.StatusBadRequest, "la firma del XML no es válida")
		return
	}
	det := &ac.DetalleAprobacionComercial
	if rc.cfg.RNC != "" && det.RNCEmisor != rc.cfg.RNC {
		httpx.Error(w, http.StatusBadRequest, "el RNC emisor no corresponde a este receptor")
		return
	}
	// The approval must match the e-CF we issued (Formato Aprobación
	// Comercial, validaciones de los campos 2-6).
	issued, err := rc.cfg.Store.Get(r.Context(), store.EmitidoECF, ecf.FileName(det.RNCEmisor, det.ENCF))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "Factura no encontrada para esta Aprobación comercial.")
		return
	}
	if ref, err := ecf.Unmarshal(issued); err == nil {
		if is := ecf.ValidateACECF(&ac, ref); is.HasErrors() {
			httpx.ErrorDetail(w, http.StatusBadRequest, "la aprobación comercial no corresponde al e-CF emitido", is)
			return
		}
	}
	name := ecf.FileName(det.RNCComprador, det.ENCF)
	if err := rc.save(r.Context(), store.RecibidoACECF, name, data); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "no fue posible almacenar la aprobación comercial")
		return
	}
	rc.log.Info("aprobación comercial recibida", "archivo", name, "estado", det.Estado)
	if rc.cfg.OnAprobacion != nil {
		rc.cfg.OnAprobacion(r.Context(), &ac)
	}
	w.WriteHeader(http.StatusOK)
}

func (rc *Receptor) save(ctx context.Context, kind store.Kind, name string, data []byte) error {
	if err := rc.cfg.Store.Put(ctx, kind, name, data); err != nil {
		return err
	}
	if rc.cfg.Notify != nil {
		rc.cfg.Notify(ctx, kind, name, data)
	}
	return nil
}

// conformsToSchema validates an incoming e-CF against the XSD of its type.
func conformsToSchema(doc *ecf.ECF, data []byte) bool {
	d, ok := xsd.ForTipo(int(doc.Encabezado.IdDoc.TipoeCF))
	if !ok {
		return false
	}
	return !ecf.ValidateXML(d, data).HasErrors()
}

func verifies(data []byte) bool {
	_, err := xmldsig.Verify(data)
	return err == nil
}

// gc drops expired seeds and tokens. Caller holds rc.mu.
func (rc *Receptor) gc(now time.Time) {
	if len(rc.seeds) < 1024 && len(rc.tokens) < 1024 {
		return
	}
	for k, exp := range rc.seeds {
		if now.After(exp) {
			delete(rc.seeds, k)
		}
	}
	for k, s := range rc.tokens {
		if now.After(s.expires) {
			delete(rc.tokens, k)
		}
	}
}
