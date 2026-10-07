package ecf

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
	"github.com/moisesbenzan/ecf-rd/internal/xsd"
)

// Severity of a validation issue.
type Severity string

// Severities.
const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// Issue is a single validation finding.
type Issue struct {
	Field    string   `json:"campo"`
	Message  string   `json:"mensaje"`
	Severity Severity `json:"severidad"`
}

// Issues is a list of validation findings.
type Issues []Issue

// HasErrors reports whether any issue has error severity.
func (is Issues) HasErrors() bool {
	for _, i := range is {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Error implements error, joining every error-level message.
func (is Issues) Error() string {
	var b strings.Builder
	for _, i := range is {
		if i.Severity != SeverityError {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("; ")
		}
		b.WriteString(i.Field)
		b.WriteString(": ")
		b.WriteString(i.Message)
	}
	return b.String()
}

// Options tune validation.
type Options struct {
	// Estricto reports amounts that exceed DGII's tolerance rule as errors.
	// DGII accepts such documents conditionally ("Aceptado Condicional");
	// with Estricto=false they are reported as warnings instead.
	Estricto bool
	// SinEsquema skips the XSD validation stage.
	SinEsquema bool
	// Ahora is the reference time for date checks (default time.Now).
	Ahora time.Time
}

// DefaultOptions are the options used by Validate.
var DefaultOptions = Options{Estricto: true}

// Obligatoriedad codes as defined by the DGII format document.
type ob uint8

const (
	no   ob = 0 // must not be present
	req  ob = 1 // mandatory
	cond ob = 2 // conditional
	opt  ob = 3 // optional
)

// rule is a per-type obligatoriedad row, ordered 31,32,33,34,41,43,44,45,46,47.
type rule [10]ob

func (r rule) For(t TipoECF) ob {
	switch t {
	case 31:
		return r[0]
	case 32:
		return r[1]
	case 33:
		return r[2]
	case 34:
		return r[3]
	case 41:
		return r[4]
	case 43:
		return r[5]
	case 44:
		return r[6]
	case 45:
		return r[7]
	case 46:
		return r[8]
	case 47:
		return r[9]
	}
	return opt
}

// Obligatoriedad rows taken from "Formato Comprobante Fiscal Electrónico
// (e-CF) V1.0". Element existence per type is additionally enforced by the
// official XSD of each type.
var (
	rFechaVencimientoSecuencia = rule{req, no, req, no, req, req, req, req, req, req}
	rIndicadorNotaCredito      = rule{no, no, no, req, no, no, no, no, no, no}
	rIndicadorEnvioDiferido    = rule{cond, cond, cond, cond, no, no, cond, cond, cond, no}
	rIndicadorMontoGravado     = rule{cond, cond, cond, cond, cond, no, no, cond, no, no}
	rTipoIngresos              = rule{req, req, req, req, no, no, req, req, req, no}
	rTipoPago                  = rule{req, req, req, req, req, opt, req, req, req, opt}
	rFechaLimitePago           = rule{cond, cond, cond, cond, cond, no, cond, cond, cond, opt}
	rTerminoPago               = rule{opt, opt, opt, no, opt, no, opt, opt, opt, opt}
	rTablaFormasPago           = rule{opt, opt, opt, no, opt, no, opt, opt, opt, opt}
	rPeriodo                   = rule{opt, opt, opt, opt, no, no, opt, opt, opt, opt}
	rRNCComprador              = rule{req, cond, cond, cond, req, no, cond, req, cond, no}
	rIdentificadorExtranjero   = rule{no, cond, cond, cond, no, no, cond, no, cond, opt}
	rRazonSocialComprador      = rule{req, cond, cond, cond, req, no, req, req, req, opt}
	rInformacionReferencia     = rule{cond, cond, req, req, cond, cond, cond, cond, cond, cond}
	rRetencion                 = rule{cond, no, cond, cond, req, no, no, no, no, req}
	rTransporte                = rule{opt, opt, opt, opt, no, no, opt, opt, opt, opt}
	rInfoAdicionales           = rule{opt, opt, opt, opt, no, no, opt, opt, opt, opt}
	rImpuestoAdicional         = rule{cond, cond, cond, cond, no, no, cond, cond, no, no}
	rMineria                   = rule{no, cond, cond, cond, no, no, no, no, cond, no}
	rDescuentosORecargos       = rule{cond, cond, cond, cond, cond, no, cond, cond, cond, no}
	rTotalITBISRetenido        = rule{cond, no, cond, cond, cond, no, no, no, no, no}
	rTotalISRRetencion         = rule{cond, no, cond, cond, cond, no, no, no, no, cond}
)

var (
	reTelefono = regexp.MustCompile(`^\d{3}-\d{3}-\d{4}$`)
	reCorreo   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// LineTolerance is the per-line tolerance of DGII's "regla de tolerancia":
// ± 1 unit between MontoItem and price × quantity.
var LineTolerance = decimal.FromInt(1)

type validator struct {
	t      TipoECF
	opt    Options
	issues Issues
	tol    decimal.Decimal // global tolerance: number of detail lines
}

func (v *validator) err(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{Field: field, Message: fmt.Sprintf(format, args...), Severity: SeverityError})
}

func (v *validator) warn(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{Field: field, Message: fmt.Sprintf(format, args...), Severity: SeverityWarning})
}

// tolerance reports an amount outside DGII's tolerance rule.
func (v *validator) tolerance(field string, got, want, tol decimal.Decimal) {
	if got.Sub(want).Abs().Cmp(tol) <= 0 {
		return
	}
	msg := fmt.Sprintf("valor %s difiere del calculado %s más allá de la tolerancia (± %s); DGII lo aceptaría de forma condicional", got, want, tol)
	if v.opt.Estricto {
		v.err(field, "%s", msg)
	} else {
		v.warn(field, "%s", msg)
	}
}

// presence enforces "must be present" (1) and "must be absent" (0).
func (v *validator) presence(field string, r rule, present bool) {
	switch r.For(v.t) {
	case req:
		if !present {
			v.err(field, "es obligatorio para el tipo %d", v.t)
		}
	case no:
		if present {
			v.err(field, "no corresponde para el tipo %d", v.t)
		}
	}
}

func (v *validator) maxLen(field, s string, n int) {
	if utf8.RuneCountInString(s) > n {
		v.err(field, "excede el largo máximo de %d caracteres", n)
	}
}

func (v *validator) required(field, s string, n int) {
	if strings.TrimSpace(s) == "" {
		v.err(field, "es obligatorio")
		return
	}
	v.maxLen(field, s, n)
}

func (v *validator) date(field, s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := ParseDate(s)
	if err != nil {
		v.err(field, "fecha inválida %q, formato esperado dd-MM-AAAA", s)
		return time.Time{}, false
	}
	return t, true
}

func (v *validator) rnc(field, s string) {
	if !ValidRNCFormat(s) {
		v.err(field, "RNC/Cédula %q inválido: debe tener 9 u 11 dígitos", s)
		return
	}
	if !ValidRNCCheckDigit(s) {
		v.warn(field, "el dígito verificador de %q no es válido", s)
	}
}

// amount checks a monetary value: at most 16 integer digits and 2 decimals.
func (v *validator) amount(field string, d decimal.Decimal, allowNegative bool) {
	if d.Places() > 2 {
		v.err(field, "admite un máximo de 2 decimales (regla de redondeo)")
	}
	if d.IntDigits() > 16 {
		v.err(field, "admite un máximo de 16 enteros")
	}
	if !allowNegative && d.Sign() < 0 {
		v.err(field, "no puede ser negativo")
	}
}

func (v *validator) optAmount(field string, d *decimal.Decimal) {
	if d != nil {
		v.amount(field, *d, false)
	}
}

// geo validates a province or municipality code against Tabla III.
func (v *validator) geo(provField, provincia, munField, municipio string) {
	if provincia != "" {
		if _, ok := ProvinciasMunicipios[provincia]; !ok {
			v.err(provField, "código %q no existe en la Tabla III", provincia)
		} else if !strings.HasSuffix(provincia, "0000") {
			v.err(provField, "%q no es un código de provincia", provincia)
		}
	}
	if municipio != "" {
		if _, ok := ProvinciasMunicipios[municipio]; !ok {
			v.err(munField, "código %q no existe en la Tabla III", municipio)
		} else if strings.HasSuffix(municipio, "0000") {
			v.err(munField, "%q es un código de provincia, no de municipio", municipio)
		} else if provincia != "" && municipio[:2] != provincia[:2] {
			v.err(munField, "el municipio %q no pertenece a la provincia %q", municipio, provincia)
		}
	}
}

// Validate checks e with DefaultOptions.
func Validate(e *ECF) Issues { return ValidateWith(e, DefaultOptions) }

// ValidateWith checks e against the official XSD of its type and the
// business rules of the e-CF format, Informe Técnico and technical
// descriptions. Warnings never block issuance; errors do.
func ValidateWith(e *ECF, opt Options) Issues {
	if opt.Ahora.IsZero() {
		opt.Ahora = time.Now()
	}
	h := &e.Encabezado
	v := &validator{t: h.IdDoc.TipoeCF, opt: opt, tol: decimal.FromInt(int64(len(e.DetallesItems.Item)))}

	if h.Version != Version {
		v.err("Encabezado.Version", "debe ser %q", Version)
	}
	v.validateIdDoc(e)
	v.validateEmisor(&h.Emisor)
	v.validateComprador(e)
	v.presence("Encabezado.Transporte", rTransporte, h.Transporte != nil)
	v.presence("Encabezado.InformacionesAdicionales", rInfoAdicionales, h.InformacionesAdicionales != nil)
	c, cerr := Compute(e)
	if cerr != nil {
		v.err("Totales", "no fue posible calcular los totales: %v", cerr)
	}
	v.validateItems(e, &c)
	v.validateDescuentos(e)
	if cerr == nil {
		v.validateTotales(e, &c)
		v.validateOtraMoneda(e, &c)
		v.validatePaginacion(e, &c)
	}
	v.validateSubtotales(e)
	v.validateReferencia(e)
	v.validateFirma(e)
	if !opt.SinEsquema && h.IdDoc.TipoeCF.Valid() {
		v.issues = append(v.issues, SchemaIssues(e)...)
	}
	return v.issues
}

// SchemaIssues validates the XML serialization of e against the official
// XSD of its type.
func SchemaIssues(e *ECF) Issues {
	doc, ok := xsd.ForTipo(int(e.Encabezado.IdDoc.TipoeCF))
	if !ok {
		return nil
	}
	tmp := *e
	if tmp.FechaHoraFirma == "" {
		tmp.FechaHoraFirma = FormatDateTime(time.Now())
	}
	b, err := Marshal(&tmp)
	if err != nil {
		return Issues{{Field: "XML", Message: err.Error(), Severity: SeverityError}}
	}
	return xsdIssues(doc, b)
}

func xsdIssues(doc xsd.Document, b []byte) Issues {
	errs, err := xsd.Validate(doc, b, xsd.Options{AllowMissingAny: true})
	if err != nil {
		return Issues{{Field: "XSD", Message: err.Error(), Severity: SeverityError}}
	}
	out := make(Issues, 0, len(errs))
	for _, e := range errs {
		out = append(out, Issue{Field: "XSD " + e.Path, Message: e.Message, Severity: SeverityError})
	}
	return out
}

func (v *validator) validateIdDoc(e *ECF) {
	id := &e.Encabezado.IdDoc
	if !id.TipoeCF.Valid() {
		v.err("IdDoc.TipoeCF", "tipo de e-CF inválido: %d", id.TipoeCF)
		return
	}
	if n, err := ParseENCF(id.ENCF); err != nil {
		v.err("IdDoc.eNCF", "%v", err)
	} else if n.Tipo != id.TipoeCF {
		v.err("IdDoc.eNCF", "el tipo del e-NCF (%d) no coincide con TipoeCF (%d)", n.Tipo, id.TipoeCF)
	}

	emision, emisionOK := ParseDate(e.Encabezado.Emisor.FechaEmision)
	v.presence("IdDoc.FechaVencimientoSecuencia", rFechaVencimientoSecuencia, id.FechaVencimientoSecuencia != "")
	if venc, ok := v.date("IdDoc.FechaVencimientoSecuencia", id.FechaVencimientoSecuencia); ok && emisionOK == nil && emision.After(venc) {
		v.err("IdDoc.FechaVencimientoSecuencia", "la secuencia está vencida a la fecha de emisión")
	}

	v.presence("IdDoc.IndicadorNotaCredito", rIndicadorNotaCredito, id.IndicadorNotaCredito != nil)
	if id.IndicadorNotaCredito != nil && *id.IndicadorNotaCredito != 0 && *id.IndicadorNotaCredito != 1 {
		v.err("IdDoc.IndicadorNotaCredito", "debe ser 0 o 1")
	}
	v.presence("IdDoc.IndicadorEnvioDiferido", rIndicadorEnvioDiferido, id.IndicadorEnvioDiferido != nil)
	if id.IndicadorEnvioDiferido != nil && *id.IndicadorEnvioDiferido != 1 {
		v.err("IdDoc.IndicadorEnvioDiferido", "el único valor válido es 1")
	}
	v.presence("IdDoc.IndicadorMontoGravado", rIndicadorMontoGravado, id.IndicadorMontoGravado != nil)
	if id.IndicadorMontoGravado != nil && *id.IndicadorMontoGravado != 0 && *id.IndicadorMontoGravado != 1 {
		v.err("IdDoc.IndicadorMontoGravado", "debe ser 0 o 1")
	}
	hasGravado := false
	for _, it := range e.DetallesItems.Item {
		if it.IndicadorFacturacion >= ITBISTasa1 && it.IndicadorFacturacion <= ITBISTasa3 {
			hasGravado = true
		}
	}
	if hasGravado && id.IndicadorMontoGravado == nil && rIndicadorMontoGravado.For(v.t) == cond {
		v.err("IdDoc.IndicadorMontoGravado", "es obligatorio cuando existen ítems gravados con ITBIS")
	}
	if id.IndicadorServicioTodoIncl != nil && *id.IndicadorServicioTodoIncl != 1 && *id.IndicadorServicioTodoIncl != 2 {
		v.err("IdDoc.IndicadorServicioTodoIncluido", "valor inválido")
	}

	v.presence("IdDoc.TipoIngresos", rTipoIngresos, id.TipoIngresos != "")
	if id.TipoIngresos != "" {
		switch id.TipoIngresos {
		case "01", "02", "03", "04", "05", "06":
		default:
			v.err("IdDoc.TipoIngresos", "valor %q inválido (01-06)", id.TipoIngresos)
		}
	}

	v.presence("IdDoc.TipoPago", rTipoPago, id.TipoPago != 0)
	if id.TipoPago != 0 && (id.TipoPago < 1 || id.TipoPago > 3) {
		v.err("IdDoc.TipoPago", "valor %d inválido (1: Contado, 2: Crédito, 3: Gratuito)", id.TipoPago)
	}
	if id.TipoPago == 3 && id.TipoeCF == FacturaCreditoFiscal {
		v.warn("IdDoc.TipoPago", "las facturas por entrega gratuita no son válidas para crédito fiscal")
	}

	v.presence("IdDoc.FechaLimitePago", rFechaLimitePago, id.FechaLimitePago != "")
	if id.TipoPago == 2 && id.FechaLimitePago == "" && rFechaLimitePago.For(v.t) == cond {
		v.err("IdDoc.FechaLimitePago", "es obligatoria cuando el tipo de pago es a crédito")
	}
	if fl, ok := v.date("IdDoc.FechaLimitePago", id.FechaLimitePago); ok && emisionOK == nil && fl.Before(emision) {
		v.err("IdDoc.FechaLimitePago", "debe ser mayor o igual a la fecha de emisión")
	}

	v.presence("IdDoc.TerminoPago", rTerminoPago, id.TerminoPago != "")
	v.maxLen("IdDoc.TerminoPago", id.TerminoPago, 15)

	v.presence("IdDoc.TablaFormasPago", rTablaFormasPago, id.TablaFormasPago != nil)
	if fp := id.TablaFormasPago; fp != nil {
		if n := len(fp.FormaDePago); n == 0 || n > 7 {
			v.err("IdDoc.TablaFormasPago", "debe contener entre 1 y 7 formas de pago")
		}
		for i, f := range fp.FormaDePago {
			field := fmt.Sprintf("IdDoc.TablaFormasPago.FormaDePago[%d]", i)
			if f.FormaPago < 1 || f.FormaPago > 8 {
				v.err(field+".FormaPago", "valor %d inválido (1-8)", f.FormaPago)
			}
			if f.FormaPago == 5 && id.TipoeCF != FacturaConsumo {
				v.err(field+".FormaPago", "la forma de pago 5 (bonos o certificados de regalo) solo aplica a e-CF tipo 32")
			}
			v.amount(field+".MontoPago", f.MontoPago, false)
		}
	}
	switch id.TipoCuentaPago {
	case "", "CT", "AH", "OT":
	default:
		v.err("IdDoc.TipoCuentaPago", "valor %q inválido (CT, AH, OT)", id.TipoCuentaPago)
	}
	v.maxLen("IdDoc.NumeroCuentaPago", id.NumeroCuentaPago, 28)
	v.maxLen("IdDoc.BancoPago", id.BancoPago, 75)

	v.presence("IdDoc.FechaDesde", rPeriodo, id.FechaDesde != "")
	v.presence("IdDoc.FechaHasta", rPeriodo, id.FechaHasta != "")
	d1, ok1 := v.date("IdDoc.FechaDesde", id.FechaDesde)
	d2, ok2 := v.date("IdDoc.FechaHasta", id.FechaHasta)
	if ok1 && ok2 && d1.After(d2) {
		v.err("IdDoc.FechaDesde", "debe ser menor o igual a FechaHasta")
	}

	if p := e.Paginacion; p != nil {
		if id.TotalPaginas != len(p.Pagina) {
			v.err("IdDoc.TotalPaginas", "debe coincidir con la cantidad de páginas en Paginacion (%d)", len(p.Pagina))
		}
	} else if id.TotalPaginas != 0 {
		v.err("IdDoc.TotalPaginas", "solo aplica cuando existe la sección Paginacion")
	}
}

func (v *validator) validateEmisor(em *Emisor) {
	v.rnc("Emisor.RNCEmisor", em.RNCEmisor)
	v.required("Emisor.RazonSocialEmisor", em.RazonSocialEmisor, 150)
	v.maxLen("Emisor.NombreComercial", em.NombreComercial, 150)
	v.maxLen("Emisor.Sucursal", em.Sucursal, 20)
	v.required("Emisor.DireccionEmisor", em.DireccionEmisor, 100)
	v.geo("Emisor.Provincia", em.Provincia, "Emisor.Municipio", em.Municipio)
	if t := em.TablaTelefonoEmisor; t != nil {
		if len(t.TelefonoEmisor) > 3 {
			v.err("Emisor.TablaTelefonoEmisor", "admite hasta 3 teléfonos")
		}
		for _, tel := range t.TelefonoEmisor {
			if !reTelefono.MatchString(tel) {
				v.err("Emisor.TablaTelefonoEmisor", "teléfono %q inválido, formato xxx-xxx-xxxx", tel)
			}
		}
	}
	if em.CorreoEmisor != "" {
		v.maxLen("Emisor.CorreoEmisor", em.CorreoEmisor, 80)
		if !reCorreo.MatchString(em.CorreoEmisor) {
			v.err("Emisor.CorreoEmisor", "correo electrónico inválido")
		}
	}
	v.maxLen("Emisor.WebSite", em.WebSite, 50)
	v.maxLen("Emisor.ActividadEconomica", em.ActividadEconomica, 100)
	v.maxLen("Emisor.CodigoVendedor", em.CodigoVendedor, 60)
	v.maxLen("Emisor.InformacionAdicionalEmisor", em.InformacionAdicionalEmisor, 250)
	if em.FechaEmision == "" {
		v.err("Emisor.FechaEmision", "es obligatoria")
	} else if fe, ok := v.date("Emisor.FechaEmision", em.FechaEmision); ok && fe.After(v.opt.Ahora) {
		v.err("Emisor.FechaEmision", "no puede ser posterior a la fecha actual")
	}
}

func (v *validator) validateComprador(e *ECF) {
	c := e.Encabezado.Comprador
	if c == nil {
		c = &Comprador{}
	}
	v.presence("Comprador.RNCComprador", rRNCComprador, c.RNCComprador != "")
	v.presence("Comprador.IdentificadorExtranjero", rIdentificadorExtranjero, c.IdentificadorExtranjero != "")
	v.presence("Comprador.RazonSocialComprador", rRazonSocialComprador, c.RazonSocialComprador != "")
	if c.RNCComprador != "" {
		v.rnc("Comprador.RNCComprador", c.RNCComprador)
	}
	if c.RNCComprador != "" && c.IdentificadorExtranjero != "" {
		v.err("Comprador", "RNCComprador e IdentificadorExtranjero son excluyentes")
	}
	v.maxLen("Comprador.IdentificadorExtranjero", c.IdentificadorExtranjero, 20)
	v.maxLen("Comprador.RazonSocialComprador", c.RazonSocialComprador, 150)
	v.maxLen("Comprador.DireccionComprador", c.DireccionComprador, 100)
	v.geo("Comprador.ProvinciaComprador", c.ProvinciaComprador, "Comprador.MunicipioComprador", c.MunicipioComprador)
	if c.CorreoComprador != "" && !reCorreo.MatchString(c.CorreoComprador) {
		v.err("Comprador.CorreoComprador", "correo electrónico inválido")
	}
	if c.TelefonoAdicional != "" && !reTelefono.MatchString(c.TelefonoAdicional) {
		v.err("Comprador.TelefonoAdicional", "formato xxx-xxx-xxxx")
	}
	v.date("Comprador.FechaEntrega", c.FechaEntrega)
	v.date("Comprador.FechaOrdenCompra", c.FechaOrdenCompra)
	if c.PaisComprador != "" && v.t != Exportaciones {
		v.err("Comprador.PaisComprador", "solo aplica para el tipo 46")
	}

	// Consumer invoices at or above the threshold must identify the buyer.
	if v.t == FacturaConsumo && e.Encabezado.Totales.MontoTotal.Cmp(UmbralConsumo) >= 0 &&
		c.RNCComprador == "" && c.IdentificadorExtranjero == "" {
		v.err("Comprador.RNCComprador", "es obligatorio en facturas de consumo con monto total ≥ RD$250,000.00")
	}
	// Credit and debit notes inherit the buyer of the modified receipt.
	if (v.t == NotaCredito || v.t == NotaDebito) && e.InformacionReferencia != nil &&
		strings.HasPrefix(strings.ToUpper(e.InformacionReferencia.NCFModificado), "E31") && c.RNCComprador == "" {
		v.err("Comprador.RNCComprador", "es obligatorio al modificar una Factura de Crédito Fiscal")
	}
}

// MaxLineas returns the line limit of a document: 1,000 lines, or 10,000
// for consumer invoices below RD$250,000 (Informe Técnico, sección 18).
func MaxLineas(e *ECF) int {
	if e.Encabezado.IdDoc.TipoeCF == FacturaConsumo && e.Encabezado.Totales.MontoTotal.Cmp(UmbralConsumo) < 0 {
		return 10000
	}
	return 1000
}

func (v *validator) validateItems(e *ECF, c *Computed) {
	items := e.DetallesItems.Item
	if len(items) == 0 {
		v.err("DetallesItems", "debe contener al menos un ítem")
		return
	}
	if max := MaxLineas(e); len(items) > max {
		v.err("DetallesItems", "admite hasta %d ítems", max)
	}
	notaCorreccionTexto := v.t == NotaCredito && e.InformacionReferencia != nil && e.InformacionReferencia.CodigoModificacion == 2
	over := tasas(e)
	for i := range items {
		it := &items[i]
		f := fmt.Sprintf("DetallesItems.Item[%d]", i)
		if it.NumeroLinea != i+1 {
			v.err(f+".NumeroLinea", "debe ser secuencial; se esperaba %d", i+1)
		}
		if tc := it.TablaCodigosItem; tc != nil && (len(tc.CodigosItem) == 0 || len(tc.CodigosItem) > 5) {
			v.err(f+".TablaCodigosItem", "debe contener entre 1 y 5 códigos")
		}
		ind := it.IndicadorFacturacion
		if ind < 0 || ind > 4 {
			v.err(f+".IndicadorFacturacion", "valor %d inválido (0-4)", ind)
		}
		switch v.t {
		case GastosMenores, RegimenesEspeciales, PagosExterior:
			if ind != Exento {
				v.err(f+".IndicadorFacturacion", "para el tipo %d debe ser 4 (exento)", v.t)
			}
		case Exportaciones:
			if ind != ITBISTasa3 {
				v.err(f+".IndicadorFacturacion", "para exportaciones debe ser 3 (ITBIS tasa 0%%)")
			}
		}
		v.presence(f+".Retencion", rRetencion, it.Retencion != nil)
		if r := it.Retencion; r != nil {
			if r.IndicadorAgenteRetencionoPercepcion != 1 && r.IndicadorAgenteRetencionoPercepcion != 2 {
				v.err(f+".Retencion.IndicadorAgenteRetencionoPercepcion", "debe ser 1 (retención) o 2 (percepción)")
			}
			v.optAmount(f+".Retencion.MontoITBISRetenido", r.MontoITBISRetenido)
			if v.t == PagosExterior && r.MontoITBISRetenido != nil {
				v.err(f+".Retencion.MontoITBISRetenido", "no corresponde para el tipo 47")
			}
			v.optAmount(f+".Retencion.MontoISRRetenido", r.MontoISRRetenido)
			if ind == Exento && r.IndicadorAgenteRetencionoPercepcion == 1 && r.MontoITBISRetenido == nil && v.t != PagosExterior {
				v.err(f+".Retencion.MontoITBISRetenido", "es obligatorio para ítems exentos con retención")
			}
			if v.t == Compras && it.IndicadorBienoServicio == 2 && r.MontoISRRetenido == nil {
				v.err(f+".Retencion.MontoISRRetenido", "es obligatorio en compras de servicios con retención")
			}
			if v.t == PagosExterior && r.MontoISRRetenido == nil {
				v.err(f+".Retencion.MontoISRRetenido", "es obligatorio en pagos al exterior")
			}
		}
		v.required(f+".NombreItem", it.NombreItem, 80)
		if it.IndicadorBienoServicio != 1 && it.IndicadorBienoServicio != 2 {
			v.err(f+".IndicadorBienoServicio", "debe ser 1 (bien) o 2 (servicio)")
		}
		if v.t == PagosExterior && it.IndicadorBienoServicio != 2 {
			v.err(f+".IndicadorBienoServicio", "en pagos al exterior debe ser 2 (servicio)")
		}
		v.maxLen(f+".DescripcionItem", it.DescripcionItem, 1000)
		if it.CantidadItem.Sign() <= 0 {
			v.err(f+".CantidadItem", "debe ser mayor que cero")
		}
		if it.CantidadItem.Places() > 2 {
			v.err(f+".CantidadItem", "admite un máximo de 2 decimales")
		}
		if it.UnidadMedida != 0 {
			if _, ok := UnidadesMedida[it.UnidadMedida]; !ok {
				v.err(f+".UnidadMedida", "código %d no existe en la Tabla IV", it.UnidadMedida)
			}
		}
		if it.UnidadReferencia != 0 {
			if _, ok := UnidadesMedida[it.UnidadReferencia]; !ok {
				v.err(f+".UnidadReferencia", "código %d no existe en la Tabla IV", it.UnidadReferencia)
			}
		}
		if it.CantidadReferencia != nil && it.UnidadReferencia == 0 {
			v.err(f+".UnidadReferencia", "es obligatoria cuando existe CantidadReferencia")
		}
		if t := it.TablaSubcantidad; t != nil {
			for _, s := range t.SubcantidadItem {
				if _, ok := UnidadesMedida[s.CodigoSubcantidad]; !ok {
					v.err(f+".TablaSubcantidad", "CodigoSubcantidad %d no existe en la Tabla IV", s.CodigoSubcantidad)
				}
				if s.Subcantidad.Sign() < 0 || s.Subcantidad.Places() > 3 {
					v.err(f+".TablaSubcantidad", "Subcantidad admite 3 decimales y no puede ser negativa")
				}
			}
		}
		v.date(f+".FechaElaboracion", it.FechaElaboracion)
		v.date(f+".FechaVencimientoItem", it.FechaVencimientoItem)
		v.presence(f+".Mineria", rMineria, it.Mineria != nil)
		if m := it.Mineria; m != nil {
			if m.TipoAfiliacion != 0 && m.TipoAfiliacion != 1 && m.TipoAfiliacion != 2 {
				v.err(f+".Mineria.TipoAfiliacion", "debe ser 1 (afiliada) o 2 (no afiliada)")
			}
			if m.Liquidacion != 0 && m.Liquidacion != 1 && m.Liquidacion != 2 {
				v.err(f+".Mineria.Liquidacion", "debe ser 1 (provisional) o 2 (final)")
			}
		}
		if it.PrecioUnitarioItem.Sign() < 0 {
			v.err(f+".PrecioUnitarioItem", "no puede ser negativo")
		}
		v.optAmount(f+".DescuentoMonto", it.DescuentoMonto)
		v.optAmount(f+".RecargoMonto", it.RecargoMonto)
		v.validateSubAjustes(f, it)
		v.validateItemImpuestos(f, it, over)
		v.amount(f+".MontoItem", it.MontoItem, false)
		if notaCorreccionTexto {
			if !it.MontoItem.IsZero() {
				v.err(f+".MontoItem", "debe ser cero en una nota de crédito por corrección de texto")
			}
		} else if want, err := LineAmount(it); err == nil {
			v.tolerance(f+".MontoItem", it.MontoItem, want, LineTolerance)
		}
		if om := it.OtraMonedaDetalle; om != nil {
			if e.Encabezado.OtraMoneda == nil {
				v.err(f+".OtraMonedaDetalle", "requiere el área OtraMoneda del encabezado")
			}
			want := mul(om.PrecioOtraMoneda, it.CantidadItem).Round(2).Sub(deref(om.DescuentoOtraMoneda)).Add(deref(om.RecargoOtraMoneda))
			v.amount(f+".OtraMonedaDetalle.MontoItemOtraMoneda", om.MontoItemOtraMoneda, false)
			v.tolerance(f+".OtraMonedaDetalle.MontoItemOtraMoneda", om.MontoItemOtraMoneda, want, LineTolerance)
		}
	}
}

func (v *validator) validateSubAjustes(f string, it *Item) {
	if t := it.TablaSubDescuento; t != nil {
		if len(t.SubDescuento) == 0 || len(t.SubDescuento) > 12 {
			v.err(f+".TablaSubDescuento", "debe contener entre 1 y 12 subdescuentos")
		}
		sum := decimal.Zero
		for j, s := range t.SubDescuento {
			sf := fmt.Sprintf("%s.TablaSubDescuento.SubDescuento[%d]", f, j)
			sum = sum.Add(s.MontoSubDescuento)
			switch s.TipoSubDescuento {
			case "%":
				if s.SubDescuentoPorcentaje == nil {
					v.err(sf+".SubDescuentoPorcentaje", "es obligatorio cuando el tipo es %%")
				} else {
					v.tolerance(sf+".MontoSubDescuento", s.MontoSubDescuento, subAjusteMonto(it, *s.SubDescuentoPorcentaje), LineTolerance)
				}
			case "$":
			default:
				v.err(sf+".TipoSubDescuento", "debe ser \"$\" o \"%%\"")
			}
		}
		if it.DescuentoMonto == nil {
			v.err(f+".DescuentoMonto", "es obligatorio cuando existen subdescuentos")
		} else if !sum.Equal(*it.DescuentoMonto) {
			v.err(f+".DescuentoMonto", "debe ser igual a la suma de los subdescuentos (%s)", sum)
		}
	}
	if t := it.TablaSubRecargo; t != nil {
		if len(t.SubRecargo) == 0 || len(t.SubRecargo) > 12 {
			v.err(f+".TablaSubRecargo", "debe contener entre 1 y 12 subrecargos")
		}
		sum := decimal.Zero
		for j, s := range t.SubRecargo {
			sf := fmt.Sprintf("%s.TablaSubRecargo.SubRecargo[%d]", f, j)
			sum = sum.Add(s.MontoSubRecargo)
			switch s.TipoSubRecargo {
			case "%":
				if s.SubRecargoPorcentaje == nil {
					v.err(sf+".SubRecargoPorcentaje", "es obligatorio cuando el tipo es %%")
				} else {
					v.tolerance(sf+".MontoSubRecargo", s.MontoSubRecargo, subAjusteMonto(it, *s.SubRecargoPorcentaje), LineTolerance)
				}
			case "$":
			default:
				v.err(sf+".TipoSubRecargo", "debe ser \"$\" o \"%%\"")
			}
		}
		if it.RecargoMonto == nil {
			v.err(f+".RecargoMonto", "es obligatorio cuando existen subrecargos")
		} else if !sum.Equal(*it.RecargoMonto) {
			v.err(f+".RecargoMonto", "debe ser igual a la suma de los subrecargos (%s)", sum)
		}
	}
}

// validateItemImpuestos checks the data required by each additional tax
// code (Formato e-CF, sección B, campos 13-18 y 34).
func (v *validator) validateItemImpuestos(f string, it *Item, over map[string]decimal.Decimal) {
	codes := itemCodes(it)
	if len(codes) == 0 {
		return
	}
	v.presence(f+".TablaImpuestoAdicional", rImpuestoAdicional, true)
	if len(codes) > 2 {
		v.err(f+".TablaImpuestoAdicional", "admite hasta 2 impuestos adicionales")
	}
	seen := map[ClaseImpuesto]bool{}
	for _, code := range codes {
		info, ok := TablaImpuestos[code]
		if !ok {
			v.err(f+".TablaImpuestoAdicional", "código %q no existe en la Tabla I", code)
			continue
		}
		if seen[info.Clase] {
			v.err(f+".TablaImpuestoAdicional", "el ítem tiene más de un código de %s", info.Clase)
		}
		seen[info.Clase] = true
		switch info.Clase {
		case ISCEspecificoAlcohol:
			if it.UnidadMedida == UnidadGranel {
				continue
			}
			if it.GradosAlcohol == nil || it.GradosAlcohol.Sign() <= 0 {
				v.err(f+".GradosAlcohol", "es obligatorio para el código %s", code)
			}
			if it.CantidadReferencia == nil {
				v.err(f+".CantidadReferencia", "es obligatoria para el código %s", code)
			}
			if it.TablaSubcantidad == nil {
				v.err(f+".TablaSubcantidad", "es obligatoria para el código %s", code)
			}
			if _, explicit := over[code]; !explicit {
				v.warn("Totales.ImpuestosAdicionales", "no se indicó la tasa del código %s; se usó %s de la Tabla I, que DGII ajusta trimestralmente", code, info.Tasa)
			}
		case ISCEspecificoCigarrillos:
			if it.CantidadReferencia == nil {
				v.err(f+".CantidadReferencia", "es obligatoria para el código %s", code)
			}
			if _, explicit := over[code]; !explicit {
				v.warn("Totales.ImpuestosAdicionales", "no se indicó la tasa del código %s; se usó %s de la Tabla I, que DGII ajusta trimestralmente", code, info.Tasa)
			}
		case ISCAdValoremAlcohol, ISCAdValoremCigarrillos:
			if info.Clase == ISCAdValoremAlcohol && it.UnidadMedida == UnidadGranel {
				continue
			}
			if it.PrecioUnitarioReferencia == nil || it.PrecioUnitarioReferencia.Sign() <= 0 {
				v.err(f+".PrecioUnitarioReferencia", "es obligatorio (PVP) para el código %s", code)
			}
			if it.CantidadReferencia == nil {
				v.err(f+".CantidadReferencia", "es obligatoria para el código %s", code)
			}
		}
	}
	if seen[ISCAdValoremAlcohol] && !seen[ISCEspecificoAlcohol] && it.UnidadMedida != UnidadGranel {
		v.err(f+".TablaImpuestoAdicional", "los códigos 023-035 requieren también el ISC específico (006-018)")
	}
	if seen[ISCAdValoremCigarrillos] && !seen[ISCEspecificoCigarrillos] {
		v.err(f+".TablaImpuestoAdicional", "los códigos 036-039 requieren también el ISC específico (019-022)")
	}
}

func (v *validator) validateDescuentos(e *ECF) {
	dr := e.DescuentosORecargos
	v.presence("DescuentosORecargos", rDescuentosORecargos, dr != nil)
	if dr == nil {
		return
	}
	if n := len(dr.DescuentoORecargo); n == 0 || n > 20 {
		v.err("DescuentosORecargos", "debe contener entre 1 y 20 líneas")
	}
	codeSets := map[string]bool{}
	for i := range e.DetallesItems.Item {
		codeSets[strings.Join(itemCodes(&e.DetallesItems.Item[i]), ",")] = true
	}
	for i, d := range dr.DescuentoORecargo {
		f := fmt.Sprintf("DescuentosORecargos.DescuentoORecargo[%d]", i)
		if d.NumeroLinea != i+1 {
			v.err(f+".NumeroLinea", "debe ser secuencial; se esperaba %d", i+1)
		}
		if d.TipoAjuste != "D" && d.TipoAjuste != "R" {
			v.err(f+".TipoAjuste", "debe ser \"D\" (descuento) o \"R\" (recargo)")
		}
		switch d.TipoValor {
		case "%":
			if d.ValorDescuentooRecargo == nil {
				v.err(f+".ValorDescuentooRecargo", "es obligatorio cuando TipoValor es \"%%\"")
			}
		case "$":
			if len(codeSets) > 1 {
				v.err(f+".TipoValor", "debe ser \"%%\" cuando los ítems tienen distintos códigos de impuesto")
			}
		case "":
			v.err(f+".TipoValor", "es obligatorio (\"$\" o \"%%\")")
		default:
			v.err(f+".TipoValor", "debe ser \"$\" o \"%%\"")
		}
		if d.IndicadorNorma1007 != nil {
			if *d.IndicadorNorma1007 != 1 {
				v.err(f+".IndicadorNorma1007", "el único valor válido es 1")
			}
			if ig := e.Encabezado.IdDoc.IndicadorMontoGravado; ig == nil || *ig != 1 {
				v.err(f+".IndicadorNorma1007", "requiere IndicadorMontoGravado = 1")
			}
		}
		v.maxLen(f+".DescripcionDescuentooRecargo", d.DescripcionDescuentooRecargo, 45)
		v.amount(f+".MontoDescuentooRecargo", d.MontoDescuentooRecargo, false)
		if d.MontoDescuentooRecargoOtraMoneda != nil {
			v.amount(f+".MontoDescuentooRecargoOtraMoneda", *d.MontoDescuentooRecargoOtraMoneda, false)
			if e.Encabezado.OtraMoneda == nil {
				v.err(f+".MontoDescuentooRecargoOtraMoneda", "requiere el área OtraMoneda")
			}
		}
		if ind := d.IndicadorFacturacionDescuentooRecargo; ind == nil {
			v.err(f+".IndicadorFacturacionDescuentooRecargo", "es obligatorio: indica a qué ítems afecta (1-4)")
		} else if *ind < 1 || *ind > 4 {
			v.err(f+".IndicadorFacturacionDescuentooRecargo", "valor inválido (1-4)")
		}
	}
}

func (v *validator) validateTotales(e *ECF, c *Computed) {
	t := &e.Encabezado.Totales
	for _, f := range []struct {
		name string
		d    *decimal.Decimal
	}{
		{"MontoGravadoTotal", t.MontoGravadoTotal}, {"MontoGravadoI1", t.MontoGravadoI1},
		{"MontoGravadoI2", t.MontoGravadoI2}, {"MontoGravadoI3", t.MontoGravadoI3},
		{"MontoExento", t.MontoExento}, {"TotalITBIS", t.TotalITBIS}, {"TotalITBIS1", t.TotalITBIS1},
		{"TotalITBIS2", t.TotalITBIS2}, {"TotalITBIS3", t.TotalITBIS3},
		{"MontoImpuestoAdicional", t.MontoImpuestoAdicional}, {"TotalITBISRetenido", t.TotalITBISRetenido},
		{"TotalISRRetencion", t.TotalISRRetencion}, {"TotalITBISPercepcion", t.TotalITBISPercepcion},
		{"TotalISRPercepcion", t.TotalISRPercepcion}, {"MontoAvancePago", t.MontoAvancePago},
	} {
		v.optAmount("Totales."+f.name, f.d)
	}
	v.amount("Totales.MontoTotal", t.MontoTotal, false)
	for _, f := range []struct {
		name string
		d    *decimal.Decimal
	}{{"MontoNoFacturable", t.MontoNoFacturable}, {"MontoPeriodo", t.MontoPeriodo}, {"SaldoAnterior", t.SaldoAnterior}, {"ValorPagar", t.ValorPagar}} {
		if f.d != nil {
			v.amount("Totales."+f.name, *f.d, true)
		}
	}

	check := func(name string, got *decimal.Decimal, want decimal.Decimal, present bool) {
		switch {
		case !present && got != nil && !got.IsZero():
			v.err("Totales."+name, "no corresponde: no existen ítems para este concepto")
		case present && got == nil:
			v.err("Totales."+name, "es obligatorio; valor calculado %s", want)
		case present:
			v.tolerance("Totales."+name, *got, want, v.tol)
		}
	}
	anyGravado := c.HasI1 || c.HasI2 || c.HasI3
	check("MontoGravadoI1", t.MontoGravadoI1, c.MontoGravadoI1, c.HasI1)
	check("MontoGravadoI2", t.MontoGravadoI2, c.MontoGravadoI2, c.HasI2)
	check("MontoGravadoI3", t.MontoGravadoI3, c.MontoGravadoI3, c.HasI3)
	check("TotalITBIS1", t.TotalITBIS1, c.TotalITBIS1, c.HasI1)
	check("TotalITBIS2", t.TotalITBIS2, c.TotalITBIS2, c.HasI2)
	check("TotalITBIS3", t.TotalITBIS3, c.TotalITBIS3, c.HasI3)
	check("MontoGravadoTotal", t.MontoGravadoTotal, c.MontoGravadoTotal(), anyGravado)
	check("TotalITBIS", t.TotalITBIS, c.TotalITBIS(), anyGravado)
	check("MontoExento", t.MontoExento, c.MontoExento, c.HasExento)
	check("MontoImpuestoAdicional", t.MontoImpuestoAdicional, c.MontoImpuestoAdicional, len(c.Impuestos) > 0)
	if c.HasNoFact && !c.MontoNoFacturable.IsZero() {
		check("MontoNoFacturable", t.MontoNoFacturable, c.MontoNoFacturable, true)
	}
	if c.HasI1 && (t.ITBIS1 == nil || *t.ITBIS1 != 18) {
		v.err("Totales.ITBIS1", "debe ser 18")
	}
	if c.HasI2 && (t.ITBIS2 == nil || *t.ITBIS2 != 16) {
		v.err("Totales.ITBIS2", "debe ser 16")
	}
	if c.HasI3 && (t.ITBIS3 == nil || *t.ITBIS3 != 0) {
		v.err("Totales.ITBIS3", "debe ser 0")
	}
	v.tolerance("Totales.MontoTotal", t.MontoTotal, c.MontoTotal(), v.tol)

	// Additional taxes per code.
	got := map[string]ImpuestoAdicionalTotal{}
	if ia := t.ImpuestosAdicionales; ia != nil {
		if len(ia.ImpuestoAdicional) > 20 {
			v.err("Totales.ImpuestosAdicionales", "admite hasta 20 impuestos adicionales")
		}
		for _, i := range ia.ImpuestoAdicional {
			if _, ok := TablaImpuestos[i.TipoImpuesto]; !ok {
				v.err("Totales.ImpuestosAdicionales", "código %q no existe en la Tabla I", i.TipoImpuesto)
			}
			got[i.TipoImpuesto] = i
		}
	}
	for _, want := range c.Impuestos {
		g, ok := got[want.TipoImpuesto]
		f := "Totales.ImpuestosAdicionales[" + want.TipoImpuesto + "]"
		if !ok {
			v.err(f, "falta el impuesto adicional %s (calculado %s)", want.TipoImpuesto, impuestoAdicionalMonto(want))
			continue
		}
		if g.TasaImpuestoAdicional == nil {
			v.err(f+".TasaImpuestoAdicional", "es obligatoria")
		}
		v.tolerance(f, impuestoAdicionalMonto(g), impuestoAdicionalMonto(want), v.tol)
		delete(got, want.TipoImpuesto)
	}
	for code := range got {
		v.err("Totales.ImpuestosAdicionales["+code+"]", "ningún ítem tiene asignado el código %s", code)
	}

	if c.HasRetencion {
		if rTotalITBISRetenido.For(v.t) != no {
			check("TotalITBISRetenido", t.TotalITBISRetenido, c.TotalITBISRetenido, true)
		} else if t.TotalITBISRetenido != nil {
			v.err("Totales.TotalITBISRetenido", "no corresponde para el tipo %d", v.t)
		}
		if !c.TotalISRRetencion.IsZero() || v.t == PagosExterior {
			check("TotalISRRetencion", t.TotalISRRetencion, c.TotalISRRetencion, true)
		}
	} else {
		check("TotalITBISRetenido", t.TotalITBISRetenido, decimal.Zero, false)
		check("TotalISRRetencion", t.TotalISRRetencion, decimal.Zero, false)
	}
	if c.HasPercepcion {
		v.warn("DetallesItems.Retencion", "el régimen de percepción no está vigente")
	}
	if t.MontoPeriodo != nil {
		v.tolerance("Totales.MontoPeriodo", *t.MontoPeriodo, t.MontoTotal.Add(deref(t.MontoNoFacturable)), v.tol)
	}
	if t.MontoAvancePago != nil && t.MontoAvancePago.Sign() <= 0 {
		v.err("Totales.MontoAvancePago", "debe ser mayor que cero")
	}
	if t.ValorPagar != nil {
		v.tolerance("Totales.ValorPagar", *t.ValorPagar, valorPagar(t, c.DescuentoNorma1007), v.tol)
	}
}

func (v *validator) validateOtraMoneda(e *ECF, c *Computed) {
	om := e.Encabezado.OtraMoneda
	if om == nil {
		return
	}
	if _, ok := Monedas[om.TipoMoneda]; !ok {
		v.err("OtraMoneda.TipoMoneda", "código %q no existe en la Tabla II", om.TipoMoneda)
	}
	if om.TipoCambio.Sign() <= 0 {
		v.err("OtraMoneda.TipoCambio", "debe ser mayor que cero")
		return
	}
	if om.TipoCambio.IntDigits() > 3 {
		v.err("OtraMoneda.TipoCambio", "admite 3 enteros y 4 decimales")
	}
	o, _ := ComputeOtraMoneda(e, c)
	check := func(name string, got *decimal.Decimal, want decimal.Decimal, present bool) {
		switch {
		case !present && got != nil && !got.IsZero():
			v.err("OtraMoneda."+name, "no corresponde")
		case present && got == nil:
			v.err("OtraMoneda."+name, "es obligatorio; valor calculado %s", want)
		case present:
			v.amount("OtraMoneda."+name, *got, false)
			v.tolerance("OtraMoneda."+name, *got, want, v.tol)
		}
	}
	any := o.Has1 || o.Has2 || o.Has3
	check("MontoGravado1OtraMoneda", om.MontoGravado1OtraMoneda, o.Gravado1, o.Has1)
	check("MontoGravado2OtraMoneda", om.MontoGravado2OtraMoneda, o.Gravado2, o.Has2)
	check("MontoGravado3OtraMoneda", om.MontoGravado3OtraMoneda, o.Gravado3, o.Has3)
	check("MontoGravadoTotalOtraMoneda", om.MontoGravadoTotalOtraMoneda, o.GravadoTotal(), any)
	check("TotalITBIS1OtraMoneda", om.TotalITBIS1OtraMoneda, o.ITBIS1, o.Has1)
	check("TotalITBIS2OtraMoneda", om.TotalITBIS2OtraMoneda, o.ITBIS2, o.Has2)
	check("TotalITBIS3OtraMoneda", om.TotalITBIS3OtraMoneda, o.ITBIS3, o.Has3)
	check("TotalITBISOtraMoneda", om.TotalITBISOtraMoneda, o.TotalITBIS(), any)
	check("MontoExentoOtraMoneda", om.MontoExentoOtraMoneda, o.Exento, o.HasExento)
	check("MontoImpuestoAdicionalOtraMoneda", om.MontoImpuestoAdicionalOtraMoneda, o.ImpuestoAdicional, len(o.Impuestos) > 0)
	v.amount("OtraMoneda.MontoTotalOtraMoneda", om.MontoTotalOtraMoneda, false)
	v.tolerance("OtraMoneda.MontoTotalOtraMoneda", om.MontoTotalOtraMoneda, o.MontoTotal(), v.tol)
}

func (v *validator) validateSubtotales(e *ECF) {
	s := e.Subtotales
	if s == nil {
		return
	}
	if n := len(s.Subtotal); n == 0 || n > 20 {
		v.err("Subtotales", "debe contener entre 1 y 20 subtotales")
	}
	for i, st := range s.Subtotal {
		f := fmt.Sprintf("Subtotales.Subtotal[%d]", i)
		if st.NumeroSubTotal != i+1 {
			v.err(f+".NumeroSubTotal", "debe ser secuencial; se esperaba %d", i+1)
		}
		v.maxLen(f+".DescripcionSubtotal", st.DescripcionSubtotal, 40)
		for _, a := range []struct {
			n string
			d *decimal.Decimal
		}{{"SubTotalMontoGravadoTotal", st.SubTotalMontoGravadoTotal}, {"SubTotalMontoGravadoI1", st.SubTotalMontoGravadoI1},
			{"SubTotalMontoGravadoI2", st.SubTotalMontoGravadoI2}, {"SubTotalMontoGravadoI3", st.SubTotalMontoGravadoI3},
			{"SubTotaITBIS", st.SubTotaITBIS}, {"SubTotaITBIS1", st.SubTotaITBIS1}, {"SubTotaITBIS2", st.SubTotaITBIS2},
			{"SubTotaITBIS3", st.SubTotaITBIS3}, {"SubTotalImpuestoAdicional", st.SubTotalImpuestoAdicional},
			{"SubTotalExento", st.SubTotalExento}, {"MontoSubTotal", st.MontoSubTotal}} {
			v.optAmount(f+"."+a.n, a.d)
		}
		if st.SubTotalMontoGravadoTotal != nil {
			want := deref(st.SubTotalMontoGravadoI1).Add(deref(st.SubTotalMontoGravadoI2)).Add(deref(st.SubTotalMontoGravadoI3))
			if !want.IsZero() && !want.Equal(*st.SubTotalMontoGravadoTotal) {
				v.err(f+".SubTotalMontoGravadoTotal", "debe ser la suma de los montos gravados por tasa (%s)", want)
			}
		}
		if st.SubTotaITBIS != nil {
			want := deref(st.SubTotaITBIS1).Add(deref(st.SubTotaITBIS2)).Add(deref(st.SubTotaITBIS3))
			if !want.IsZero() && !want.Equal(*st.SubTotaITBIS) {
				v.err(f+".SubTotaITBIS", "debe ser la suma del ITBIS por tasa (%s)", want)
			}
		}
		if st.MontoSubTotal != nil {
			want := deref(st.SubTotalMontoGravadoTotal).Add(deref(st.SubTotaITBIS)).Add(deref(st.SubTotalImpuestoAdicional)).Add(deref(st.SubTotalExento))
			if !want.Equal(*st.MontoSubTotal) {
				v.err(f+".MontoSubTotal", "debe ser gravado + ITBIS + impuesto adicional + exento (%s)", want)
			}
		}
		if st.Lineas < 0 || st.Lineas > len(e.DetallesItems.Item) {
			v.err(f+".Lineas", "no puede exceder la cantidad de ítems")
		}
	}
}

func (v *validator) validatePaginacion(e *ECF, c *Computed) {
	p := e.Paginacion
	if p == nil {
		return
	}
	n := len(e.DetallesItems.Item)
	if len(p.Pagina) < 2 {
		v.err("Paginacion", "solo aplica cuando la representación impresa tiene más de una página")
	}
	if len(p.Pagina) > MaxPaginas {
		v.err("Paginacion", "admite hasta %d páginas", MaxPaginas)
	}
	next := 1
	sum := decimal.Zero
	for i, pg := range p.Pagina {
		f := fmt.Sprintf("Paginacion.Pagina[%d]", i)
		if pg.PaginaNo != i+1 {
			v.err(f+".PaginaNo", "debe ser secuencial; se esperaba %d", i+1)
		}
		if pg.NoLineaDesde <= 0 || pg.NoLineaHasta < pg.NoLineaDesde {
			v.err(f, "rango de líneas inválido %d-%d", pg.NoLineaDesde, pg.NoLineaHasta)
			continue
		}
		if pg.NoLineaDesde != next {
			v.err(f+".NoLineaDesde", "debe ser %d: las páginas deben cubrir las líneas de forma consecutiva", next)
		}
		next = pg.NoLineaHasta + 1
		want := ComputePagina(e, c, pg.NoLineaDesde, pg.NoLineaHasta, i == len(p.Pagina)-1)
		chk := func(name string, got *decimal.Decimal, w decimal.Decimal, present bool) {
			switch {
			case present && got == nil:
				v.err(f+"."+name, "es obligatorio; valor calculado %s", w)
			case !present && got != nil && !got.IsZero():
				v.err(f+"."+name, "no corresponde para las líneas de la página")
			case present:
				v.tolerance(f+"."+name, *got, w, decimal.FromInt(int64(pg.NoLineaHasta-pg.NoLineaDesde+1)))
			}
		}
		anyG := want.Has1 || want.Has2 || want.Has3
		chk("SubtotalMontoGravado1Pagina", pg.SubtotalMontoGravado1Pagina, want.Gravado1, want.Has1)
		chk("SubtotalMontoGravado2Pagina", pg.SubtotalMontoGravado2Pagina, want.Gravado2, want.Has2)
		chk("SubtotalMontoGravado3Pagina", pg.SubtotalMontoGravado3Pagina, want.Gravado3, want.Has3)
		chk("SubtotalMontoGravadoPagina", pg.SubtotalMontoGravadoPagina, want.GravadoTotal(), anyG)
		chk("SubtotalExentoPagina", pg.SubtotalExentoPagina, want.Exento, want.HasExento)
		chk("SubtotalItbis1Pagina", pg.SubtotalItbis1Pagina, want.ITBIS1, want.Has1)
		chk("SubtotalItbis2Pagina", pg.SubtotalItbis2Pagina, want.ITBIS2, want.Has2)
		chk("SubtotalItbis3Pagina", pg.SubtotalItbis3Pagina, want.ITBIS3, want.Has3)
		chk("SubtotalItbisPagina", pg.SubtotalItbisPagina, want.ITBIS(), anyG)
		chk("SubtotalImpuestoAdicionalPagina", pg.SubtotalImpuestoAdicionalPagina, want.ImpuestoAdicional(), want.ImpuestoAdicional().Sign() > 0)
		chk("MontoSubtotalPagina", pg.MontoSubtotalPagina, want.Monto(), true)
		sum = sum.Add(deref(pg.MontoSubtotalPagina))
	}
	if next-1 != n && len(p.Pagina) > 0 {
		v.err("Paginacion", "las páginas deben cubrir las líneas 1 a %d", n)
	}
	v.tolerance("Paginacion", sum, e.Encabezado.Totales.MontoTotal, v.tol)
}

// paperNCFTipo maps the type code of a paper NCF (B01, B02...) to the
// equivalent e-CF type.
var paperNCFTipo = map[string]TipoECF{
	"01": FacturaCreditoFiscal, "02": FacturaConsumo, "03": NotaDebito, "04": NotaCredito,
	"11": Compras, "13": GastosMenores, "14": RegimenesEspeciales, "15": Gubernamental,
	"16": Exportaciones, "17": PagosExterior,
}

func (v *validator) validateReferencia(e *ECF) {
	r := e.InformacionReferencia
	v.presence("InformacionReferencia", rInformacionReferencia, r != nil)
	if r == nil {
		return
	}
	ncf := strings.ToUpper(r.NCFModificado)
	paper := false
	switch len(ncf) {
	case 13:
		if _, err := ParseENCF(ncf); err != nil {
			v.err("InformacionReferencia.NCFModificado", "%v", err)
		}
	case 11:
		paper = true
		if ncf[0] != 'B' {
			v.err("InformacionReferencia.NCFModificado", "un NCF de 11 posiciones debe ser de la serie B")
		}
	case 19:
		paper = true
		if ncf[0] != 'A' && ncf[0] != 'P' {
			v.err("InformacionReferencia.NCFModificado", "un NCF de 19 posiciones debe ser de la serie A o P")
		}
	case 0:
		v.err("InformacionReferencia.NCFModificado", "es obligatorio")
	default:
		v.err("InformacionReferencia.NCFModificado", "longitud inválida: debe tener 11, 13 o 19 posiciones")
	}
	if r.RNCOtroContribuyente != "" {
		v.rnc("InformacionReferencia.RNCOtroContribuyente", r.RNCOtroContribuyente)
	}
	if r.FechaNCFModificado == "" {
		v.err("InformacionReferencia.FechaNCFModificado", "es obligatoria")
	} else if fm, ok := v.date("InformacionReferencia.FechaNCFModificado", r.FechaNCFModificado); ok {
		if fe, err := ParseDate(e.Encabezado.Emisor.FechaEmision); err == nil && fm.After(fe) {
			v.err("InformacionReferencia.FechaNCFModificado", "no puede ser posterior a la fecha de emisión")
		}
	}
	switch r.CodigoModificacion {
	case 1, 2, 3:
		if v.t != NotaCredito && v.t != NotaDebito {
			v.err("InformacionReferencia.CodigoModificacion", "los códigos 1, 2 y 3 solo aplican a notas de crédito o débito")
		}
	case 4:
		if !paper {
			v.err("InformacionReferencia.NCFModificado", "el reemplazo de contingencia (código 4) debe referenciar un NCF no electrónico")
		} else if len(ncf) == 11 {
			if t, ok := paperNCFTipo[ncf[1:3]]; ok && t != v.t {
				v.err("InformacionReferencia.NCFModificado", "el tipo del NCF reemplazado (B%s) no equivale al tipo de e-CF %d", ncf[1:3], v.t)
			}
		}
	case 5:
		if v.t != FacturaCreditoFiscal {
			v.err("InformacionReferencia.CodigoModificacion", "el código 5 solo aplica a la Factura de Crédito Fiscal")
		}
		if !strings.HasPrefix(ncf[min(1, len(ncf)):], "32") {
			v.err("InformacionReferencia.NCFModificado", "el código 5 debe referenciar una Factura de Consumo Electrónica")
		}
	default:
		v.err("InformacionReferencia.CodigoModificacion", "valor %d inválido (1-5)", r.CodigoModificacion)
	}
	if (v.t == NotaCredito || v.t == NotaDebito) && r.CodigoModificacion == 0 {
		v.err("InformacionReferencia.CodigoModificacion", "es obligatorio en notas de crédito y débito")
	}
	if r.RazonModificacion != "" && v.t != NotaCredito && v.t != NotaDebito {
		v.err("InformacionReferencia.RazonModificacion", "solo aplica a notas de crédito o débito")
	}
	v.maxLen("InformacionReferencia.RazonModificacion", r.RazonModificacion, 90)
	if v.t == NotaCredito && e.Encabezado.IdDoc.IndicadorNotaCredito != nil {
		if want, ok := indicadorNotaCredito(e.Encabezado.Emisor.FechaEmision, r.FechaNCFModificado); ok &&
			want != *e.Encabezado.IdDoc.IndicadorNotaCredito {
			v.err("IdDoc.IndicadorNotaCredito", "debe ser %d según la fecha del comprobante modificado", want)
		}
	}
}

func (v *validator) validateFirma(e *ECF) {
	if e.FechaHoraFirma == "" {
		return
	}
	firma, err := ParseDateTime(e.FechaHoraFirma)
	if err != nil {
		v.err("FechaHoraFirma", "formato esperado dd-MM-AAAA HH:mm:ss")
		return
	}
	if firma.After(v.opt.Ahora.Add(5 * time.Minute)) {
		v.err("FechaHoraFirma", "no puede ser posterior a la fecha y hora actual")
	}
	if emision, err := ParseDate(e.Encabezado.Emisor.FechaEmision); err == nil && emision.After(firma) {
		v.err("Emisor.FechaEmision", "la fecha de emisión es posterior a la fecha de firma")
	}
}

func errRangeType(t TipoECF, s Secuencias) error {
	return fmt.Errorf("el rango %s-%s no corresponde al tipo %d", s.SecuenciaeNCFDesde, s.SecuenciaeNCFHasta, t)
}

func errRangeOrder(s Secuencias) error {
	return fmt.Errorf("SecuenciaeNCFDesde %s es mayor que SecuenciaeNCFHasta %s", s.SecuenciaeNCFDesde, s.SecuenciaeNCFHasta)
}
