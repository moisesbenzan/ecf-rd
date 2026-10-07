package ecf

import (
	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// Computed holds the totals derived from the detail lines of an e-CF
// following the "Formato e-CF" totals rules and the Informe Técnico e-CF.
type Computed struct {
	MontoGravadoI1, MontoGravadoI2, MontoGravadoI3 decimal.Decimal
	MontoExento                                    decimal.Decimal
	MontoNoFacturable                              decimal.Decimal
	TotalITBIS1, TotalITBIS2, TotalITBIS3          decimal.Decimal
	TotalITBISRetenido, TotalISRRetencion          decimal.Decimal
	TotalITBISPercepcion, TotalISRPercepcion       decimal.Decimal
	HasRetencion, HasPercepcion                    bool
	HasI1, HasI2, HasI3, HasExento, HasNoFact      bool

	// Additional taxes (Tabla I), one entry per code, rounded.
	Impuestos              []ImpuestoAdicionalTotal
	MontoImpuestoAdicional decimal.Decimal
	// ISCItem and OtrosItem are the selective consumption taxes and the
	// other additional taxes per line (unrounded).
	ISCItem, OtrosItem []decimal.Decimal
	// ISCEspecificoItem and ISCAdValoremItem split ISCItem per class.
	ISCEspecificoItem, ISCAdValoremItem []decimal.Decimal
	// DescuentoNorma1007 is the sum of global discounts flagged with
	// IndicadorNorma1007, which reduce ValorPagar but not the tax bases.
	DescuentoNorma1007 decimal.Decimal
}

// MontoGravadoTotal is the sum of the taxed bases.
func (c *Computed) MontoGravadoTotal() decimal.Decimal {
	return c.MontoGravadoI1.Add(c.MontoGravadoI2).Add(c.MontoGravadoI3)
}

// TotalITBIS is the sum of ITBIS at all rates.
func (c *Computed) TotalITBIS() decimal.Decimal {
	return c.TotalITBIS1.Add(c.TotalITBIS2).Add(c.TotalITBIS3)
}

// MontoTotal = gravado + exento + ITBIS + impuestos adicionales.
func (c *Computed) MontoTotal() decimal.Decimal {
	return c.MontoGravadoTotal().Add(c.MontoExento).Add(c.TotalITBIS()).Add(c.MontoImpuestoAdicional)
}

// LineAmount computes MontoItem = round2(CantidadItem × PrecioUnitarioItem)
// − DescuentoMonto + RecargoMonto.
func LineAmount(it *Item) (decimal.Decimal, error) {
	m, err := it.CantidadItem.MulChecked(it.PrecioUnitarioItem)
	if err != nil {
		return decimal.Zero, err
	}
	m = m.Round(2)
	if it.DescuentoMonto != nil {
		m = m.Sub(*it.DescuentoMonto)
	}
	if it.RecargoMonto != nil {
		m = m.Add(*it.RecargoMonto)
	}
	return m, nil
}

func montoGravadoIncluido(e *ECF) bool {
	return e.Encabezado.IdDoc.IndicadorMontoGravado != nil && *e.Encabezado.IdDoc.IndicadorMontoGravado == 1
}

// globalAjustes returns, per IndicadorFacturacion (1-4), the net global
// discount (negative) / surcharge (positive) that affects the tax bases,
// plus the total discounts and surcharges used to apportion additional
// taxes, and the Norma 10-07 discounts.
func globalAjustes(e *ECF) (porInd [5]decimal.Decimal, desc, rec, norma1007 decimal.Decimal, hasNorma bool) {
	dr := e.DescuentosORecargos
	if dr == nil {
		return
	}
	for _, l := range dr.DescuentoORecargo {
		isNorma := l.IndicadorNorma1007 != nil && *l.IndicadorNorma1007 == 1
		if isNorma {
			hasNorma = true
		}
		switch l.TipoAjuste {
		case "D":
			if isNorma {
				norma1007 = norma1007.Add(l.MontoDescuentooRecargo)
				continue
			}
			desc = desc.Add(l.MontoDescuentooRecargo)
		case "R":
			rec = rec.Add(l.MontoDescuentooRecargo)
		default:
			continue
		}
		ind := 0
		if l.IndicadorFacturacionDescuentooRecargo != nil {
			ind = *l.IndicadorFacturacionDescuentooRecargo
		}
		if ind < 1 || ind > 4 {
			continue
		}
		if l.TipoAjuste == "D" {
			porInd[ind] = porInd[ind].Sub(l.MontoDescuentooRecargo)
		} else {
			porInd[ind] = porInd[ind].Add(l.MontoDescuentooRecargo)
		}
	}
	return
}

// Compute derives every total from the detail lines, global discounts and
// surcharges. It does not modify e.
func Compute(e *ECF) (Computed, error) {
	var c Computed
	items := e.DetallesItems.Item
	incluido := montoGravadoIncluido(e)
	ajustes, desc, rec, norma, hasNorma := globalAjustes(e)
	c.DescuentoNorma1007 = norma
	imp := computeImpuestos(e, incluido, hasNorma, desc, rec)
	c.ISCItem, c.OtrosItem = imp.iscItem, imp.otrosItem
	c.ISCEspecificoItem, c.ISCAdValoremItem = imp.espItem, imp.advItem
	c.Impuestos, c.MontoImpuestoAdicional = imp.totales()

	over := tasas(e)
	var sum, isc [5]decimal.Decimal
	var has [5]bool
	for i := range items {
		it := &items[i]
		ind := it.IndicadorFacturacion
		if ind < 0 || ind > 4 {
			continue
		}
		amount := it.MontoItem
		if incluido && (ind == ITBISTasa1 || ind == ITBISTasa2) {
			divisor := one.Add(TasaITBIS1)
			if ind == ITBISTasa2 {
				divisor = one.Add(TasaITBIS2)
			}
			if hasNorma && ind == ITBISTasa1 {
				for _, code := range itemCodes(it) {
					if code == "002" || code == "004" {
						t, _ := tasaDe(over, code)
						divisor = divisor.Add(pct(t))
					}
				}
			}
			amount = div(amount, divisor)
		}
		sum[ind] = sum[ind].Add(amount)
		isc[ind] = isc[ind].Add(imp.iscItem[i])
		has[ind] = true
		if r := it.Retencion; r != nil {
			switch r.IndicadorAgenteRetencionoPercepcion {
			case 1:
				c.HasRetencion = true
				c.TotalITBISRetenido = c.TotalITBISRetenido.Add(deref(r.MontoITBISRetenido))
				c.TotalISRRetencion = c.TotalISRRetencion.Add(deref(r.MontoISRRetenido))
			case 2:
				c.HasPercepcion = true
				c.TotalITBISPercepcion = c.TotalITBISPercepcion.Add(deref(r.MontoITBISRetenido))
				c.TotalISRPercepcion = c.TotalISRPercepcion.Add(deref(r.MontoISRRetenido))
			}
		}
	}
	for ind := 1; ind <= 4; ind++ {
		if !ajustes[ind].IsZero() {
			sum[ind] = sum[ind].Round(2).Add(ajustes[ind])
			has[ind] = true
		}
	}
	c.MontoGravadoI1 = sum[ITBISTasa1].Round(2)
	c.MontoGravadoI2 = sum[ITBISTasa2].Round(2)
	c.MontoGravadoI3 = sum[ITBISTasa3].Round(2)
	c.MontoExento = sum[Exento].Round(2)
	c.MontoNoFacturable = sum[NoFacturable].Round(2)
	// Selective consumption taxes (006-039) are part of the ITBIS base.
	c.TotalITBIS1 = mul(c.MontoGravadoI1.Add(isc[ITBISTasa1]), TasaITBIS1).Round(2)
	c.TotalITBIS2 = mul(c.MontoGravadoI2.Add(isc[ITBISTasa2]), TasaITBIS2).Round(2)
	c.TotalITBIS3 = decimal.Zero
	c.HasI1, c.HasI2, c.HasI3 = has[ITBISTasa1], has[ITBISTasa2], has[ITBISTasa3]
	c.HasExento, c.HasNoFact = has[Exento], has[NoFacturable]
	return c, nil
}

// ItemITBIS returns the ITBIS of line i: (taxable base + ISC) × rate, where
// the base excludes ITBIS when IndicadorMontoGravado = 1.
func ItemITBIS(e *ECF, c *Computed, i int) decimal.Decimal {
	it := &e.DetallesItems.Item[i]
	var rate decimal.Decimal
	switch it.IndicadorFacturacion {
	case ITBISTasa1:
		rate = TasaITBIS1
	case ITBISTasa2:
		rate = TasaITBIS2
	default:
		return decimal.Zero
	}
	base := it.MontoItem
	if montoGravadoIncluido(e) {
		base = div(base, one.Add(rate))
	}
	if i < len(c.ISCItem) {
		base = base.Add(c.ISCItem[i])
	}
	return mul(base, rate).Round(2)
}

func decPtr(d decimal.Decimal) *decimal.Decimal { return &d }

func intPtr(i int) *int { return &i }

// Complete fills derived fields so that callers only need to supply the
// business data:
//
//   - Encabezado.Version; an empty Comprador for type 32 (the XSD requires
//     the element)
//   - Item.NumeroLinea (sequential), Item.MontoItem (when zero),
//     OtraMonedaDetalle.MontoItemOtraMoneda (when zero), sub-discount and
//     sub-surcharge amounts given as percentages, and DescuentoMonto /
//     RecargoMonto from their tables
//   - DescuentoORecargo.NumeroLinea and amounts given as percentages
//   - every Totales field derivable from the lines: taxed bases, ITBIS,
//     exempt, non-billable, additional taxes (Tabla I), retentions,
//     perceptions, MontoTotal, and MontoPeriodo/ValorPagar when present
//   - OtraMoneda totals when TipoMoneda and TipoCambio are given
//   - IdDoc.IndicadorNotaCredito for type 34, IdDoc.TotalPaginas and the
//     Paginacion subtotals
func Complete(e *ECF) error {
	h := &e.Encabezado
	if h.Version == "" {
		h.Version = Version
	}
	if h.IdDoc.TipoeCF == FacturaConsumo && h.Comprador == nil {
		h.Comprador = &Comprador{}
	}
	for i := range e.DetallesItems.Item {
		it := &e.DetallesItems.Item[i]
		it.NumeroLinea = i + 1
		completeSubAjustes(it)
		if it.MontoItem.IsZero() {
			m, err := LineAmount(it)
			if err != nil {
				return err
			}
			it.MontoItem = m
		}
		if om := it.OtraMonedaDetalle; om != nil && om.MontoItemOtraMoneda.IsZero() {
			m := mul(om.PrecioOtraMoneda, it.CantidadItem).Round(2)
			om.MontoItemOtraMoneda = m.Sub(deref(om.DescuentoOtraMoneda)).Add(deref(om.RecargoOtraMoneda))
		}
	}
	completeDescuentos(e)

	c, err := Compute(e)
	if err != nil {
		return err
	}
	t := &h.Totales
	t.MontoGravadoI1, t.MontoGravadoI2, t.MontoGravadoI3 = nil, nil, nil
	t.TotalITBIS1, t.TotalITBIS2, t.TotalITBIS3 = nil, nil, nil
	t.ITBIS1, t.ITBIS2, t.ITBIS3 = nil, nil, nil
	t.MontoGravadoTotal, t.TotalITBIS, t.MontoExento, t.MontoNoFacturable = nil, nil, nil, nil
	t.MontoImpuestoAdicional, t.ImpuestosAdicionales = nil, nil
	if c.HasI1 {
		t.MontoGravadoI1, t.TotalITBIS1, t.ITBIS1 = decPtr(c.MontoGravadoI1), decPtr(c.TotalITBIS1), intPtr(18)
	}
	if c.HasI2 {
		t.MontoGravadoI2, t.TotalITBIS2, t.ITBIS2 = decPtr(c.MontoGravadoI2), decPtr(c.TotalITBIS2), intPtr(16)
	}
	if c.HasI3 {
		t.MontoGravadoI3, t.TotalITBIS3, t.ITBIS3 = decPtr(c.MontoGravadoI3), decPtr(c.TotalITBIS3), intPtr(0)
	}
	if c.HasI1 || c.HasI2 || c.HasI3 {
		t.MontoGravadoTotal = decPtr(c.MontoGravadoTotal())
		t.TotalITBIS = decPtr(c.TotalITBIS())
	}
	if c.HasExento {
		t.MontoExento = decPtr(c.MontoExento)
	}
	if c.HasNoFact && !c.MontoNoFacturable.IsZero() {
		t.MontoNoFacturable = decPtr(c.MontoNoFacturable)
	}
	if len(c.Impuestos) > 0 {
		t.ImpuestosAdicionales = &ImpuestosAdicionales{ImpuestoAdicional: c.Impuestos}
		t.MontoImpuestoAdicional = decPtr(c.MontoImpuestoAdicional)
	}
	t.TotalITBISRetenido, t.TotalISRRetencion, t.TotalITBISPercepcion, t.TotalISRPercepcion = nil, nil, nil, nil
	if c.HasRetencion {
		if rTotalITBISRetenido.For(h.IdDoc.TipoeCF) != no {
			t.TotalITBISRetenido = decPtr(c.TotalITBISRetenido)
		}
		if !c.TotalISRRetencion.IsZero() || h.IdDoc.TipoeCF == PagosExterior {
			t.TotalISRRetencion = decPtr(c.TotalISRRetencion)
		}
	}
	if c.HasPercepcion {
		if !c.TotalITBISPercepcion.IsZero() {
			t.TotalITBISPercepcion = decPtr(c.TotalITBISPercepcion)
		}
		if !c.TotalISRPercepcion.IsZero() {
			t.TotalISRPercepcion = decPtr(c.TotalISRPercepcion)
		}
	}
	t.MontoTotal = c.MontoTotal()
	if t.MontoPeriodo != nil {
		t.MontoPeriodo = decPtr(t.MontoTotal.Add(deref(t.MontoNoFacturable)))
	}
	if t.ValorPagar != nil || t.MontoAvancePago != nil || t.SaldoAnterior != nil {
		t.ValorPagar = decPtr(valorPagar(t, c.DescuentoNorma1007))
	}

	completeOtraMoneda(e, &c)

	if h.IdDoc.TipoeCF == NotaCredito && e.InformacionReferencia != nil {
		if ind, ok := indicadorNotaCredito(h.Emisor.FechaEmision, e.InformacionReferencia.FechaNCFModificado); ok {
			h.IdDoc.IndicadorNotaCredito = intPtr(ind)
		}
	}
	if p := e.Paginacion; p != nil && len(p.Pagina) > 0 {
		completePaginacion(e, &c)
		h.IdDoc.TotalPaginas = len(p.Pagina)
	}
	return nil
}

// valorPagar = MontoTotal − MontoAvancePago ± SaldoAnterior (− Norma 10-07
// discounts).
func valorPagar(t *Totales, norma1007 decimal.Decimal) decimal.Decimal {
	return t.MontoTotal.Sub(norma1007).Sub(deref(t.MontoAvancePago)).Add(deref(t.SaldoAnterior))
}

func completeSubAjustes(it *Item) {
	if t := it.TablaSubDescuento; t != nil {
		sum := decimal.Zero
		for i := range t.SubDescuento {
			s := &t.SubDescuento[i]
			if s.TipoSubDescuento == "%" && s.SubDescuentoPorcentaje != nil && s.MontoSubDescuento.IsZero() {
				s.MontoSubDescuento = subAjusteMonto(it, *s.SubDescuentoPorcentaje)
			}
			sum = sum.Add(s.MontoSubDescuento)
		}
		if it.DescuentoMonto == nil {
			it.DescuentoMonto = decPtr(sum)
		}
	}
	if t := it.TablaSubRecargo; t != nil {
		sum := decimal.Zero
		for i := range t.SubRecargo {
			s := &t.SubRecargo[i]
			if s.TipoSubRecargo == "%" && s.SubRecargoPorcentaje != nil && s.MontoSubRecargo.IsZero() {
				s.MontoSubRecargo = subAjusteMonto(it, *s.SubRecargoPorcentaje)
			}
			sum = sum.Add(s.MontoSubRecargo)
		}
		if it.RecargoMonto == nil {
			it.RecargoMonto = decPtr(sum)
		}
	}
}

// subAjusteMonto = PrecioUnitarioItem × porcentaje × CantidadItem.
func subAjusteMonto(it *Item, porcentaje decimal.Decimal) decimal.Decimal {
	return mul(mul(it.PrecioUnitarioItem, pct(porcentaje)), it.CantidadItem).Round(2)
}

// completeDescuentos numbers global discount lines and computes amounts
// given as a percentage of the base they affect.
func completeDescuentos(e *ECF) {
	dr := e.DescuentosORecargos
	if dr == nil {
		return
	}
	var base [5]decimal.Decimal
	for i := range e.DetallesItems.Item {
		it := &e.DetallesItems.Item[i]
		if it.IndicadorFacturacion >= 1 && it.IndicadorFacturacion <= 4 {
			base[it.IndicadorFacturacion] = base[it.IndicadorFacturacion].Add(it.MontoItem)
		}
	}
	for i := range dr.DescuentoORecargo {
		l := &dr.DescuentoORecargo[i]
		l.NumeroLinea = i + 1
		if l.TipoValor == "%" && l.ValorDescuentooRecargo != nil && l.MontoDescuentooRecargo.IsZero() {
			b := decimal.Zero
			if l.IndicadorFacturacionDescuentooRecargo != nil && *l.IndicadorFacturacionDescuentooRecargo >= 1 && *l.IndicadorFacturacionDescuentooRecargo <= 4 {
				b = base[*l.IndicadorFacturacionDescuentooRecargo]
			} else {
				for _, v := range base[1:] {
					b = b.Add(v)
				}
			}
			l.MontoDescuentooRecargo = mul(b, pct(*l.ValorDescuentooRecargo)).Round(2)
		}
	}
}

// expectedMontoTotal is kept for callers comparing a supplied total.
func expectedMontoTotal(c *Computed) decimal.Decimal { return c.MontoTotal() }

// indicadorNotaCredito returns 1 when the credit note is issued more than
// 30 calendar days after the modified receipt, 0 otherwise.
func indicadorNotaCredito(fechaEmision, fechaModificado string) (int, bool) {
	fe, err1 := ParseDate(fechaEmision)
	fm, err2 := ParseDate(fechaModificado)
	if err1 != nil || err2 != nil {
		return 0, false
	}
	if fe.Sub(fm).Hours() > 30*24 {
		return 1, true
	}
	return 0, true
}
