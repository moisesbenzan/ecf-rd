package ecf

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
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
// (e-CF) V1.0". Only fields with type-dependent rules are listed; the rest
// are validated individually below.
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
)

var (
	reTelefono = regexp.MustCompile(`^\d{3}-\d{3}-\d{4}$`)
	reCorreo   = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type validator struct {
	t      TipoECF
	issues Issues
}

func (v *validator) err(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{Field: field, Message: fmt.Sprintf(format, args...), Severity: SeverityError})
}

func (v *validator) warn(field, format string, args ...any) {
	v.issues = append(v.issues, Issue{Field: field, Message: fmt.Sprintf(format, args...), Severity: SeverityWarning})
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

func (v *validator) date(field, s string) {
	if s == "" {
		return
	}
	if _, err := ParseDate(s); err != nil {
		v.err(field, "fecha inválida %q, formato esperado dd-MM-AAAA", s)
	}
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
		v.err(field, "admite un máximo de 2 decimales")
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

// Validate checks e against the structural and business rules of the e-CF
// format. Warnings never block issuance; errors do.
func Validate(e *ECF) Issues {
	h := &e.Encabezado
	v := &validator{t: h.IdDoc.TipoeCF}

	if h.Version != Version {
		v.err("Encabezado.Version", "debe ser %q", Version)
	}
	v.validateIdDoc(e)
	v.validateEmisor(&h.Emisor)
	v.validateComprador(e)
	v.presence("Encabezado.Transporte", rTransporte, h.Transporte != nil)
	v.presence("Encabezado.InformacionesAdicionales", rInfoAdicionales, h.InformacionesAdicionales != nil)
	v.validateItems(e)
	v.validateDescuentos(e)
	v.validateTotales(e)
	v.validateReferencia(e)
	if e.FechaHoraFirma != "" {
		firma, err := ParseDateTime(e.FechaHoraFirma)
		if err != nil {
			v.err("FechaHoraFirma", "formato esperado dd-MM-AAAA HH:mm:ss")
		} else if emision, err := ParseDate(h.Emisor.FechaEmision); err == nil && emision.After(firma) {
			v.warn("Emisor.FechaEmision", "la fecha de emisión es posterior a la fecha de firma")
		}
	}
	return v.issues
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

	v.presence("IdDoc.FechaVencimientoSecuencia", rFechaVencimientoSecuencia, id.FechaVencimientoSecuencia != "")
	v.date("IdDoc.FechaVencimientoSecuencia", id.FechaVencimientoSecuencia)

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
	v.date("IdDoc.FechaLimitePago", id.FechaLimitePago)
	if id.FechaLimitePago != "" {
		fl, e1 := ParseDate(id.FechaLimitePago)
		fe, e2 := ParseDate(e.Encabezado.Emisor.FechaEmision)
		if e1 == nil && e2 == nil && fl.Before(fe) {
			v.err("IdDoc.FechaLimitePago", "debe ser mayor o igual a la fecha de emisión")
		}
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
	v.date("IdDoc.FechaDesde", id.FechaDesde)
	v.date("IdDoc.FechaHasta", id.FechaHasta)
	if id.FechaDesde != "" && id.FechaHasta != "" {
		d, e1 := ParseDate(id.FechaDesde)
		h, e2 := ParseDate(id.FechaHasta)
		if e1 == nil && e2 == nil && d.After(h) {
			v.err("IdDoc.FechaDesde", "debe ser menor o igual a FechaHasta")
		}
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
	} else {
		v.date("Emisor.FechaEmision", em.FechaEmision)
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
}

func (v *validator) validateItems(e *ECF) {
	items := e.DetallesItems.Item
	if len(items) == 0 {
		v.err("DetallesItems", "debe contener al menos un ítem")
		return
	}
	if len(items) > 1000 {
		v.err("DetallesItems", "admite hasta 1000 ítems")
	}
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
			if ind >= ITBISTasa1 && ind <= ITBISTasa3 {
				v.err(f+".IndicadorFacturacion", "el tipo %d no admite ítems gravados con ITBIS", v.t)
			}
		case Exportaciones:
			if ind == ITBISTasa1 || ind == ITBISTasa2 {
				v.err(f+".IndicadorFacturacion", "las exportaciones solo admiten ITBIS tasa 3 (0%%) o exento")
			}
		}
		v.presence(f+".Retencion", rRetencion, it.Retencion != nil)
		if r := it.Retencion; r != nil {
			if r.IndicadorAgenteRetencionoPercepcion != 1 && r.IndicadorAgenteRetencionoPercepcion != 2 {
				v.err(f+".Retencion.IndicadorAgenteRetencionoPercepcion", "debe ser 1 (retención) o 2 (percepción)")
			}
			v.optAmount(f+".Retencion.MontoITBISRetenido", r.MontoITBISRetenido)
			v.optAmount(f+".Retencion.MontoISRRetenido", r.MontoISRRetenido)
		}
		v.required(f+".NombreItem", it.NombreItem, 80)
		if it.IndicadorBienoServicio != 1 && it.IndicadorBienoServicio != 2 {
			v.err(f+".IndicadorBienoServicio", "debe ser 1 (bien) o 2 (servicio)")
		}
		v.maxLen(f+".DescripcionItem", it.DescripcionItem, 1000)
		if it.CantidadItem.Sign() <= 0 {
			v.err(f+".CantidadItem", "debe ser mayor que cero")
		}
		if it.CantidadItem.Places() > 2 {
			v.err(f+".CantidadItem", "admite un máximo de 2 decimales")
		}
		if it.UnidadMedida != 0 && (it.UnidadMedida < 1 || it.UnidadMedida > 99) {
			v.err(f+".UnidadMedida", "código inválido")
		}
		v.date(f+".FechaElaboracion", it.FechaElaboracion)
		v.date(f+".FechaVencimientoItem", it.FechaVencimientoItem)
		if it.PrecioUnitarioItem.Sign() < 0 {
			v.err(f+".PrecioUnitarioItem", "no puede ser negativo")
		}
		v.optAmount(f+".DescuentoMonto", it.DescuentoMonto)
		v.optAmount(f+".RecargoMonto", it.RecargoMonto)
		if t := it.TablaSubDescuento; t != nil {
			if it.DescuentoMonto == nil {
				v.err(f+".TablaSubDescuento", "requiere DescuentoMonto")
			} else {
				sum := decimal.Zero
				for _, s := range t.SubDescuento {
					sum = sum.Add(s.MontoSubDescuento)
					if s.TipoSubDescuento != "$" && s.TipoSubDescuento != "%" {
						v.err(f+".TablaSubDescuento", "TipoSubDescuento debe ser \"$\" o \"%%\"")
					}
				}
				if !sum.Equal(*it.DescuentoMonto) {
					v.err(f+".DescuentoMonto", "debe ser igual a la suma de los subdescuentos (%s)", sum)
				}
			}
		}
		if t := it.TablaSubRecargo; t != nil {
			if it.RecargoMonto == nil {
				v.err(f+".TablaSubRecargo", "requiere RecargoMonto")
			} else {
				sum := decimal.Zero
				for _, s := range t.SubRecargo {
					sum = sum.Add(s.MontoSubRecargo)
				}
				if !sum.Equal(*it.RecargoMonto) {
					v.err(f+".RecargoMonto", "debe ser igual a la suma de los subrecargos (%s)", sum)
				}
			}
		}
		if t := it.TablaImpuestoAdicional; t != nil && len(t.ImpuestoAdicional) > 2 {
			v.err(f+".TablaImpuestoAdicional", "admite hasta 2 impuestos adicionales")
		}
		v.amount(f+".MontoItem", it.MontoItem, false)
		if want, err := LineAmount(it); err == nil && !want.Equal(it.MontoItem) {
			v.err(f+".MontoItem", "debe ser (Cantidad × Precio) − Descuento + Recargo = %s", want)
		}
	}
}

func (v *validator) validateDescuentos(e *ECF) {
	dr := e.DescuentosORecargos
	if dr == nil {
		return
	}
	if n := len(dr.DescuentoORecargo); n == 0 || n > 20 {
		v.err("DescuentosORecargos", "debe contener entre 1 y 20 líneas")
	}
	for i, d := range dr.DescuentoORecargo {
		f := fmt.Sprintf("DescuentosORecargos.DescuentoORecargo[%d]", i)
		if d.NumeroLinea != i+1 {
			v.err(f+".NumeroLinea", "debe ser secuencial; se esperaba %d", i+1)
		}
		if d.TipoAjuste != "D" && d.TipoAjuste != "R" {
			v.err(f+".TipoAjuste", "debe ser \"D\" (descuento) o \"R\" (recargo)")
		}
		if d.TipoValor != "" && d.TipoValor != "$" && d.TipoValor != "%" {
			v.err(f+".TipoValor", "debe ser \"$\" o \"%%\"")
		}
		if d.TipoValor == "%" && d.ValorDescuentooRecargo == nil {
			v.err(f+".ValorDescuentooRecargo", "es obligatorio cuando TipoValor es \"%%\"")
		}
		v.maxLen(f+".DescripcionDescuentooRecargo", d.DescripcionDescuentooRecargo, 45)
		v.amount(f+".MontoDescuentooRecargo", d.MontoDescuentooRecargo, false)
		if ind := d.IndicadorFacturacionDescuentooRecargo; ind != nil && (*ind < 1 || *ind > 4) {
			v.err(f+".IndicadorFacturacionDescuentooRecargo", "valor inválido (1-4)")
		}
	}
}

func (v *validator) validateTotales(e *ECF) {
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
		{"TotalISRPercepcion", t.TotalISRPercepcion},
	} {
		v.optAmount("Totales."+f.name, f.d)
	}
	v.amount("Totales.MontoTotal", t.MontoTotal, false)
	if t.MontoNoFacturable != nil {
		v.amount("Totales.MontoNoFacturable", *t.MontoNoFacturable, true)
	}
	if ia := t.ImpuestosAdicionales; ia != nil && len(ia.ImpuestoAdicional) > 20 {
		v.err("Totales.ImpuestosAdicionales", "admite hasta 20 impuestos adicionales")
	}

	c, err := Compute(e)
	if err != nil {
		v.err("Totales", "no fue posible calcular los totales: %v", err)
		return
	}
	check := func(name string, got *decimal.Decimal, want decimal.Decimal, present bool) {
		switch {
		case !present && got != nil && !got.IsZero():
			v.err("Totales."+name, "no corresponde: no existen ítems para este concepto")
		case present && got == nil:
			v.err("Totales."+name, "es obligatorio; valor calculado %s", want)
		case present && !got.Equal(want):
			v.err("Totales."+name, "valor %s no coincide con el calculado %s", got, want)
		}
	}
	anyGravado := c.HasI1 || c.HasI2 || c.HasI3
	check("MontoGravadoI1", t.MontoGravadoI1, c.MontoGravadoI1, c.HasI1)
	check("MontoGravadoI2", t.MontoGravadoI2, c.MontoGravadoI2, c.HasI2)
	check("MontoGravadoI3", t.MontoGravadoI3, c.MontoGravadoI3, c.HasI3)
	check("TotalITBIS1", t.TotalITBIS1, c.TotalITBIS1, c.HasI1)
	check("TotalITBIS2", t.TotalITBIS2, c.TotalITBIS2, c.HasI2)
	check("MontoGravadoTotal", t.MontoGravadoTotal, c.MontoGravadoTotal(), anyGravado)
	check("TotalITBIS", t.TotalITBIS, c.TotalITBIS(), anyGravado)
	check("MontoExento", t.MontoExento, c.MontoExento, c.HasExento)
	if c.HasI1 && (t.ITBIS1 == nil || *t.ITBIS1 != 18) {
		v.err("Totales.ITBIS1", "debe ser 18")
	}
	if c.HasI2 && (t.ITBIS2 == nil || *t.ITBIS2 != 16) {
		v.err("Totales.ITBIS2", "debe ser 16")
	}
	if c.HasI3 && (t.ITBIS3 == nil || *t.ITBIS3 != 0) {
		v.err("Totales.ITBIS3", "debe ser 0")
	}
	if want := expectedMontoTotal(&c, t); !t.MontoTotal.Equal(want) {
		v.err("Totales.MontoTotal", "valor %s no coincide con el calculado %s", t.MontoTotal, want)
	}
	if c.HasRetencion {
		check("TotalITBISRetenido", t.TotalITBISRetenido, c.TotalITBISRetenido, true)
		check("TotalISRRetencion", t.TotalISRRetencion, c.TotalISRRetencion, true)
	}
}

func (v *validator) validateReferencia(e *ECF) {
	r := e.InformacionReferencia
	v.presence("InformacionReferencia", rInformacionReferencia, r != nil)
	if r == nil {
		return
	}
	if r.NCFModificado == "" {
		v.err("InformacionReferencia.NCFModificado", "es obligatorio")
	} else if len(r.NCFModificado) != 13 && len(r.NCFModificado) != 11 && len(r.NCFModificado) != 19 {
		v.err("InformacionReferencia.NCFModificado", "longitud inválida")
	}
	if r.RNCOtroContribuyente != "" {
		v.rnc("InformacionReferencia.RNCOtroContribuyente", r.RNCOtroContribuyente)
	}
	if r.FechaNCFModificado == "" {
		v.err("InformacionReferencia.FechaNCFModificado", "es obligatoria")
	}
	v.date("InformacionReferencia.FechaNCFModificado", r.FechaNCFModificado)
	if r.CodigoModificacion < 1 || r.CodigoModificacion > 5 {
		v.err("InformacionReferencia.CodigoModificacion", "valor %d inválido (1-5)", r.CodigoModificacion)
	}
	if r.CodigoModificacion >= 1 && r.CodigoModificacion <= 3 && v.t != NotaCredito && v.t != NotaDebito {
		v.err("InformacionReferencia.CodigoModificacion", "los códigos 1, 2 y 3 solo aplican a notas de crédito o débito")
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

func errRangeType(t TipoECF, s Secuencias) error {
	return fmt.Errorf("el rango %s-%s no corresponde al tipo %d", s.SecuenciaeNCFDesde, s.SecuenciaeNCFHasta, t)
}

func errRangeOrder(s Secuencias) error {
	return fmt.Errorf("SecuenciaeNCFDesde %s es mayor que SecuenciaeNCFHasta %s", s.SecuenciaeNCFDesde, s.SecuenciaeNCFHasta)
}
