package ecf

import (
	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// MaxPaginas is the maximum PaginaNo allowed by the format.
const MaxPaginas = 100

// Paginar splits the detail lines into pages of lineasPorPagina lines and
// builds the Paginacion section (subtotals are filled by Complete). The
// section is removed when everything fits in a single page, since it only
// applies to printed representations with more than one page.
func Paginar(e *ECF, lineasPorPagina int) {
	n := len(e.DetallesItems.Item)
	if lineasPorPagina <= 0 || n <= lineasPorPagina {
		e.Paginacion = nil
		e.Encabezado.IdDoc.TotalPaginas = 0
		return
	}
	p := &Paginacion{}
	for desde, no := 1, 1; desde <= n; desde, no = desde+lineasPorPagina, no+1 {
		hasta := desde + lineasPorPagina - 1
		if hasta > n {
			hasta = n
		}
		p.Pagina = append(p.Pagina, Pagina{PaginaNo: no, NoLineaDesde: desde, NoLineaHasta: hasta})
	}
	e.Paginacion = p
	e.Encabezado.IdDoc.TotalPaginas = len(p.Pagina)
}

// PaginaComputada holds the computed subtotals of a page.
type PaginaComputada struct {
	Gravado1, Gravado2, Gravado3, Exento, NoFacturable decimal.Decimal
	ITBIS1, ITBIS2, ITBIS3                             decimal.Decimal
	ISC, Otros                                         decimal.Decimal
	Has1, Has2, Has3, HasExento, HasNoFact             bool
}

// GravadoTotal sums the taxed bases of the page.
func (p *PaginaComputada) GravadoTotal() decimal.Decimal {
	return p.Gravado1.Add(p.Gravado2).Add(p.Gravado3)
}

// ITBIS sums the page ITBIS.
func (p *PaginaComputada) ITBIS() decimal.Decimal { return p.ITBIS1.Add(p.ITBIS2).Add(p.ITBIS3) }

// ImpuestoAdicional sums ISC and other additional taxes of the page.
func (p *PaginaComputada) ImpuestoAdicional() decimal.Decimal { return p.ISC.Add(p.Otros) }

// Monto = gravado + exento + ITBIS + impuesto adicional.
func (p *PaginaComputada) Monto() decimal.Decimal {
	return p.GravadoTotal().Add(p.Exento).Add(p.ITBIS()).Add(p.ImpuestoAdicional())
}

// ComputePagina derives the subtotals of the lines desde..hasta (1-based,
// inclusive). Global discounts/surcharges are assigned to the last page,
// which carries the document totals, so that Σ MontoSubtotalPagina equals
// MontoTotal.
func ComputePagina(e *ECF, c *Computed, desde, hasta int, ultima bool) PaginaComputada {
	var p PaginaComputada
	incluido := montoGravadoIncluido(e)
	var sum, isc [5]decimal.Decimal
	var has [5]bool
	items := e.DetallesItems.Item
	for i := desde - 1; i < hasta && i < len(items); i++ {
		if i < 0 {
			continue
		}
		it := &items[i]
		ind := it.IndicadorFacturacion
		if ind < 0 || ind > 4 {
			continue
		}
		v := it.MontoItem
		if incluido && ind == ITBISTasa1 {
			v = div(v, one.Add(TasaITBIS1))
		} else if incluido && ind == ITBISTasa2 {
			v = div(v, one.Add(TasaITBIS2))
		}
		sum[ind] = sum[ind].Add(v)
		has[ind] = true
		if i < len(c.ISCItem) {
			isc[ind] = isc[ind].Add(c.ISCItem[i])
			p.ISC = p.ISC.Add(c.ISCItem[i])
			p.Otros = p.Otros.Add(c.OtrosItem[i])
		}
	}
	if ultima {
		ajustes, _, _, _, _ := globalAjustes(e)
		for ind := 1; ind <= 4; ind++ {
			if !ajustes[ind].IsZero() {
				sum[ind] = sum[ind].Round(2).Add(ajustes[ind])
				has[ind] = true
			}
		}
	}
	p.Gravado1, p.Gravado2, p.Gravado3 = sum[1].Round(2), sum[2].Round(2), sum[3].Round(2)
	p.Exento, p.NoFacturable = sum[4].Round(2), sum[0].Round(2)
	p.ITBIS1 = mul(p.Gravado1.Add(isc[1]), TasaITBIS1).Round(2)
	p.ITBIS2 = mul(p.Gravado2.Add(isc[2]), TasaITBIS2).Round(2)
	p.ISC, p.Otros = p.ISC.Round(2), p.Otros.Round(2)
	p.Has1, p.Has2, p.Has3, p.HasExento, p.HasNoFact = has[1], has[2], has[3], has[4], has[0]
	return p
}

func completePaginacion(e *ECF, c *Computed) {
	pages := e.Paginacion.Pagina
	for i := range pages {
		pg := &pages[i]
		pg.PaginaNo = i + 1
		p := ComputePagina(e, c, pg.NoLineaDesde, pg.NoLineaHasta, i == len(pages)-1)
		*pg = Pagina{PaginaNo: pg.PaginaNo, NoLineaDesde: pg.NoLineaDesde, NoLineaHasta: pg.NoLineaHasta}
		if p.Has1 {
			pg.SubtotalMontoGravado1Pagina, pg.SubtotalItbis1Pagina = decPtr(p.Gravado1), decPtr(p.ITBIS1)
		}
		if p.Has2 {
			pg.SubtotalMontoGravado2Pagina, pg.SubtotalItbis2Pagina = decPtr(p.Gravado2), decPtr(p.ITBIS2)
		}
		if p.Has3 {
			pg.SubtotalMontoGravado3Pagina, pg.SubtotalItbis3Pagina = decPtr(p.Gravado3), decPtr(p.ITBIS3)
		}
		if p.Has1 || p.Has2 || p.Has3 {
			pg.SubtotalMontoGravadoPagina = decPtr(p.GravadoTotal())
			pg.SubtotalItbisPagina = decPtr(p.ITBIS())
		}
		if p.HasExento {
			pg.SubtotalExentoPagina = decPtr(p.Exento)
		}
		if p.ImpuestoAdicional().Sign() > 0 {
			pg.SubtotalImpuestoAdicionalPagina = decPtr(p.ImpuestoAdicional())
			sia := &SubtotalImpuestoAdicional{}
			if p.ISC.Sign() > 0 {
				sia.SubtotalImpuestoSelectivoConsumoEspecificoPagina = decPtr(p.ISC)
			}
			if p.Otros.Sign() > 0 {
				sia.SubtotalOtrosImpuesto = decPtr(p.Otros)
			}
			pg.SubtotalImpuestoAdicional = sia
		}
		pg.MontoSubtotalPagina = decPtr(p.Monto())
		if p.HasNoFact && !p.NoFacturable.IsZero() {
			pg.SubtotalMontoNoFacturablePagina = decPtr(p.NoFacturable)
		}
	}
}
