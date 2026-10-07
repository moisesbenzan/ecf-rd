// Package emision orchestrates the issuance of e-CF: completion of derived
// fields, validation, signing, storage and submission to DGII.
package emision

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// ErrInvalid is returned (wrapped in *ValidationError) when a document fails
// validation.
var ErrInvalid = errors.New("emision: el documento no es válido")

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
	// Enviar submits the signed document to DGII.
	Enviar bool
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
	// summary, while the full e-CF is kept by the issuer and delivered to
	// the buyer.
	EsResumen    bool       `json:"esResumen"`
	XML          string     `json:"xml"`
	XMLResumen   string     `json:"xmlResumen,omitempty"`
	Envio        *Envio     `json:"envio,omitempty"`
	Advertencias ecf.Issues `json:"advertencias,omitempty"`
}

// Envio describes the submission to DGII.
type Envio struct {
	TrackID   string `json:"trackId,omitempty"`
	Estado    string `json:"estado,omitempty"`
	Respuesta any    `json:"respuesta,omitempty"`
	Error     string `json:"error,omitempty"`
}

// Preparar completes (optionally) and validates e without signing it.
func (s *Service) Preparar(e *ecf.ECF, completar bool) (ecf.Issues, error) {
	if completar {
		if err := ecf.Complete(e); err != nil {
			return nil, fmt.Errorf("emision: completando el documento: %w", err)
		}
	}
	issues := ecf.Validate(e)
	if s.RNC != "" && e.Encabezado.Emisor.RNCEmisor != s.RNC {
		issues = append(issues, ecf.Issue{
			Field: "Emisor.RNCEmisor", Severity: ecf.SeverityError,
			Message: fmt.Sprintf("este servicio solo emite para el RNC %s", s.RNC),
		})
	}
	if issues.HasErrors() {
		return issues, &ValidationError{Issues: issues}
	}
	return issues, nil
}

// Emitir completes, validates, signs, stores and optionally submits e.
func (s *Service) Emitir(ctx context.Context, e *ecf.ECF, opt Options) (*Resultado, error) {
	if s.Signer == nil {
		return nil, errors.New("emision: no hay certificado digital configurado")
	}
	if e.FechaHoraFirma == "" {
		e.FechaHoraFirma = ecf.FormatDateTime(s.now())
	}
	issues, err := s.Preparar(e, opt.Completar)
	if err != nil {
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

	h := &e.Encabezado
	res := &Resultado{
		ENCF:            h.IdDoc.ENCF,
		TipoeCF:         int(h.IdDoc.TipoeCF),
		Archivo:         ecf.FileName(h.Emisor.RNCEmisor, h.IdDoc.ENCF),
		FechaHoraFirma:  e.FechaHoraFirma,
		CodigoSeguridad: code,
		MontoTotal:      h.Totales.MontoTotal.String(),
		XML:             string(signed),
		Advertencias:    issues,
	}
	if err := s.Store.Put(ctx, store.EmitidoECF, res.Archivo, signed); err != nil {
		return nil, fmt.Errorf("emision: almacenando: %w", err)
	}

	var signedRFCE []byte
	if h.IdDoc.TipoeCF == ecf.FacturaConsumo && h.Totales.MontoTotal.Cmp(ecf.UmbralConsumo) < 0 {
		res.EsResumen = true
		res.URLTimbre = ecf.TimbreFCURL(s.Endpoints.ConsultaTimbreFC, e, code)
		rfce, err := ecf.Marshal(ecf.NewRFCE(e, code))
		if err != nil {
			return nil, err
		}
		if signedRFCE, err = xmldsig.Sign(rfce, s.Signer); err != nil {
			return nil, err
		}
		res.XMLResumen = string(signedRFCE)
		if err := s.Store.Put(ctx, store.EmitidoRFCE, res.Archivo, signedRFCE); err != nil {
			return nil, fmt.Errorf("emision: almacenando resumen: %w", err)
		}
	} else {
		res.URLTimbre = ecf.TimbreURL(s.Endpoints.ConsultaTimbre, e, code)
	}

	if opt.Enviar {
		res.Envio = s.enviar(ctx, res, signed, signedRFCE)
	}
	return res, nil
}

func (s *Service) enviar(ctx context.Context, res *Resultado, signed, signedRFCE []byte) *Envio {
	if s.DGII == nil {
		return &Envio{Error: "el envío a DGII no está configurado"}
	}
	if res.EsResumen {
		r, err := s.DGII.EnviarRFCE(ctx, res.Archivo, signedRFCE)
		env := &Envio{Estado: r.Estado, Respuesta: r}
		if err != nil {
			env.Error = err.Error()
		}
		return env
	}
	r, err := s.DGII.EnviarECF(ctx, res.Archivo, signed)
	env := &Envio{TrackID: r.TrackID, Respuesta: r}
	if err != nil {
		env.Error = err.Error()
	}
	return env
}

// FirmarAprobacion signs an ACECF and optionally submits it to DGII.
func (s *Service) FirmarAprobacion(ctx context.Context, a *ecf.ACECF, enviar bool) ([]byte, *Envio, error) {
	d := &a.DetalleAprobacionComercial
	if d.Version == "" {
		d.Version = ecf.Version
	}
	if d.FechaHoraAprobacionComercial == "" {
		d.FechaHoraAprobacionComercial = ecf.FormatDateTime(s.now())
	}
	if _, err := ecf.ParseENCF(d.ENCF); err != nil {
		return nil, nil, err
	}
	if d.Estado != ecf.AprobacionAceptado && d.Estado != ecf.AprobacionRechazado {
		return nil, nil, errors.New("Estado debe ser 1 (aceptado) o 2 (rechazado)")
	}
	if d.Estado == ecf.AprobacionRechazado && d.DetalleMotivoRechazo == "" {
		return nil, nil, errors.New("DetalleMotivoRechazo es obligatorio al rechazar")
	}
	if s.RNC != "" && d.RNCComprador != s.RNC {
		return nil, nil, fmt.Errorf("RNCComprador debe ser %s", s.RNC)
	}
	return s.signAndSend(ctx, a, store.EmitidoACECF, ecf.FileName(d.RNCComprador, d.ENCF), enviar,
		func(name string, b []byte) (any, string, error) {
			r, err := s.DGII.EnviarAprobacionComercial(ctx, name, b)
			return r, r.Estado, err
		})
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
	if n := len(a.DetalleAnulacion.Anulacion); n == 0 || n > 10 {
		return nil, nil, errors.New("DetalleAnulacion debe contener entre 1 y 10 anulaciones")
	}
	if err := a.Fill(); err != nil {
		return nil, nil, err
	}
	name := a.Encabezado.RncEmisor + "ANECF" + s.now().Format("20060102150405") + ".xml"
	return s.signAndSend(ctx, a, store.EmitidoANECF, name, enviar,
		func(name string, b []byte) (any, string, error) {
			r, err := s.DGII.AnularRangos(ctx, name, b)
			return r, r.Nombre, err
		})
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
