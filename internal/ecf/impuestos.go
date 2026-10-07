package ecf

import (
	"fmt"
	"sort"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// ClaseImpuesto groups the additional tax codes of Tabla I by how they are
// computed (Informe Técnico e-CF, secciones 14 y 15).
type ClaseImpuesto int

// Additional tax classes.
const (
	// OtroImpuesto (001-005): percentage over the item amount; reported in
	// OtrosImpuestosAdicionales. Not part of the ITBIS base.
	OtroImpuesto ClaseImpuesto = iota + 1
	// ISCEspecificoAlcohol (006-018): specific amount × alcohol degrees ×
	// reference quantity × sub-quantity × quantity.
	ISCEspecificoAlcohol
	// ISCEspecificoCigarrillos (019-022): specific amount × reference
	// quantity × quantity.
	ISCEspecificoCigarrillos
	// ISCAdValoremAlcohol (023-035): percentage over the retail reference
	// price net of ITBIS and specific ISC.
	ISCAdValoremAlcohol
	// ISCAdValoremCigarrillos (036-039): as above, net of the specific rate.
	ISCAdValoremCigarrillos
)

// ImpuestoAdicional describes a Tabla I entry.
type ImpuestoAdicional struct {
	Codigo      string
	Clase       ClaseImpuesto
	Nombre      string
	Descripcion string
	// Tasa is a percentage (e.g. 10 = 10%) for OtroImpuesto and ad valorem
	// classes, or an amount in DOP for the specific ISC classes. Specific
	// ISC amounts are adjusted quarterly by inflation; callers should pass
	// the rate in force in Totales.ImpuestosAdicionales.TasaImpuestoAdicional.
	Tasa decimal.Decimal
}

// Especifico reports whether the rate is a specific amount.
func (i ImpuestoAdicional) Especifico() bool {
	return i.Clase == ISCEspecificoAlcohol || i.Clase == ISCEspecificoCigarrillos
}

// EsISC reports whether the code is a selective consumption tax (006-039),
// which is part of the ITBIS taxable base.
func (i ImpuestoAdicional) EsISC() bool { return i.Clase != OtroImpuesto }

var d = decimal.MustParse

// TablaImpuestos is Tabla I (Codificación Tipos de Impuestos Adicionales).
var TablaImpuestos = map[string]ImpuestoAdicional{
	"001": {"001", OtroImpuesto, "Propina Legal", "Propina Legal", d("10")},
	"002": {"002", OtroImpuesto, "CDT", "Contribución al Desarrollo de las Telecomunicaciones Ley 153-98 Art. 45", d("2")},
	"003": {"003", OtroImpuesto, "ISC", "Servicios Seguros en general", d("16")},
	"004": {"004", OtroImpuesto, "ISC", "Servicios de Telecomunicaciones", d("10")},
	"005": {"005", OtroImpuesto, "Primera Placa", "Expedición de la primera placa", d("17")},
	"006": {"006", ISCEspecificoAlcohol, "ISC Específico", "Cerveza", d("632.58")},
	"007": {"007", ISCEspecificoAlcohol, "ISC Específico", "Vinos de uva", d("632.58")},
	"008": {"008", ISCEspecificoAlcohol, "ISC Específico", "Vermut y demás vinos de uvas frescas", d("632.58")},
	"009": {"009", ISCEspecificoAlcohol, "ISC Específico", "Demás bebidas fermentadas", d("632.58")},
	"010": {"010", ISCEspecificoAlcohol, "ISC Específico", "Alcohol Etílico sin desnaturalizar (Mayor o igual a 80%)", d("632.58")},
	"011": {"011", ISCEspecificoAlcohol, "ISC Específico", "Alcohol Etílico sin desnaturalizar (inferior a 80%)", d("632.58")},
	"012": {"012", ISCEspecificoAlcohol, "ISC Específico", "Aguardientes de uva", d("632.58")},
	"013": {"013", ISCEspecificoAlcohol, "ISC Específico", "Whisky", d("632.58")},
	"014": {"014", ISCEspecificoAlcohol, "ISC Específico", "Ron y demás aguardientes de caña", d("632.58")},
	"015": {"015", ISCEspecificoAlcohol, "ISC Específico", "Gin y Ginebra", d("632.58")},
	"016": {"016", ISCEspecificoAlcohol, "ISC Específico", "Vodka", d("632.58")},
	"017": {"017", ISCEspecificoAlcohol, "ISC Específico", "Licores", d("632.58")},
	"018": {"018", ISCEspecificoAlcohol, "ISC Específico", "Los demás (Bebidas y Alcoholes)", d("632.58")},
	"019": {"019", ISCEspecificoCigarrillos, "ISC Específico", "Cigarrillos que contengan tabaco cajetilla 20 unidades", d("53.51")},
	"020": {"020", ISCEspecificoCigarrillos, "ISC Específico", "Los demás Cigarrillos que contengan 20 unidades", d("53.51")},
	"021": {"021", ISCEspecificoCigarrillos, "ISC Específico", "Cigarrillos que contengan 10 unidades", d("26.75")},
	"022": {"022", ISCEspecificoCigarrillos, "ISC Específico", "Los demás Cigarrillos que contengan 10 unidades", d("26.75")},
	"023": {"023", ISCAdValoremAlcohol, "ISC AdValorem", "Cerveza", d("10")},
	"024": {"024", ISCAdValoremAlcohol, "ISC AdValorem", "Vinos de uva", d("10")},
	"025": {"025", ISCAdValoremAlcohol, "ISC AdValorem", "Vermut y demás vinos de uvas frescas", d("10")},
	"026": {"026", ISCAdValoremAlcohol, "ISC AdValorem", "Demás bebidas fermentadas", d("10")},
	"027": {"027", ISCAdValoremAlcohol, "ISC AdValorem", "Alcohol Etílico sin desnaturalizar (Mayor o igual a 80%)", d("10")},
	"028": {"028", ISCAdValoremAlcohol, "ISC AdValorem", "Alcohol Etílico sin desnaturalizar (inferior a 80%)", d("10")},
	"029": {"029", ISCAdValoremAlcohol, "ISC AdValorem", "Aguardientes de uva", d("10")},
	"030": {"030", ISCAdValoremAlcohol, "ISC AdValorem", "Whisky", d("10")},
	"031": {"031", ISCAdValoremAlcohol, "ISC AdValorem", "Ron y demás aguardientes de caña", d("10")},
	"032": {"032", ISCAdValoremAlcohol, "ISC AdValorem", "Gin y Ginebra", d("10")},
	"033": {"033", ISCAdValoremAlcohol, "ISC AdValorem", "Vodka", d("10")},
	"034": {"034", ISCAdValoremAlcohol, "ISC AdValorem", "Licores", d("10")},
	"035": {"035", ISCAdValoremAlcohol, "ISC AdValorem", "Los demás (Bebidas y Alcoholes)", d("10")},
	"036": {"036", ISCAdValoremCigarrillos, "ISC AdValorem", "Cigarrillos que contengan tabaco cajetilla 20 unidades", d("20")},
	"037": {"037", ISCAdValoremCigarrillos, "ISC AdValorem", "Los demás Cigarrillos que contengan 20 unidades", d("20")},
	"038": {"038", ISCAdValoremCigarrillos, "ISC AdValorem", "Cigarrillos que contengan 10 unidades", d("20")},
	"039": {"039", ISCAdValoremCigarrillos, "ISC AdValorem", "Los demás Cigarrillos que contengan 10 unidades", d("20")},
}

// UnidadGranel is the "Granel" (bulk) unit of measure code in Tabla IV.
const UnidadGranel = 18

var (
	one       = decimal.FromInt(1)
	hundred   = decimal.FromInt(100)
	recargo30 = decimal.MustParse("1.30")
)

// pct converts a percentage (10 → 0.10).
func pct(p decimal.Decimal) decimal.Decimal {
	r, _ := p.Div(hundred)
	return r
}

// mul multiplies ignoring overflow errors (inputs are validated ranges).
func mul(a, b decimal.Decimal) decimal.Decimal {
	r, err := a.MulChecked(b)
	if err != nil {
		return decimal.Zero
	}
	return r
}

func div(a, b decimal.Decimal) decimal.Decimal {
	r, err := a.Div(b)
	if err != nil {
		return decimal.Zero
	}
	return r
}

// tasas resolves the rate of every code: the rate supplied in
// Totales.ImpuestosAdicionales takes precedence over the Tabla I default
// (specific ISC amounts change quarterly).
func tasas(e *ECF) map[string]decimal.Decimal {
	m := make(map[string]decimal.Decimal, 8)
	if ia := e.Encabezado.Totales.ImpuestosAdicionales; ia != nil {
		for _, i := range ia.ImpuestoAdicional {
			if i.TasaImpuestoAdicional != nil {
				m[i.TipoImpuesto] = *i.TasaImpuestoAdicional
			}
		}
	}
	return m
}

func tasaDe(over map[string]decimal.Decimal, code string) (decimal.Decimal, bool) {
	if t, ok := over[code]; ok {
		return t, true
	}
	t, ok := TablaImpuestos[code]
	return t.Tasa, ok
}

// itemCodes returns the additional tax codes of an item.
func itemCodes(it *Item) []string {
	if it.TablaImpuestoAdicional == nil {
		return nil
	}
	out := make([]string, 0, len(it.TablaImpuestoAdicional.ImpuestoAdicional))
	for _, c := range it.TablaImpuestoAdicional.ImpuestoAdicional {
		out = append(out, c.TipoImpuesto)
	}
	return out
}

// impuestosCalculados holds the per-code additional tax results.
type impuestosCalculados struct {
	porCodigo map[string]decimal.Decimal // unrounded amount per code
	iscItem   []decimal.Decimal          // ISC (specific + ad valorem) per item, unrounded
	otrosItem []decimal.Decimal          // other additional taxes (001-005) per item, unrounded
	tasa      map[string]decimal.Decimal
}

// computeImpuestos applies Informe Técnico e-CF sections 14 and 15.
func computeImpuestos(e *ECF, incluido, norma1007 bool, globalD, globalR decimal.Decimal) impuestosCalculados {
	over := tasas(e)
	res := impuestosCalculados{
		porCodigo: map[string]decimal.Decimal{},
		iscItem:   make([]decimal.Decimal, len(e.DetallesItems.Item)),
		otrosItem: make([]decimal.Decimal, len(e.DetallesItems.Item)),
		tasa:      map[string]decimal.Decimal{},
	}
	items := e.DetallesItems.Item
	sumItems := decimal.Zero
	for i := range items {
		sumItems = sumItems.Add(items[i].MontoItem)
	}
	// Combined rate of codes 002 and 004 (Norma General 10-07 divisor).
	var r002, r004 decimal.Decimal
	for i := range items {
		for _, c := range itemCodes(&items[i]) {
			switch c {
			case "002":
				r002, _ = tasaDe(over, "002")
			case "004":
				r004, _ = tasaDe(over, "004")
			}
		}
	}
	divisor1007 := one.Add(TasaITBIS1).Add(pct(r002)).Add(pct(r004))

	for i := range items {
		it := &items[i]
		codes := itemCodes(it)
		if len(codes) == 0 {
			continue
		}
		// Specific ISC first: the ad valorem computation depends on it.
		var iscEsp, tasaEspCig decimal.Decimal
		for _, c := range codes {
			info, ok := TablaImpuestos[c]
			if !ok {
				continue
			}
			tasa, _ := tasaDe(over, c)
			res.tasa[c] = tasa
			switch info.Clase {
			case ISCEspecificoAlcohol:
				if it.UnidadMedida == UnidadGranel {
					continue
				}
				amt := mul(mul(mul(mul(tasa, pct(deref(it.GradosAlcohol))), deref(it.CantidadReferencia)), subcantidad(it)), it.CantidadItem)
				iscEsp = iscEsp.Add(amt)
				res.porCodigo[c] = res.porCodigo[c].Add(amt)
			case ISCEspecificoCigarrillos:
				amt := mul(mul(it.CantidadItem, deref(it.CantidadReferencia)), tasa)
				iscEsp = iscEsp.Add(amt)
				tasaEspCig = tasa
				res.porCodigo[c] = res.porCodigo[c].Add(amt)
			}
		}
		res.iscItem[i] = iscEsp
		for _, c := range codes {
			info, ok := TablaImpuestos[c]
			if !ok {
				continue
			}
			tasa, _ := tasaDe(over, c)
			res.tasa[c] = tasa
			rate := pct(tasa)
			cant, cantRef := it.CantidadItem, deref(it.CantidadReferencia)
			var amt decimal.Decimal
			switch info.Clase {
			case ISCAdValoremAlcohol:
				if it.UnidadMedida == UnidadGranel {
					amt = mul(mul(mul(it.PrecioUnitarioItem, recargo30), rate), cant)
				} else {
					pvpSinITBIS := div(deref(it.PrecioUnitarioReferencia), one.Add(TasaITBIS1))
					unitEsp := decimal.Zero
					if q := mul(cant, cantRef); !q.IsZero() {
						unitEsp = div(iscEsp, q)
					}
					base := div(pvpSinITBIS.Sub(unitEsp), one.Add(rate))
					amt = mul(mul(mul(base, cant), cantRef), rate)
				}
			case ISCAdValoremCigarrillos:
				pvpSinITBIS := div(deref(it.PrecioUnitarioReferencia), one.Add(TasaITBIS1))
				base := div(pvpSinITBIS.Sub(tasaEspCig), one.Add(rate))
				amt = mul(mul(mul(base, cant), cantRef), rate)
			case OtroImpuesto:
				// Apportion global discounts/surcharges by the line's share
				// of the total item amount (Norma 10-07 discounts excluded).
				adj := it.MontoItem
				if !sumItems.IsZero() {
					share := div(it.MontoItem, sumItems)
					adj = adj.Sub(mul(share, globalD)).Add(mul(share, globalR))
				}
				switch {
				case norma1007 && (c == "002" || c == "004") && it.IndicadorFacturacion == ITBISTasa1:
					adj = div(it.MontoItem, divisor1007)
				case incluido && it.IndicadorFacturacion == ITBISTasa1:
					adj = div(adj, one.Add(TasaITBIS1))
				case incluido && it.IndicadorFacturacion == ITBISTasa2:
					adj = div(adj, one.Add(TasaITBIS2))
				}
				amt = mul(adj, rate)
			default:
				continue
			}
			if info.EsISC() {
				res.iscItem[i] = res.iscItem[i].Add(amt)
			} else {
				res.otrosItem[i] = res.otrosItem[i].Add(amt)
			}
			res.porCodigo[c] = res.porCodigo[c].Add(amt)
		}
	}
	return res
}

func subcantidad(it *Item) decimal.Decimal {
	if t := it.TablaSubcantidad; t != nil && len(t.SubcantidadItem) > 0 {
		return t.SubcantidadItem[0].Subcantidad
	}
	return decimal.Zero
}

func deref(p *decimal.Decimal) decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	return *p
}

// totalesImpuestos converts per-code results into the Totales table, sorted
// by code.
func (r impuestosCalculados) totales() ([]ImpuestoAdicionalTotal, decimal.Decimal) {
	codes := make([]string, 0, len(r.porCodigo))
	for c := range r.porCodigo {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	var out []ImpuestoAdicionalTotal
	total := decimal.Zero
	for _, c := range codes {
		amt := r.porCodigo[c].Round(2)
		if amt.Sign() <= 0 {
			continue
		}
		total = total.Add(amt)
		tasa := r.tasa[c]
		entry := ImpuestoAdicionalTotal{TipoImpuesto: c, TasaImpuestoAdicional: decPtr(tasa)}
		switch TablaImpuestos[c].Clase {
		case OtroImpuesto:
			entry.OtrosImpuestosAdicionales = decPtr(amt)
		case ISCEspecificoAlcohol, ISCEspecificoCigarrillos:
			entry.MontoImpuestoSelectivoConsumoEspecifico = decPtr(amt)
		default:
			entry.MontoImpuestoSelectivoConsumoAdvalorem = decPtr(amt)
		}
		out = append(out, entry)
	}
	return out, total
}

// impuestoAdicionalMonto returns the amount of a totals entry.
func impuestoAdicionalMonto(i ImpuestoAdicionalTotal) decimal.Decimal {
	return deref(i.MontoImpuestoSelectivoConsumoEspecifico).
		Add(deref(i.MontoImpuestoSelectivoConsumoAdvalorem)).
		Add(deref(i.OtrosImpuestosAdicionales))
}

// String implements fmt.Stringer for diagnostics.
func (c ClaseImpuesto) String() string {
	switch c {
	case OtroImpuesto:
		return "otro impuesto adicional"
	case ISCEspecificoAlcohol:
		return "ISC específico alcoholes"
	case ISCEspecificoCigarrillos:
		return "ISC específico cigarrillos"
	case ISCAdValoremAlcohol:
		return "ISC ad valorem alcoholes"
	case ISCAdValoremCigarrillos:
		return "ISC ad valorem cigarrillos"
	}
	return fmt.Sprintf("clase(%d)", int(c))
}
