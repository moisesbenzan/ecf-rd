package ecf

import (
	"fmt"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
	"github.com/moisesbenzan/ecf-rd/internal/xsd"
)

func schemaOf(doc xsd.Document, v any) Issues {
	b, err := Marshal(v)
	if err != nil {
		return Issues{{Field: "XML", Message: err.Error(), Severity: SeverityError}}
	}
	return xsdIssues(doc, b)
}

// ValidateXML validates raw XML against an official schema. Missing
// signatures are tolerated so documents can be checked before signing.
func ValidateXML(doc xsd.Document, data []byte) Issues { return xsdIssues(doc, data) }

// ValidateRFCE checks a consumer-invoice summary ("Formato de Resumen
// Factura de Consumo Electrónica <DOP250,000").
func ValidateRFCE(r *RFCE) Issues {
	v := &validator{t: FacturaConsumo, opt: DefaultOptions}
	h := &r.Encabezado
	if h.IdDoc.TipoeCF != FacturaConsumo {
		v.err("IdDoc.TipoeCF", "el resumen solo aplica a e-CF tipo 32")
	}
	if n, err := ParseENCF(h.IdDoc.ENCF); err != nil {
		v.err("IdDoc.eNCF", "%v", err)
	} else if n.Tipo != FacturaConsumo {
		v.err("IdDoc.eNCF", "debe ser un e-NCF tipo 32")
	}
	if h.Totales.MontoTotal.Cmp(UmbralConsumo) >= 0 {
		v.err("Totales.MontoTotal", "las facturas de consumo ≥ RD$250,000.00 se envían completas al servicio de recepción de e-CF")
	}
	if h.Comprador.RNCComprador != "" && h.Comprador.IdentificadorExtranjero != "" {
		v.err("Comprador.RNCComprador", "si se completa IdentificadorExtranjero, RNCComprador debe ir en blanco")
	}
	t := &h.Totales
	gravado := deref(t.MontoGravadoI1).Add(deref(t.MontoGravadoI2)).Add(deref(t.MontoGravadoI3))
	itbis := deref(t.TotalITBIS1).Add(deref(t.TotalITBIS2)).Add(deref(t.TotalITBIS3))
	if t.MontoGravadoTotal != nil && !t.MontoGravadoTotal.Equal(gravado) {
		v.err("Totales.MontoGravadoTotal", "debe ser la suma de los montos gravados (%s)", gravado)
	}
	if t.TotalITBIS != nil && !t.TotalITBIS.Equal(itbis) {
		v.err("Totales.TotalITBIS", "debe ser la suma del ITBIS por tasa (%s)", itbis)
	}
	imp := decimal.Zero
	if ia := t.ImpuestosAdicionales; ia != nil {
		for _, i := range ia.ImpuestoAdicional {
			imp = imp.Add(deref(i.MontoImpuestoSelectivoConsumoEspecifico)).Add(deref(i.MontoImpuestoSelectivoConsumoAdvalorem)).Add(deref(i.OtrosImpuestosAdicionales))
		}
	}
	if t.MontoImpuestoAdicional != nil && !t.MontoImpuestoAdicional.Equal(imp) {
		v.err("Totales.MontoImpuestoAdicional", "debe ser la suma de los impuestos adicionales (%s)", imp)
	}
	want := deref(t.MontoGravadoTotal).Add(deref(t.MontoExento)).Add(deref(t.TotalITBIS)).Add(deref(t.MontoImpuestoAdicional))
	if !t.MontoTotal.Equal(want) {
		v.err("Totales.MontoTotal", "debe ser gravado + exento + ITBIS + impuesto adicional (%s)", want)
	}
	if t.MontoPeriodo != nil && !t.MontoPeriodo.Equal(t.MontoTotal.Add(deref(t.MontoNoFacturable))) {
		v.err("Totales.MontoPeriodo", "debe ser MontoTotal + MontoNoFacturable")
	}
	if len(h.CodigoSeguridadeCF) != 6 {
		v.err("CodigoSeguridadeCF", "debe tener 6 caracteres")
	}
	return append(v.issues, schemaOf(xsd.RFCE, r)...)
}

// ValidateARECF checks a receipt acknowledgement.
func ValidateARECF(a *ARECF) Issues {
	v := &validator{opt: DefaultOptions}
	d := &a.DetalleAcusedeRecibo
	if _, err := ParseENCF(d.ENCF); err != nil {
		v.err("eNCF", "%v", err)
	}
	switch d.Estado {
	case AcuseRecibido:
		if d.CodigoMotivoNoRecibido != 0 {
			v.err("CodigoMotivoNoRecibido", "solo aplica cuando Estado es 1 (no recibido)")
		}
	case AcuseNoRecibido:
		if d.CodigoMotivoNoRecibido < 1 || d.CodigoMotivoNoRecibido > 4 {
			v.err("CodigoMotivoNoRecibido", "es obligatorio (1-4) cuando Estado es 1")
		}
	}
	return append(v.issues, schemaOf(xsd.ARECF, a)...)
}

// ValidateACECF checks a commercial approval. When ref is not nil, the
// approval must match the e-CF it refers to (Formato Aprobación Comercial,
// validaciones de los campos 2-6).
func ValidateACECF(a *ACECF, ref *ECF) Issues {
	v := &validator{opt: DefaultOptions}
	d := &a.DetalleAprobacionComercial
	if _, err := ParseENCF(d.ENCF); err != nil {
		v.err("eNCF", "%v", err)
	}
	if d.Estado == AprobacionRechazado && d.DetalleMotivoRechazo == "" {
		v.err("DetalleMotivoRechazo", "es obligatorio cuando el e-CF es rechazado")
	}
	if d.Estado == AprobacionAceptado && d.DetalleMotivoRechazo != "" {
		v.err("DetalleMotivoRechazo", "solo aplica cuando el e-CF es rechazado")
	}
	if ref != nil {
		h := &ref.Encabezado
		if d.RNCEmisor != h.Emisor.RNCEmisor {
			v.err("RNCEmisor", "no coincide con el emisor del e-CF (%s)", h.Emisor.RNCEmisor)
		}
		if d.ENCF != h.IdDoc.ENCF {
			v.err("eNCF", "no coincide con el e-NCF del e-CF (%s)", h.IdDoc.ENCF)
		}
		if d.FechaEmision != h.Emisor.FechaEmision {
			v.err("FechaEmision", "no coincide con la fecha de emisión del e-CF (%s)", h.Emisor.FechaEmision)
		}
		if !d.MontoTotal.Equal(h.Totales.MontoTotal) {
			v.err("MontoTotal", "no coincide con el monto total del e-CF (%s)", h.Totales.MontoTotal)
		}
		if h.Comprador == nil || d.RNCComprador != h.Comprador.RNCComprador {
			v.err("RNCComprador", "no coincide con el comprador del e-CF")
		}
		switch h.IdDoc.TipoeCF {
		case FacturaConsumo, GastosMenores, PagosExterior, Compras:
			v.warn("eNCF", "DGII no requiere aprobación comercial para el tipo %d", h.IdDoc.TipoeCF)
		}
	}
	return append(v.issues, schemaOf(xsd.ACECF, a)...)
}

// ValidateANECF checks a sequence voiding request (Formato Anulación de
// e-NCF): up to 10 lines, ranges of the declared type with the same series,
// Desde ≤ Hasta, and consistent counts.
func ValidateANECF(a *ANECF) Issues {
	v := &validator{opt: DefaultOptions}
	if n := len(a.DetalleAnulacion.Anulacion); n == 0 || n > 10 {
		v.err("DetalleAnulacion", "debe contener entre 1 y 10 anulaciones")
	}
	var total int64
	for i, an := range a.DetalleAnulacion.Anulacion {
		f := fmt.Sprintf("DetalleAnulacion.Anulacion[%d]", i)
		if an.NoLinea != i+1 {
			v.err(f+".NoLinea", "debe ser secuencial; se esperaba %d", i+1)
		}
		if !an.TipoeCF.Valid() {
			v.err(f+".TipoeCF", "tipo inválido %d", an.TipoeCF)
		}
		if n := len(an.TablaRangoSecuenciasAnuladaseNCF.Secuencias); n == 0 || n > 10000 {
			v.err(f+".TablaRangoSecuenciasAnuladaseNCF", "debe contener entre 1 y 10,000 rangos")
		}
		var count int64
		for j, s := range an.TablaRangoSecuenciasAnuladaseNCF.Secuencias {
			sf := fmt.Sprintf("%s.Secuencias[%d]", f, j)
			d, err1 := ParseENCF(s.SecuenciaeNCFDesde)
			h, err2 := ParseENCF(s.SecuenciaeNCFHasta)
			if err1 != nil {
				v.err(sf+".SecuenciaeNCFDesde", "%v", err1)
			}
			if err2 != nil {
				v.err(sf+".SecuenciaeNCFHasta", "%v", err2)
			}
			if err1 != nil || err2 != nil {
				continue
			}
			if d.Tipo != an.TipoeCF || h.Tipo != an.TipoeCF {
				v.err(sf, "el tipo de las secuencias debe ser %d", an.TipoeCF)
			}
			if d.Serie != h.Serie {
				v.err(sf, "Desde y Hasta deben tener la misma serie")
			}
			if d.Secuencia > h.Secuencia {
				v.err(sf, "SecuenciaeNCFDesde es mayor que SecuenciaeNCFHasta")
				continue
			}
			count += h.Secuencia - d.Secuencia + 1
		}
		if an.CantidadeNCFAnulados != count {
			v.err(f+".CantidadeNCFAnulados", "debe ser %d", count)
		}
		total += count
	}
	if a.Encabezado.CantidadeNCFAnulados != total {
		v.err("Encabezado.CantidadeNCFAnulados", "debe ser %d (suma de las anulaciones)", total)
	}
	if _, err := ParseDateTime(a.Encabezado.FechaHoraAnulacioneNCF); err != nil {
		v.err("Encabezado.FechaHoraAnulacioneNCF", "formato esperado dd-MM-AAAA HH:mm:ss")
	}
	return append(v.issues, schemaOf(xsd.ANECF, a)...)
}
