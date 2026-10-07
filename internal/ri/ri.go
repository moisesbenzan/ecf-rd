// Package ri renders the printed representation ("Representación Impresa",
// RI) of an e-CF as PDF, following section 18 of the Informe Técnico e-CF:
//
//   - top right: e-CF type in words, e-NCF, sequence expiry date and, for
//     credit/debit notes, the modified NCF and modification code in words;
//   - top left: issuer (trade name, legal name, branch, RNC, address,
//     municipality, province, issue date); below it the buyer;
//   - centre: quantity, "E" for exempt lines, description, unit of measure in
//     words, alcohol degrees and PVP (if any), price, specific and ad
//     valorem ISC, ITBIS, discount, surcharge and line value;
//   - totals: subtotal gravado, subtotal exento, total ISC, total ITBIS,
//     CDT, propina legal and other additional taxes, total; global
//     discounts/surcharges; foreign currency totals;
//   - bottom left: QR code (≥ 22 × 22 mm, ≥ 2 cm from the page edge, 3 mm
//     quiet zone), security code and digital signature date, repeated on
//     every page; page subtotals when the e-CF carries a Paginacion section.
package ri

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-pdf/fpdf"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
)

// Opciones configure the rendering.
type Opciones struct {
	// CodigoSeguridad: first six characters of the SignatureValue.
	CodigoSeguridad string
	// URLTimbre is the URL encoded in the QR code (ConsultaTimbre or
	// ConsultaTimbreFC for consumer invoices below RD$250,000).
	URLTimbre string
	// Contingencia prints "e-CF emitido en modalidad de contingencia"
	// (Informe Técnico, sección 19).
	Contingencia bool
	// LineasPorPagina is used when the e-CF has no Paginacion section
	// (default 22).
	LineasPorPagina int
}

// QRVersion is the QR code version mandated by DGII for the RI.
const QRVersion = 8

// QR encodes content as a PNG QR code. Version 8 is used whenever the
// content fits (DGII mandates version 8). A version 8 symbol holds at most
// 192 bytes, while a complete ConsultaTimbre URL is usually ~200 bytes, so
// longer URLs fall back to the smallest version that holds them (reported by
// the second return value); any QR reader decodes them.
func QR(content string) ([]byte, int, error) {
	q, err := qrcode.NewWithForcedVersion(content, QRVersion, qrcode.Medium)
	if err != nil {
		q, err = qrcode.NewWithForcedVersion(content, QRVersion, qrcode.Low)
	}
	if err != nil {
		q, err = qrcode.New(content, qrcode.Low)
		if err != nil {
			return nil, 0, err
		}
	}
	q.DisableBorder = true
	png, err := q.PNG(512)
	if err != nil {
		return nil, 0, err
	}
	return png, q.VersionNumber, nil
}

var modificacion = map[int]string{
	1: "Anula el NCF modificado",
	2: "Corrige Texto del Comprobante Fiscal modificado",
	3: "Corrige montos del NCF modificado",
	4: "Reemplazo NCF emitido en contingencia",
	5: "Referencia Factura Consumo Electrónica",
}

type column struct {
	title string
	width float64
	align string
	value func(i int) string
}

type renderer struct {
	pdf   *fpdf.Fpdf
	tr    func(string) string
	e     *ecf.ECF
	c     ecf.Computed
	opt   Opciones
	qr    string
	cols  []column
	pages [][2]int // line ranges per page (1-based, inclusive)
}

const (
	pageW, pageH = 215.9, 279.4 // Letter
	margin       = 12.0
	qrSize       = 24.0 // ≥ 22 mm
	qrEdge       = 20.0 // ≥ 2 cm from the edge
	footerH      = qrEdge + qrSize + 8
)

// Render produces the PDF of e.
func Render(e *ecf.ECF, opt Opciones) ([]byte, error) {
	c, err := ecf.Compute(e)
	if err != nil {
		return nil, err
	}
	if opt.LineasPorPagina <= 0 {
		opt.LineasPorPagina = 22
	}
	pdf := fpdf.New("P", "mm", "Letter", "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(false, 0)
	pdf.SetTitle(e.Encabezado.IdDoc.ENCF, true)
	pdf.SetCreator("ecf-rd", true)
	r := &renderer{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor(""), e: e, c: c, opt: opt}

	if opt.URLTimbre != "" {
		png, _, err := QR(opt.URLTimbre)
		if err != nil {
			return nil, fmt.Errorf("ri: generando QR: %w", err)
		}
		r.qr = "timbre"
		pdf.RegisterImageOptionsReader(r.qr, fpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(png))
	}
	r.columns()
	r.paginate()
	for i, pg := range r.pages {
		r.page(i, pg[0], pg[1], i == len(r.pages)-1)
	}
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *renderer) paginate() {
	n := len(r.e.DetallesItems.Item)
	if p := r.e.Paginacion; p != nil && len(p.Pagina) > 1 {
		for _, pg := range p.Pagina {
			r.pages = append(r.pages, [2]int{pg.NoLineaDesde, pg.NoLineaHasta})
		}
		return
	}
	per := r.opt.LineasPorPagina
	for d := 1; d <= n; d += per {
		h := d + per - 1
		if h > n {
			h = n
		}
		r.pages = append(r.pages, [2]int{d, h})
	}
	if len(r.pages) == 0 {
		r.pages = [][2]int{{1, 0}}
	}
}

func money(d decimal.Decimal) string {
	s := d.StringFixed(2)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	ip, fp, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, ch := range ip {
		if i > 0 && (len(ip)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := b.String() + "." + fp
	if neg {
		out = "-" + out
	}
	return out
}

func qty(d decimal.Decimal) string {
	s := d.String()
	if strings.HasSuffix(s, ".00") {
		return strings.TrimSuffix(s, ".00")
	}
	return s
}

func optMoney(d *decimal.Decimal) string {
	if d == nil || d.IsZero() {
		return ""
	}
	return money(*d)
}

// columns chooses the detail columns according to the data present.
func (r *renderer) columns() {
	items := r.e.DetallesItems.Item
	var hasUnit, hasAlcohol, hasPVP, hasEsp, hasAdv, hasITBIS, hasDesc, hasRec bool
	for i := range items {
		it := &items[i]
		hasUnit = hasUnit || it.UnidadMedida != 0
		hasAlcohol = hasAlcohol || it.GradosAlcohol != nil
		hasPVP = hasPVP || it.PrecioUnitarioReferencia != nil
		hasEsp = hasEsp || r.c.ISCEspecificoItem[i].Sign() > 0
		hasAdv = hasAdv || r.c.ISCAdValoremItem[i].Sign() > 0
		hasITBIS = hasITBIS || it.IndicadorFacturacion == ecf.ITBISTasa1 || it.IndicadorFacturacion == ecf.ITBISTasa2
		hasDesc = hasDesc || (it.DescuentoMonto != nil && !it.DescuentoMonto.IsZero())
		hasRec = hasRec || (it.RecargoMonto != nil && !it.RecargoMonto.IsZero())
	}
	it := func(i int) *ecf.Item { return &items[i] }
	cols := []column{
		{"Cant.", 13, "R", func(i int) string { return qty(it(i).CantidadItem) }},
		{"Descripción", 0, "L", func(i int) string {
			name := it(i).NombreItem
			if it(i).IndicadorFacturacion == ecf.Exento {
				name = "E  " + name
			}
			return name
		}},
	}
	if hasUnit {
		cols = append(cols, column{"Unidad", 20, "L", func(i int) string {
			if u, ok := ecf.UnidadesMedida[it(i).UnidadMedida]; ok {
				return u.Nombre
			}
			return ""
		}})
	}
	if hasAlcohol {
		cols = append(cols, column{"Grados %", 14, "R", func(i int) string {
			if g := it(i).GradosAlcohol; g != nil {
				return g.String()
			}
			return ""
		}})
	}
	if hasPVP {
		cols = append(cols, column{"PVP", 17, "R", func(i int) string { return optMoney(it(i).PrecioUnitarioReferencia) }})
	}
	cols = append(cols, column{"Precio", 20, "R", func(i int) string {
		p := it(i).PrecioUnitarioItem
		if p.Places() > 2 {
			return p.String()
		}
		return money(p)
	}})
	if hasEsp {
		cols = append(cols, column{"ISC Específico", 18, "R", func(i int) string { return money(r.c.ISCEspecificoItem[i].Round(2)) }})
	}
	if hasAdv {
		cols = append(cols, column{"ISC Ad-Valorem", 18, "R", func(i int) string { return money(r.c.ISCAdValoremItem[i].Round(2)) }})
	}
	if hasITBIS {
		cols = append(cols, column{"ITBIS", 18, "R", func(i int) string {
			v := ecf.ItemITBIS(r.e, &r.c, i)
			if v.IsZero() {
				return ""
			}
			return money(v)
		}})
	}
	if hasDesc {
		cols = append(cols, column{"Descuento", 18, "R", func(i int) string { return optMoney(it(i).DescuentoMonto) }})
	}
	if hasRec {
		cols = append(cols, column{"Recargo", 17, "R", func(i int) string { return optMoney(it(i).RecargoMonto) }})
	}
	cols = append(cols, column{"Valor", 22, "R", func(i int) string { return money(it(i).MontoItem) }})
	used := 0.0
	for _, c := range cols {
		used += c.width
	}
	cols[1].width = pageW - 2*margin - used
	if cols[1].width < 35 {
		cols[1].width = 35
	}
	r.cols = cols
}

func (r *renderer) text(x, y, w float64, style string, size float64, align, s string) {
	r.pdf.SetFont("Helvetica", style, size)
	r.pdf.SetXY(x, y)
	r.pdf.CellFormat(w, size*0.45, r.tr(s), "", 0, align, false, 0, "")
}

func (r *renderer) header() float64 {
	h := &r.e.Encabezado
	em := &h.Emisor
	pdf := r.pdf
	type line struct {
		s, style string
		size     float64
	}
	var left []line
	add := func(s, style string, size float64) {
		if s != "" {
			left = append(left, line{s, style, size})
		}
	}
	add(em.NombreComercial, "B", 13)
	add(em.RazonSocialEmisor, "B", 10)
	add(em.Sucursal, "", 8.5)
	add("RNC: "+em.RNCEmisor, "", 8.5)
	add(em.DireccionEmisor, "", 8.5)
	if m, ok := ecf.ProvinciasMunicipios[em.Municipio]; ok {
		add(m, "", 8.5)
	}
	if p, ok := ecf.ProvinciasMunicipios[em.Provincia]; ok {
		add(p, "", 8.5)
	}
	add("Fecha Emisión: "+em.FechaEmision, "", 8.5)
	y := margin
	for _, l := range left {
		r.text(margin, y, 110, l.style, l.size, "L", l.s)
		y += l.size * 0.48
	}

	// Right block.
	rx, ry, rw := pageW-margin-80, margin, 80.0
	r.text(rx, ry, rw, "B", 11, "R", h.IdDoc.TipoeCF.Name())
	ry += 6
	r.text(rx, ry, rw, "B", 10, "R", "e-NCF: "+h.IdDoc.ENCF)
	ry += 5
	if h.IdDoc.FechaVencimientoSecuencia != "" {
		r.text(rx, ry, rw, "", 8.5, "R", "Fecha Vencimiento: "+h.IdDoc.FechaVencimientoSecuencia)
		ry += 4.5
	}
	if ref := r.e.InformacionReferencia; ref != nil {
		r.text(rx, ry, rw, "", 8.5, "R", "NCF Modificado: "+ref.NCFModificado)
		ry += 4.5
		if m, ok := modificacion[ref.CodigoModificacion]; ok {
			pdf.SetFont("Helvetica", "", 8)
			pdf.SetXY(rx, ry)
			pdf.MultiCell(rw, 3.8, r.tr("Código de Modificación: "+m), "", "R", false)
			ry = pdf.GetY()
		}
		if ref.RazonModificacion != "" {
			pdf.SetFont("Helvetica", "", 8)
			pdf.SetXY(rx, ry)
			pdf.MultiCell(rw, 3.8, r.tr("Razón: "+ref.RazonModificacion), "", "R", false)
			ry = pdf.GetY()
		}
	}
	if r.opt.Contingencia {
		r.text(rx, ry+1, rw, "B", 8.5, "R", "e-CF emitido en modalidad de contingencia")
		ry += 5.5
	}
	if y < ry {
		y = ry
	}

	// Buyer.
	y += 3
	if c := h.Comprador; c != nil && (c.RazonSocialComprador != "" || c.RNCComprador != "" || c.IdentificadorExtranjero != "") {
		pdf.SetDrawColor(180, 180, 180)
		pdf.Line(margin, y, pageW-margin, y)
		y += 2
		if c.RazonSocialComprador != "" {
			r.text(margin, y, 150, "", 9, "L", "Razón Social Cliente: "+c.RazonSocialComprador)
			y += 4.5
		}
		if c.RNCComprador != "" {
			r.text(margin, y, 150, "", 9, "L", "RNC Cliente: "+c.RNCComprador)
			y += 4.5
		} else if c.IdentificadorExtranjero != "" {
			r.text(margin, y, 150, "", 9, "L", "Identificación Cliente: "+c.IdentificadorExtranjero)
			y += 4.5
		}
	}
	return y + 2
}

func (r *renderer) tableHeader(y float64) float64 {
	pdf := r.pdf
	pdf.SetFillColor(235, 235, 235)
	pdf.SetFont("Helvetica", "B", 7.5)
	x := margin
	for _, c := range r.cols {
		pdf.SetXY(x, y)
		pdf.CellFormat(c.width, 6, r.tr(c.title), "TB", 0, c.align, true, 0, "")
		x += c.width
	}
	return y + 6
}

func (r *renderer) row(y float64, i int) float64 {
	pdf := r.pdf
	pdf.SetFont("Helvetica", "", 7.5)
	// Description may wrap; compute its height first.
	desc := r.tr(r.cols[1].value(i))
	lines := pdf.SplitLines([]byte(desc), r.cols[1].width-1)
	h := float64(len(lines)) * 3.6
	if h < 4.5 {
		h = 4.5
	}
	x := margin
	for k, c := range r.cols {
		pdf.SetXY(x, y)
		if k == 1 {
			pdf.MultiCell(c.width, 3.6, desc, "", "L", false)
		} else {
			pdf.CellFormat(c.width, 4.5, r.tr(c.value(i)), "", 0, c.align, false, 0, "")
		}
		x += c.width
	}
	return y + h
}

func (r *renderer) totalLine(y float64, label, value string, bold bool) float64 {
	style := ""
	if bold {
		style = "B"
	}
	r.text(pageW-margin-95, y, 60, style, 9, "R", label)
	r.text(pageW-margin-35, y, 35, style, 9, "R", value)
	return y + 4.8
}

func (r *renderer) totals(y float64) float64 {
	t := &r.e.Encabezado.Totales
	r.pdf.SetDrawColor(180, 180, 180)
	r.pdf.Line(margin, y, pageW-margin, y)
	y += 2
	if t.MontoGravadoTotal != nil {
		y = r.totalLine(y, "Subtotal Gravado", money(*t.MontoGravadoTotal), false)
	}
	if t.MontoExento != nil {
		y = r.totalLine(y, "Subtotal Exento", money(*t.MontoExento), false)
	}
	isc := decimal.Zero
	var otros []ecf.ImpuestoAdicionalTotal
	if ia := t.ImpuestosAdicionales; ia != nil {
		for _, i := range ia.ImpuestoAdicional {
			if info, ok := ecf.TablaImpuestos[i.TipoImpuesto]; ok && info.EsISC() {
				isc = isc.Add(deref(i.MontoImpuestoSelectivoConsumoEspecifico)).Add(deref(i.MontoImpuestoSelectivoConsumoAdvalorem))
			} else {
				otros = append(otros, i)
			}
		}
	}
	if isc.Sign() > 0 {
		y = r.totalLine(y, "Total ISC", money(isc), false)
	}
	if t.TotalITBIS != nil {
		label := "Total ITBIS"
		if t.ITBIS1 != nil && t.ITBIS2 == nil {
			label = "Total ITBIS (18%)"
		}
		y = r.totalLine(y, label, money(*t.TotalITBIS), false)
	}
	for _, o := range otros {
		label := ecf.TablaImpuestos[o.TipoImpuesto].Nombre
		if o.TasaImpuestoAdicional != nil {
			label += " (" + qty(*o.TasaImpuestoAdicional) + "%)"
		}
		y = r.totalLine(y, label, money(deref(o.OtrosImpuestosAdicionales)), false)
	}
	y = r.totalLine(y, "Total", money(t.MontoTotal), true)
	if t.MontoNoFacturable != nil {
		y = r.totalLine(y, "Monto No Facturable", money(*t.MontoNoFacturable), false)
	}
	if t.TotalITBISRetenido != nil && !t.TotalITBISRetenido.IsZero() {
		y = r.totalLine(y, "ITBIS Retenido", money(*t.TotalITBISRetenido), false)
	}
	if t.TotalISRRetencion != nil && !t.TotalISRRetencion.IsZero() {
		y = r.totalLine(y, "Retención ISR", money(*t.TotalISRRetencion), false)
	}
	if t.MontoAvancePago != nil {
		y = r.totalLine(y, "Avance de Pago", money(*t.MontoAvancePago), false)
	}
	if t.SaldoAnterior != nil {
		y = r.totalLine(y, "Saldo Anterior", money(*t.SaldoAnterior), false)
	}
	if t.ValorPagar != nil {
		y = r.totalLine(y, "Valor a Pagar", money(*t.ValorPagar), true)
	}

	// Global discounts and surcharges.
	if dr := r.e.DescuentosORecargos; dr != nil {
		y += 2
		for _, d := range dr.DescuentoORecargo {
			kind := "Descuento"
			if d.TipoAjuste == "R" {
				kind = "Recargo"
			}
			label := kind
			if d.DescripcionDescuentooRecargo != "" {
				label += ": " + d.DescripcionDescuentooRecargo
			}
			if d.TipoValor == "%" && d.ValorDescuentooRecargo != nil {
				label += " (" + qty(*d.ValorDescuentooRecargo) + "%)"
			}
			y = r.totalLine(y, label, money(d.MontoDescuentooRecargo), false)
		}
	}

	// Foreign currency.
	if om := r.e.Encabezado.OtraMoneda; om != nil {
		y += 2
		cur := om.TipoMoneda
		y = r.totalLine(y, "Tipo de Cambio "+cur, om.TipoCambio.String(), false)
		if om.MontoGravadoTotalOtraMoneda != nil {
			y = r.totalLine(y, "Subtotal Gravado "+cur, money(*om.MontoGravadoTotalOtraMoneda), false)
		}
		if om.MontoExentoOtraMoneda != nil {
			y = r.totalLine(y, "Subtotal Exento "+cur, money(*om.MontoExentoOtraMoneda), false)
		}
		if om.TotalITBISOtraMoneda != nil {
			y = r.totalLine(y, "Total ITBIS "+cur, money(*om.TotalITBISOtraMoneda), false)
		}
		if om.MontoImpuestoAdicionalOtraMoneda != nil {
			y = r.totalLine(y, "Impuestos Adicionales "+cur, money(*om.MontoImpuestoAdicionalOtraMoneda), false)
		}
		y = r.totalLine(y, "Total "+cur, money(om.MontoTotalOtraMoneda), true)
	}
	return y
}

func (r *renderer) pageSubtotals(y float64, idx int) float64 {
	p := r.e.Paginacion
	if p == nil || idx >= len(p.Pagina) {
		return y
	}
	pg := p.Pagina[idx]
	r.pdf.SetDrawColor(180, 180, 180)
	r.pdf.Line(margin, y, pageW-margin, y)
	y += 2
	if pg.SubtotalMontoGravadoPagina != nil {
		y = r.totalLine(y, "Subtotal Gravado Página", money(*pg.SubtotalMontoGravadoPagina), false)
	}
	if pg.SubtotalExentoPagina != nil {
		y = r.totalLine(y, "Subtotal Exento Página", money(*pg.SubtotalExentoPagina), false)
	}
	if s := pg.SubtotalImpuestoAdicional; s != nil {
		if s.SubtotalImpuestoSelectivoConsumoEspecificoPagina != nil {
			y = r.totalLine(y, "Subtotal ISC Página", money(*s.SubtotalImpuestoSelectivoConsumoEspecificoPagina), false)
		}
		if s.SubtotalOtrosImpuesto != nil {
			y = r.totalLine(y, "Subtotal Otros Impuestos Página", money(*s.SubtotalOtrosImpuesto), false)
		}
	}
	if pg.SubtotalItbisPagina != nil {
		y = r.totalLine(y, "Subtotal ITBIS Página", money(*pg.SubtotalItbisPagina), false)
	}
	if pg.MontoSubtotalPagina != nil {
		y = r.totalLine(y, "Monto Total Página", money(*pg.MontoSubtotalPagina), true)
	}
	return y
}

func (r *renderer) footer(pageNo int) {
	pdf := r.pdf
	y := pageH - qrEdge - qrSize
	if r.qr != "" {
		// 3 mm quiet zone around the QR.
		pdf.SetFillColor(255, 255, 255)
		pdf.Rect(qrEdge-3, y-3, qrSize+6, qrSize+6, "F")
		pdf.ImageOptions(r.qr, qrEdge, y, qrSize, qrSize, false, fpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	}
	tx := qrEdge + qrSize + 6
	r.text(tx, y+6, 100, "B", 9, "L", "Código de Seguridad: "+r.opt.CodigoSeguridad)
	r.text(tx, y+11, 100, "", 9, "L", "Fecha de Firma Digital: "+r.e.FechaHoraFirma)
	r.text(qrEdge, y+qrSize+3, 60, "", 7.5, "L", "Código de Seguridad: "+r.opt.CodigoSeguridad)
	if len(r.pages) > 1 {
		r.text(pageW-margin-40, pageH-qrEdge, 40, "", 8, "R", "Página "+strconv.Itoa(pageNo)+" de "+strconv.Itoa(len(r.pages)))
	}
}

func (r *renderer) page(idx, desde, hasta int, last bool) {
	r.pdf.AddPage()
	y := r.header()
	y = r.tableHeader(y)
	limit := pageH - footerH
	for i := desde - 1; i < hasta; i++ {
		if y > limit-6 {
			// Overflow beyond the declared pagination: continue on a new
			// page with the same header.
			r.footer(r.pdf.PageNo())
			r.pdf.AddPage()
			y = r.tableHeader(r.header())
		}
		y = r.row(y, i)
	}
	y += 2
	if last {
		if y > limit-60 {
			r.footer(r.pdf.PageNo())
			r.pdf.AddPage()
			y = r.header()
		}
		r.totals(y)
	} else {
		r.pageSubtotals(y, idx)
	}
	r.footer(idx + 1)
}

func deref(d *decimal.Decimal) decimal.Decimal {
	if d == nil {
		return decimal.Zero
	}
	return *d
}
