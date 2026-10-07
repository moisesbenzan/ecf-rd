// Package emision orchestrates the issuance of e-CF: completion of derived
// fields, validation, signing, storage, submission to DGII through the
// tracker, and the receiver-side commercial approval flow.
package emision

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/seguimiento"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// ErrInvalid is returned (wrapped in *ValidationError) when a document fails
// validation.
var ErrInvalid = errors.New("emision: el documento no es válido")

// ErrDuplicado is returned when an e-NCF was already issued.
var ErrDuplicado = errors.New("emision: el e-NCF ya fue emitido")

// ValidationError carries the validation findings.
type ValidationError struct {
	Issues ecf.Issues
}

func (e *ValidationError) Error() string { return "documento inválido: " + e.Issues.Error() }

// Unwrap returns ErrInvalid.
func (e *ValidationError) Unwrap() error { return ErrInvalid }

// Service issues documents for a single taxpayer.
type Service struct {
	RNC       string // issuer RNC; documents for other RNCs are rejected
	Signer    *xmldsig.Signer
	DGII      *dgii.Client // nil disables submission
	Receptor  *dgii.ReceptorClient
	Tracker   *seguimiento.Tracker // nil disables submission tracking
	Store     store.Store
	Endpoints dgii.Endpoints
	Now       func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Options control Emitir.
type Options struct {
	// Completar computes line amounts and totals before validating.
	Completar bool
	// Estricto rejects amounts outside DGII's tolerance rule instead of
	// accepting a conditional acceptance.
	Estricto bool
	// Enviar submits the signed document to DGII (asynchronously).
	Enviar bool
	// Esperar waits up to this long for DGII's final validation result.
	Esperar time.Duration
	// LineasPorPagina builds the Paginacion section for printed
	// representations with more than one page (0 = none).
	LineasPorPagina int
}

// Resultado describes an issued document.
type Resultado struct {
	ENCF            string `json:"eNCF"`
	TipoeCF         int    `json:"tipoeCF"`
	Archivo         string `json:"archivo"`
	FechaHoraFirma  string `json:"fechaHoraFirma"`
	CodigoSeguridad string `json:"codigoSeguridad"`
	URLTimbre       string `json:"urlTimbre"`
	MontoTotal      string `json:"montoTotal"`
	// EsResumen is true for type 32 below RD$250,000: DGII receives the RFCE
	// summary, while the full e-CF is kept by the issuer and given to the
	// buyer.
	EsResumen    bool                  `json:"esResumen"`
	XML          string                `json:"xml"`
	XMLResumen   string                `json:"xmlResumen,omitempty"`
	Seguimiento  *seguimiento.Registro `json:"seguimiento,omitempty"`
	Advertencias ecf.Issues            `json:"advertencias,omitempty"`
}

// Envio describes a synchronous submission (approvals, voiding).
type Envio struct {
	Estado    string `json:"estado,omitempty"`
	Respuesta any    `json:"respuesta,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Preparar completes (optionally) and validates e without signing it.
func (s *Service) Preparar(ctx context.Context, e *ecf.ECF, opt Options) (ecf.Issues, error) {
	if opt.LineasPorPagina > 0 {
		ecf.Paginar(e, opt.LineasPorPagina)
	}
	if opt.Completar {
		if err := ecf.Complete(e); err != nil {
			return nil, fmt.Errorf("emision: completando el documento: %w", err)
		}
	}
	issues := ecf.ValidateWith(e, ecf.Options{Estricto: opt.Estricto, Ahora: s.now()})
	if s.RNC != "" && e.Encabezado.Emisor.RNCEmisor != s.RNC {
		issues = append(issues, ecf.Issue{
			Field: "Emisor.RNCEmisor", Severity: ecf.SeverityError,
			Message: fmt.Sprintf("este servicio solo emite para el RNC %s", s.RNC),
		})
	}
	issues = append(issues, s.checkModificado(ctx, e)...)
	if issues.HasErrors() {
		return issues, &ValidationError{Issues: issues}
	}
	return issues, nil
}

// checkModificado verifies a credit note against the modified e-CF when it
// was issued by this service: its total cannot exceed the original
// (Formato e-CF, campo Monto Total, validación d).
func (s *Service) checkModificado(ctx context.Context, e *ecf.ECF) ecf.Issues {
	ref := e.InformacionReferencia
	if ref == nil || e.Encabezado.IdDoc.TipoeCF != ecf.NotaCredito || len(ref.NCFModificado) != 13 {
		return nil
	}
	rnc := e.Encabezado.Emisor.RNCEmisor
	if ref.RNCOtroContribuyente != "" {
		rnc = ref.RNCOtroContribuyente
	}
	b, err := s.Store.Get(ctx, store.EmitidoECF, ecf.FileName(rnc, ref.NCFModificado))
	if err != nil {
		return nil
	}
	orig, err := ecf.Unmarshal(b)
	if err != nil {
		return nil
	}
	var out ecf.Issues
	if e.Encabezado.Totales.MontoTotal.Cmp(orig.Encabezado.Totales.MontoTotal) > 0 {
		out = append(out, ecf.Issue{Field: "Totales.MontoTotal", Severity: ecf.SeverityError,
			Message: fmt.Sprintf("la nota de crédito no puede exceder el monto total del e-CF modificado (%s)", orig.Encabezado.Totales.MontoTotal)})
	}
	if orig.Encabezado.Emisor.FechaEmision != ref.FechaNCFModificado {
		out = append(out, ecf.Issue{Field: "InformacionReferencia.FechaNCFModificado", Severity: ecf.SeverityError,
			Message: fmt.Sprintf("no coincide con la fecha de emisión del e-CF modificado (%s)", orig.Encabezado.Emisor.FechaEmision)})
	}
	return out
}

// Emitir completes, validates, signs, stores and optionally submits e.
func (s *Service) Emitir(ctx context.Context, e *ecf.ECF, opt Options) (*Resultado, error) {
	if s.Signer == nil {
		return nil, errors.New("emision: no hay certificado digital configurado")
	}
	if err := xmldsig.CheckValidity(s.Signer.Certificate, s.now()); err != nil {
		return nil, err
	}
	if e.FechaHoraFirma == "" {
		e.FechaHoraFirma = ecf.FormatDateTime(s.now())
	}
	issues, err := s.Preparar(ctx, e, opt)
	if err != nil {
		return nil, err
	}
	h := &e.Encabezado
	archivo := ecf.FileName(h.Emisor.RNCEmisor, h.IdDoc.ENCF)
	if err := s.checkDuplicado(ctx, archivo); err != nil {
		return nil, err
	}

	unsigned, err := ecf.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("emision: serializando: %w", err)
	}
	signed, err := xmldsig.Sign(unsigned, s.Signer)
	if err != nil {
		return nil, err
	}
	code, err := xmldsig.SecurityCode(signed)
	if err != nil {
		return nil, err
	}

	res := &Resultado{
		ENCF:            h.IdDoc.ENCF,
		TipoeCF:         int(h.IdDoc.TipoeCF),
		Archivo:         archivo,
		FechaHoraFirma:  e.FechaHoraFirma,
		CodigoSeguridad: code,
		MontoTotal:      h.Totales.MontoTotal.String(),
		XML:             string(signed),
		Advertencias:    issues,
	}

	var signedRFCE []byte
	if EsResumen(e) {
		res.EsResumen = true
		res.URLTimbre = ecf.TimbreFCURL(s.Endpoints.ConsultaTimbreFC, e, code)
		r := ecf.NewRFCE(e, code)
		if is := ecf.ValidateRFCE(r); is.HasErrors() {
			return nil, &ValidationError{Issues: is}
		}
		rfce, err := ecf.Marshal(r)
		if err != nil {
			return nil, err
		}
		if signedRFCE, err = xmldsig.Sign(rfce, s.Signer); err != nil {
			return nil, err
		}
		res.XMLResumen = string(signedRFCE)
	} else {
		res.URLTimbre = ecf.TimbreURL(s.Endpoints.ConsultaTimbre, e, code)
	}

	// Persist before any submission: DGII requires keeping the signed XML.
	if err := s.Store.Put(ctx, store.EmitidoECF, archivo, signed); err != nil {
		return nil, fmt.Errorf("emision: almacenando: %w", err)
	}
	if signedRFCE != nil {
		if err := s.Store.Put(ctx, store.EmitidoRFCE, archivo, signedRFCE); err != nil {
			return nil, fmt.Errorf("emision: almacenando resumen: %w", err)
		}
	}

	if opt.Enviar {
		if s.Tracker == nil {
			return nil, errors.New("emision: el envío a DGII no está configurado")
		}
		reg := &seguimiento.Registro{
			Archivo: archivo, ENCF: h.IdDoc.ENCF, TipoeCF: int(h.IdDoc.TipoeCF),
			RNCEmisor: h.Emisor.RNCEmisor, FechaEmision: h.Emisor.FechaEmision,
			FechaHoraFirma: e.FechaHoraFirma, MontoTotal: res.MontoTotal,
			CodigoSeguridad: code, Resumen: res.EsResumen,
		}
		if h.Comprador != nil {
			reg.RNCComprador = h.Comprador.RNCComprador
		}
		if err := s.Tracker.Submit(ctx, reg); err != nil {
			return nil, err
		}
		if opt.Esperar > 0 {
			wctx, cancel := context.WithTimeout(ctx, opt.Esperar)
			res.Seguimiento, _ = s.Tracker.Wait(wctx, archivo)
			cancel()
		} else {
			res.Seguimiento, _ = s.Tracker.Get(archivo)
		}
	}
	return res, nil
}

// EsResumen reports whether DGII receives the RFCE summary for e (type 32
// below RD$250,000).
func EsResumen(e *ecf.ECF) bool {
	return e.Encabezado.IdDoc.TipoeCF == ecf.FacturaConsumo && e.Encabezado.Totales.MontoTotal.Cmp(ecf.UmbralConsumo) < 0
}

// checkDuplicado rejects issuing an e-NCF twice, unless DGII rejected the
// previous submission and reported that the sequence can be reused
// (secuenciaUtilizada = false).
func (s *Service) checkDuplicado(ctx context.Context, archivo string) error {
	exists, err := s.Store.Exists(ctx, store.EmitidoECF, archivo)
	if err != nil || !exists {
		return err
	}
	if s.Tracker != nil {
		if r, ok := s.Tracker.Get(archivo); ok && (r.Estado == seguimiento.Rechazado || r.Estado == seguimiento.ErrorEnvio) &&
			(r.SecuenciaUtilizada == nil || !*r.SecuenciaUtilizada) {
			return nil
		}
	}
	return ErrDuplicado
}

// FirmarAprobacion signs an ACECF given in full and optionally submits it
// to DGII.
func (s *Service) FirmarAprobacion(ctx context.Context, a *ecf.ACECF, enviar bool) ([]byte, *Envio, error) {
	d := &a.DetalleAprobacionComercial
	if d.Version == "" {
		d.Version = ecf.Version
	}
	if d.FechaHoraAprobacionComercial == "" {
		d.FechaHoraAprobacionComercial = ecf.FormatDateTime(s.now())
	}
	if s.RNC != "" && d.RNCComprador != s.RNC {
		return nil, nil, fmt.Errorf("RNCComprador debe ser %s", s.RNC)
	}
	var ref *ecf.ECF
	if b, err := s.Store.Get(ctx, store.RecibidoECF, ecf.FileName(d.RNCEmisor, d.ENCF)); err == nil {
		ref, _ = ecf.Unmarshal(b)
	}
	if is := ecf.ValidateACECF(a, ref); is.HasErrors() {
		return nil, nil, &ValidationError{Issues: is}
	}
	return s.signAndSend(ctx, a, store.EmitidoACECF, ecf.FileName(d.RNCComprador, d.ENCF), enviar,
		func(name string, b []byte) (any, string, error) {
			r, err := s.DGII.EnviarAprobacionComercial(ctx, name, b)
			return r, r.Estado, err
		})
}

// ResultadoAprobacion reports where a commercial approval was delivered.
type ResultadoAprobacion struct {
	XML     string `json:"xml"`
	DGII    *Envio `json:"dgii,omitempty"`
	Emisor  *Envio `json:"emisor,omitempty"`
	Archivo string `json:"archivo"`
}

// AprobarRecibido builds, signs and sends the commercial approval of an
// e-CF received by this taxpayer (estado 1 = aceptado, 2 = rechazado): to
// the issuer's aprobacioncomercial service published in the DGII directory
// and to DGII (Modelo de Operación, pasos 5 y 6).
func (s *Service) AprobarRecibido(ctx context.Context, archivo string, estado int, motivo string, enviar bool) (*ResultadoAprobacion, error) {
	if s.Signer == nil {
		return nil, errors.New("emision: no hay certificado digital configurado")
	}
	b, err := s.Store.Get(ctx, store.RecibidoECF, archivo)
	if err != nil {
		return nil, err
	}
	ref, err := ecf.Unmarshal(b)
	if err != nil {
		return nil, err
	}
	h := &ref.Encabezado
	if h.Comprador == nil || h.Comprador.RNCComprador == "" {
		return nil, errors.New("el e-CF recibido no identifica al comprador")
	}
	a := &ecf.ACECF{DetalleAprobacionComercial: ecf.DetalleAprobacionComercial{
		Version: ecf.Version, RNCEmisor: h.Emisor.RNCEmisor, ENCF: h.IdDoc.ENCF,
		FechaEmision: h.Emisor.FechaEmision, MontoTotal: h.Totales.MontoTotal,
		RNCComprador: h.Comprador.RNCComprador, Estado: estado, DetalleMotivoRechazo: motivo,
		FechaHoraAprobacionComercial: ecf.FormatDateTime(s.now()),
	}}
	if is := ecf.ValidateACECF(a, ref); is.HasErrors() {
		return nil, &ValidationError{Issues: is}
	}
	unsigned, err := ecf.Marshal(a)
	if err != nil {
		return nil, err
	}
	signed, err := xmldsig.Sign(unsigned, s.Signer)
	if err != nil {
		return nil, err
	}
	name := ecf.FileName(h.Comprador.RNCComprador, h.IdDoc.ENCF)
	if err := s.Store.Put(ctx, store.EmitidoACECF, name, signed); err != nil {
		return nil, err
	}
	out := &ResultadoAprobacion{XML: string(signed), Archivo: name}
	if !enviar {
		return out, nil
	}
	if s.DGII == nil {
		return nil, errors.New("emision: el envío a DGII no está configurado")
	}
	// DGII only receives approvals of e-CF it accepted (Informe Técnico,
	// sección 8, nota 10): check the status first when DGII can answer.
	if code, err := xmldsig.SecurityCode(b); err == nil {
		if st, err := s.DGII.ConsultarEstado(ctx, h.Emisor.RNCEmisor, h.IdDoc.ENCF, h.Comprador.RNCComprador, code); err == nil &&
			st.Codigo != dgii.EstadoAceptado && st.Codigo != dgii.EstadoAceptadoCondicional {
			return nil, fmt.Errorf("el e-CF no ha sido aceptado por DGII (estado: %s); no se puede enviar la aprobación comercial", st.Estado)
		}
	}
	// Issuer first, then DGII.
	out.Emisor = &Envio{}
	if dir, err := s.DGII.DirectorioPorRNC(ctx, h.Emisor.RNCEmisor); err != nil {
		out.Emisor.Error = "consultando directorio: " + err.Error()
	} else if len(dir) == 0 || dir[0].URLAceptacion == "" || s.Receptor == nil {
		out.Emisor.Estado = "emisor sin servicio de aprobación comercial publicado"
	} else if err := s.Receptor.EnviarAprobacionComercial(ctx, dgii.DestinoFrom(dir[0]), name, signed); err != nil {
		out.Emisor.Error = err.Error()
	} else {
		out.Emisor.Estado = "entregada"
	}
	r, err := s.DGII.EnviarAprobacionComercial(ctx, name, signed)
	out.DGII = &Envio{Estado: r.Estado, Respuesta: r}
	if err != nil {
		out.DGII.Error = err.Error()
	}
	return out, nil
}

// Recibido summarizes a received e-CF.
type Recibido struct {
	Archivo     string `json:"archivo"`
	ENCF        string `json:"eNCF"`
	TipoeCF     int    `json:"tipoeCF"`
	RNCEmisor   string `json:"rncEmisor"`
	RazonSocial string `json:"razonSocialEmisor"`
	Fecha       string `json:"fechaEmision"`
	MontoTotal  string `json:"montoTotal"`
	Aprobacion  *int   `json:"aprobacionComercial,omitempty"`
}

// ListarRecibidos lists the e-CF received by this taxpayer.
func (s *Service) ListarRecibidos(ctx context.Context) ([]Recibido, error) {
	names, err := s.Store.List(ctx, store.RecibidoECF)
	if err != nil {
		return nil, err
	}
	out := make([]Recibido, 0, len(names))
	for _, n := range names {
		b, err := s.Store.Get(ctx, store.RecibidoECF, n)
		if err != nil {
			continue
		}
		e, err := ecf.Unmarshal(b)
		if err != nil {
			continue
		}
		h := &e.Encabezado
		r := Recibido{Archivo: n, ENCF: h.IdDoc.ENCF, TipoeCF: int(h.IdDoc.TipoeCF), RNCEmisor: h.Emisor.RNCEmisor,
			RazonSocial: h.Emisor.RazonSocialEmisor, Fecha: h.Emisor.FechaEmision, MontoTotal: h.Totales.MontoTotal.String()}
		if h.Comprador != nil {
			if ab, err := s.Store.Get(ctx, store.EmitidoACECF, ecf.FileName(h.Comprador.RNCComprador, h.IdDoc.ENCF)); err == nil {
				var a ecf.ACECF
				if xml.Unmarshal(ab, &a) == nil {
					st := a.DetalleAprobacionComercial.Estado
					r.Aprobacion = &st
				}
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// Anular signs an ANECF and optionally submits it to DGII.
func (s *Service) Anular(ctx context.Context, a *ecf.ANECF, enviar bool) ([]byte, *Envio, error) {
	if s.RNC != "" && a.Encabezado.RncEmisor == "" {
		a.Encabezado.RncEmisor = s.RNC
	}
	if s.RNC != "" && a.Encabezado.RncEmisor != s.RNC {
		return nil, nil, fmt.Errorf("RncEmisor debe ser %s", s.RNC)
	}
	if a.Encabezado.FechaHoraAnulacioneNCF == "" {
		a.Encabezado.FechaHoraAnulacioneNCF = ecf.FormatDateTime(s.now())
	}
	if err := a.Fill(); err != nil {
		return nil, nil, err
	}
	if is := ecf.ValidateANECF(a); is.HasErrors() {
		return nil, nil, &ValidationError{Issues: is}
	}
	name := a.Encabezado.RncEmisor + "ANECF" + s.now().Format("20060102150405") + ".xml"
	signed, env, err := s.signAndSend(ctx, a, store.EmitidoANECF, name, enviar,
		func(name string, b []byte) (any, string, error) {
			r, err := s.DGII.AnularRangos(ctx, name, b)
			return r, r.Nombre, err
		})
	if err == nil && enviar && env != nil && env.Error == "" && s.Tracker != nil {
		// Signed documents that never reached DGII must not be sent anymore.
		s.Tracker.MarcarAnulados(ctx, a.Encabezado.RncEmisor, func(encf string) bool { return AnuladoEn(a, encf) })
	}
	return signed, env, err
}

// AnuladoEn reports whether encf falls in one of the voided ranges of a.
func AnuladoEn(a *ecf.ANECF, encf string) bool {
	n, err := ecf.ParseENCF(encf)
	if err != nil {
		return false
	}
	for _, an := range a.DetalleAnulacion.Anulacion {
		if an.TipoeCF != n.Tipo {
			continue
		}
		for _, sq := range an.TablaRangoSecuenciasAnuladaseNCF.Secuencias {
			d, e1 := ecf.ParseENCF(sq.SecuenciaeNCFDesde)
			h, e2 := ecf.ParseENCF(sq.SecuenciaeNCFHasta)
			if e1 == nil && e2 == nil && d.Serie == n.Serie && n.Secuencia >= d.Secuencia && n.Secuencia <= h.Secuencia {
				return true
			}
		}
	}
	return false
}

func (s *Service) signAndSend(ctx context.Context, doc any, kind store.Kind, name string, enviar bool,
	send func(name string, b []byte) (any, string, error)) ([]byte, *Envio, error) {
	if s.Signer == nil {
		return nil, nil, errors.New("emision: no hay certificado digital configurado")
	}
	b, err := ecf.Marshal(doc)
	if err != nil {
		return nil, nil, err
	}
	signed, err := xmldsig.Sign(b, s.Signer)
	if err != nil {
		return nil, nil, err
	}
	if err := s.Store.Put(ctx, kind, name, signed); err != nil {
		return nil, nil, err
	}
	if !enviar {
		return signed, nil, nil
	}
	if s.DGII == nil {
		return signed, &Envio{Error: "el envío a DGII no está configurado"}, nil
	}
	resp, estado, err := send(name, signed)
	env := &Envio{Estado: estado, Respuesta: resp}
	if err != nil {
		env.Error = err.Error()
	}
	return signed, env, nil
}
