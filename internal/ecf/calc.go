package ecf

import (
	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// Computed holds totals derived from the detail lines of an e-CF.
type Computed struct {
	MontoGravadoI1, MontoGravadoI2, MontoGravadoI3 decimal.Decimal
	MontoExento                                    decimal.Decimal
	MontoNoFacturable                              decimal.Decimal
	TotalITBIS1, TotalITBIS2, TotalITBIS3          decimal.Decimal
	TotalITBISRetenido, TotalISRRetencion          decimal.Decimal
	HasRetencion                                   bool
	HasI1, HasI2, HasI3, HasExento, HasNoFact      bool
}

// MontoGravadoTotal is the sum of the taxed bases.
func (c *Computed) MontoGravadoTotal() decimal.Decimal {
	return c.MontoGravadoI1.Add(c.MontoGravadoI2).Add(c.MontoGravadoI3)
}

// TotalITBIS is the sum of ITBIS at all rates.
func (c *Computed) TotalITBIS() decimal.Decimal {
	return c.TotalITBIS1.Add(c.TotalITBIS2).Add(c.TotalITBIS3)
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

// Compute derives the tax bases and ITBIS amounts from the detail lines and
// global discounts/surcharges. It does not modify e.
//
// When IndicadorMontoGravado is 1 the line amounts include ITBIS and the
// base is obtained by dividing the line total by (1 + rate).
//
// Selective consumption taxes (ISC) and other additional taxes are not
// derived here: their amounts are taken from Totales as supplied.
func Compute(e *ECF) (Computed, error) {
	var c Computed
	var sum [5]decimal.Decimal // indexed by IndicadorFacturacion
	var has [5]bool
	for i := range e.DetallesItems.Item {
		it := &e.DetallesItems.Item[i]
		ind := it.IndicadorFacturacion
		if ind < 0 || ind > 4 {
			continue
		}
		sum[ind] = sum[ind].Add(it.MontoItem)
		has[ind] = true
		if r := it.Retencion; r != nil {
			c.HasRetencion = true
			if r.MontoITBISRetenido != nil {
				c.TotalITBISRetenido = c.TotalITBISRetenido.Add(*r.MontoITBISRetenido)
			}
			if r.MontoISRRetenido != nil {
				c.TotalISRRetencion = c.TotalISRRetencion.Add(*r.MontoISRRetenido)
			}
		}
	}
	if dr := e.DescuentosORecargos; dr != nil {
		for _, d := range dr.DescuentoORecargo {
			ind := 0
			if d.IndicadorFacturacionDescuentooRecargo != nil {
				ind = *d.IndicadorFacturacionDescuentooRecargo
			}
			if ind < 1 || ind > 4 {
				continue
			}
			switch d.TipoAjuste {
			case "D":
				sum[ind] = sum[ind].Sub(d.MontoDescuentooRecargo)
			case "R":
				sum[ind] = sum[ind].Add(d.MontoDescuentooRecargo)
			}
			has[ind] = true
		}
	}

	incluido := e.Encabezado.IdDoc.IndicadorMontoGravado != nil && *e.Encabezado.IdDoc.IndicadorMontoGravado == 1
	base := func(total, rate decimal.Decimal) (decimal.Decimal, decimal.Decimal, error) {
		if incluido {
			b, err := total.Div(decimal.FromInt(1).Add(rate))
			if err != nil {
				return decimal.Zero, decimal.Zero, err
			}
			b = b.Round(2)
			return b, total.Sub(b), nil
		}
		tax, err := total.MulChecked(rate)
		if err != nil {
			return decimal.Zero, decimal.Zero, err
		}
		return total, tax.Round(2), nil
	}
	var err error
	if c.MontoGravadoI1, c.TotalITBIS1, err = base(sum[ITBISTasa1], TasaITBIS1); err != nil {
		return c, err
	}
	if c.MontoGravadoI2, c.TotalITBIS2, err = base(sum[ITBISTasa2], TasaITBIS2); err != nil {
		return c, err
	}
	c.MontoGravadoI3 = sum[ITBISTasa3]
	c.TotalITBIS3 = decimal.Zero
	c.MontoExento = sum[Exento]
	c.MontoNoFacturable = sum[NoFacturable]
	c.HasI1, c.HasI2, c.HasI3 = has[ITBISTasa1], has[ITBISTasa2], has[ITBISTasa3]
	c.HasExento, c.HasNoFact = has[Exento], has[NoFacturable]
	return c, nil
}

func decPtr(d decimal.Decimal) *decimal.Decimal { return &d }

func intPtr(i int) *int { return &i }

// Complete fills derived fields so that callers only need to supply the
// business data:
//
//   - Encabezado.Version
//   - Item.NumeroLinea (sequential) and Item.MontoItem (when zero)
//   - DescuentoORecargo.NumeroLinea
//   - every Totales field derivable from the lines (bases, ITBIS, exento,
//     no facturable, retenciones, MontoTotal)
//   - IdDoc.IndicadorNotaCredito for type 34 when InformacionReferencia is
//     present
//   - IdDoc.TotalPaginas when Paginacion is present
//
// Values for MontoImpuestoAdicional/ImpuestosAdicionales are preserved and
// included in MontoTotal.
func Complete(e *ECF) error {
	h := &e.Encabezado
	if h.Version == "" {
		h.Version = Version
	}
	for i := range e.DetallesItems.Item {
		it := &e.DetallesItems.Item[i]
		it.NumeroLinea = i + 1
		if it.MontoItem.IsZero() {
			m, err := LineAmount(it)
			if err != nil {
				return err
			}
			it.MontoItem = m
		}
	}
	if dr := e.DescuentosORecargos; dr != nil {
		for i := range dr.DescuentoORecargo {
			dr.DescuentoORecargo[i].NumeroLinea = i + 1
		}
	}
	if p := e.Paginacion; p != nil && len(p.Pagina) > 0 {
		h.IdDoc.TotalPaginas = len(p.Pagina)
	}

	c, err := Compute(e)
	if err != nil {
		return err
	}
	t := &h.Totales
	t.MontoGravadoI1, t.MontoGravadoI2, t.MontoGravadoI3 = nil, nil, nil
	t.TotalITBIS1, t.TotalITBIS2, t.TotalITBIS3 = nil, nil, nil
	t.ITBIS1, t.ITBIS2, t.ITBIS3 = nil, nil, nil
	t.MontoGravadoTotal, t.TotalITBIS, t.MontoExento, t.MontoNoFacturable = nil, nil, nil, nil
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
	if c.HasRetencion {
		t.TotalITBISRetenido = decPtr(c.TotalITBISRetenido)
		t.TotalISRRetencion = decPtr(c.TotalISRRetencion)
	}
	t.MontoTotal = expectedMontoTotal(&c, t)

	if h.IdDoc.TipoeCF == NotaCredito && e.InformacionReferencia != nil {
		if ind, ok := indicadorNotaCredito(h.Emisor.FechaEmision, e.InformacionReferencia.FechaNCFModificado); ok {
			h.IdDoc.IndicadorNotaCredito = intPtr(ind)
		}
	}
	return nil
}

func expectedMontoTotal(c *Computed, t *Totales) decimal.Decimal {
	total := c.MontoGravadoTotal().Add(c.MontoExento).Add(c.TotalITBIS())
	if t.MontoImpuestoAdicional != nil {
		total = total.Add(*t.MontoImpuestoAdicional)
	}
	return total
}

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
