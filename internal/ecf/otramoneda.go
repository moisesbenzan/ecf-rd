package ecf

import (
	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// ComputedOtraMoneda holds the totals in the alternative currency.
type ComputedOtraMoneda struct {
	Gravado1, Gravado2, Gravado3, Exento decimal.Decimal
	ITBIS1, ITBIS2, ITBIS3               decimal.Decimal
	Impuestos                            []ImpuestoAdicionalOtraMoneda
	ImpuestoAdicional                    decimal.Decimal
	Has1, Has2, Has3, HasExento          bool
}

// GravadoTotal sums the taxed bases.
func (o *ComputedOtraMoneda) GravadoTotal() decimal.Decimal {
	return o.Gravado1.Add(o.Gravado2).Add(o.Gravado3)
}

// TotalITBIS sums ITBIS.
func (o *ComputedOtraMoneda) TotalITBIS() decimal.Decimal {
	return o.ITBIS1.Add(o.ITBIS2).Add(o.ITBIS3)
}

// MontoTotal = gravado + exento + ITBIS + impuestos adicionales.
func (o *ComputedOtraMoneda) MontoTotal() decimal.Decimal {
	return o.GravadoTotal().Add(o.Exento).Add(o.TotalITBIS()).Add(o.ImpuestoAdicional)
}

// ComputeOtraMoneda derives the foreign-currency totals. Line amounts come
// from OtraMonedaDetalle when every line carries it, otherwise DOP amounts
// are converted with TipoCambio. Additional taxes are always computed in
// DOP first and then converted (Informe Técnico, sección 14 "Otra Moneda").
func ComputeOtraMoneda(e *ECF, c *Computed) (ComputedOtraMoneda, bool) {
	var o ComputedOtraMoneda
	om := e.Encabezado.OtraMoneda
	if om == nil || om.TipoCambio.Sign() <= 0 {
		return o, false
	}
	tc := om.TipoCambio
	conv := func(v decimal.Decimal) decimal.Decimal { return div(v, tc) }
	items := e.DetallesItems.Item
	allForeign := len(items) > 0
	for i := range items {
		if items[i].OtraMonedaDetalle == nil {
			allForeign = false
			break
		}
	}
	incluido := montoGravadoIncluido(e)
	var sum, isc [5]decimal.Decimal
	var has [5]bool
	for i := range items {
		it := &items[i]
		ind := it.IndicadorFacturacion
		if ind < 1 || ind > 4 {
			continue
		}
		var v decimal.Decimal
		if allForeign {
			v = it.OtraMonedaDetalle.MontoItemOtraMoneda
		} else {
			v = conv(it.MontoItem)
		}
		if incluido && ind == ITBISTasa1 {
			v = div(v, one.Add(TasaITBIS1))
		} else if incluido && ind == ITBISTasa2 {
			v = div(v, one.Add(TasaITBIS2))
		}
		sum[ind] = sum[ind].Add(v)
		if i < len(c.ISCItem) {
			isc[ind] = isc[ind].Add(conv(c.ISCItem[i]))
		}
		has[ind] = true
	}
	if dr := e.DescuentosORecargos; dr != nil {
		for _, l := range dr.DescuentoORecargo {
			if l.IndicadorNorma1007 != nil && *l.IndicadorNorma1007 == 1 {
				continue
			}
			ind := 0
			if l.IndicadorFacturacionDescuentooRecargo != nil {
				ind = *l.IndicadorFacturacionDescuentooRecargo
			}
			if ind < 1 || ind > 4 {
				continue
			}
			v := conv(l.MontoDescuentooRecargo)
			if l.MontoDescuentooRecargoOtraMoneda != nil {
				v = *l.MontoDescuentooRecargoOtraMoneda
			}
			if l.TipoAjuste == "D" {
				v = v.Neg()
			}
			sum[ind] = sum[ind].Add(v)
			has[ind] = true
		}
	}
	o.Gravado1, o.Gravado2, o.Gravado3 = sum[1].Round(2), sum[2].Round(2), sum[3].Round(2)
	o.Exento = sum[4].Round(2)
	o.Has1, o.Has2, o.Has3, o.HasExento = has[1], has[2], has[3], has[4]
	o.ITBIS1 = mul(o.Gravado1.Add(isc[1]), TasaITBIS1).Round(2)
	o.ITBIS2 = mul(o.Gravado2.Add(isc[2]), TasaITBIS2).Round(2)
	for _, imp := range c.Impuestos {
		entry := ImpuestoAdicionalOtraMoneda{
			TipoImpuestoOtraMoneda:          imp.TipoImpuesto,
			TasaImpuestoAdicionalOtraMoneda: imp.TasaImpuestoAdicional,
		}
		if v := imp.MontoImpuestoSelectivoConsumoEspecifico; v != nil {
			entry.MontoImpuestoSelectivoConsumoEspecificoOtraMoneda = decPtr(conv(*v).Round(2))
		}
		if v := imp.MontoImpuestoSelectivoConsumoAdvalorem; v != nil {
			entry.MontoImpuestoSelectivoConsumoAdvaloremOtraMoneda = decPtr(conv(*v).Round(2))
		}
		if v := imp.OtrosImpuestosAdicionales; v != nil {
			entry.OtrosImpuestosAdicionalesOtraMoneda = decPtr(conv(*v).Round(2))
		}
		o.ImpuestoAdicional = o.ImpuestoAdicional.Add(impuestoAdicionalOtraMonedaMonto(entry))
		o.Impuestos = append(o.Impuestos, entry)
	}
	return o, true
}

func impuestoAdicionalOtraMonedaMonto(i ImpuestoAdicionalOtraMoneda) decimal.Decimal {
	return deref(i.MontoImpuestoSelectivoConsumoEspecificoOtraMoneda).
		Add(deref(i.MontoImpuestoSelectivoConsumoAdvaloremOtraMoneda)).
		Add(deref(i.OtrosImpuestosAdicionalesOtraMoneda))
}

func completeOtraMoneda(e *ECF, c *Computed) {
	o, ok := ComputeOtraMoneda(e, c)
	if !ok {
		return
	}
	om := e.Encabezado.OtraMoneda
	om.MontoGravadoTotalOtraMoneda, om.MontoGravado1OtraMoneda, om.MontoGravado2OtraMoneda, om.MontoGravado3OtraMoneda = nil, nil, nil, nil
	om.TotalITBISOtraMoneda, om.TotalITBIS1OtraMoneda, om.TotalITBIS2OtraMoneda, om.TotalITBIS3OtraMoneda = nil, nil, nil, nil
	om.MontoExentoOtraMoneda, om.MontoImpuestoAdicionalOtraMoneda, om.ImpuestosAdicionalesOtraMoneda = nil, nil, nil
	if o.Has1 {
		om.MontoGravado1OtraMoneda, om.TotalITBIS1OtraMoneda = decPtr(o.Gravado1), decPtr(o.ITBIS1)
	}
	if o.Has2 {
		om.MontoGravado2OtraMoneda, om.TotalITBIS2OtraMoneda = decPtr(o.Gravado2), decPtr(o.ITBIS2)
	}
	if o.Has3 {
		om.MontoGravado3OtraMoneda, om.TotalITBIS3OtraMoneda = decPtr(o.Gravado3), decPtr(o.ITBIS3)
	}
	if o.Has1 || o.Has2 || o.Has3 {
		om.MontoGravadoTotalOtraMoneda = decPtr(o.GravadoTotal())
		om.TotalITBISOtraMoneda = decPtr(o.TotalITBIS())
	}
	if o.HasExento {
		om.MontoExentoOtraMoneda = decPtr(o.Exento)
	}
	if len(o.Impuestos) > 0 {
		om.ImpuestosAdicionalesOtraMoneda = &ImpuestosAdicionalesOtraMoneda{ImpuestoAdicionalOtraMoneda: o.Impuestos}
		om.MontoImpuestoAdicionalOtraMoneda = decPtr(o.ImpuestoAdicional)
	}
	om.MontoTotalOtraMoneda = o.MontoTotal()
}
