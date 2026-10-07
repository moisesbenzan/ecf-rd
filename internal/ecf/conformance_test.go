package ecf

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

func dp(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }

func baseDoc(tipo TipoECF, encf string) *ECF {
	return &ECF{
		Encabezado: Encabezado{
			IdDoc: IdDoc{TipoeCF: tipo, ENCF: encf},
			Emisor: Emisor{
				RNCEmisor: "131880738", RazonSocialEmisor: "Comercial Ejemplo, S.R.L.",
				DireccionEmisor: "Av. 27 de Febrero No. 100", Municipio: "010101", Provincia: "010000",
				FechaEmision: "01-10-2026",
			},
		},
		FechaHoraFirma: "01-10-2026 10:00:00",
	}
}

func item(ind IndicadorFacturacion, bs int, nombre, cant, precio string) Item {
	return Item{IndicadorFacturacion: ind, NombreItem: nombre, IndicadorBienoServicio: bs,
		CantidadItem: decimal.MustParse(cant), PrecioUnitarioItem: decimal.MustParse(precio), UnidadMedida: 43}
}

// sampleDocs returns one valid document per type plus feature scenarios.
func sampleDocs() map[string]*ECF {
	docs := map[string]*ECF{}

	e31 := baseDoc(31, "E310000000001")
	e31.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e31.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	e31.Encabezado.IdDoc.TipoIngresos, e31.Encabezado.IdDoc.TipoPago = "01", 1
	e31.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Cliente, S.A."}
	e31.DetallesItems.Item = []Item{item(1, 2, "Servicio", "10", "350"), item(4, 1, "Libro", "2", "250")}
	docs["31"] = e31

	e32 := baseDoc(32, "E320000000001")
	e32.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(1)
	e32.Encabezado.IdDoc.TipoIngresos, e32.Encabezado.IdDoc.TipoPago = "01", 1
	e32.DetallesItems.Item = []Item{item(1, 1, "Combo", "2", "590")}
	docs["32"] = e32

	e33 := baseDoc(33, "E330000000001")
	e33.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e33.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	e33.Encabezado.IdDoc.TipoIngresos, e33.Encabezado.IdDoc.TipoPago = "01", 1
	e33.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Cliente, S.A."}
	e33.DetallesItems.Item = []Item{item(1, 2, "Ajuste de intereses", "1", "100")}
	e33.InformacionReferencia = &InformacionReferencia{NCFModificado: "E310000000001", FechaNCFModificado: "15-09-2026", CodigoModificacion: 3, RazonModificacion: "Intereses por mora"}
	docs["33"] = e33

	e34 := baseDoc(34, "E340000000001")
	e34.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	e34.Encabezado.IdDoc.TipoIngresos, e34.Encabezado.IdDoc.TipoPago = "01", 1
	e34.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Cliente, S.A."}
	e34.DetallesItems.Item = []Item{item(1, 2, "Devolución", "1", "350")}
	e34.InformacionReferencia = &InformacionReferencia{NCFModificado: "E310000000001", FechaNCFModificado: "15-07-2026", CodigoModificacion: 3, RazonModificacion: "Devolución parcial"}
	docs["34"] = e34

	e41 := baseDoc(41, "E410000000001")
	e41.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e41.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	e41.Encabezado.IdDoc.TipoPago = 1
	e41.Encabezado.Comprador = &Comprador{RNCComprador: "00113918205", RazonSocialComprador: "Juan Pérez"}
	it41 := item(1, 2, "Servicio de transporte", "1", "1000")
	it41.Retencion = &Retencion{IndicadorAgenteRetencionoPercepcion: 1, MontoITBISRetenido: dp("180.00"), MontoISRRetenido: dp("100.00")}
	e41.DetallesItems.Item = []Item{it41}
	docs["41"] = e41

	e43 := baseDoc(43, "E430000000001")
	e43.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e43.DetallesItems.Item = []Item{item(4, 2, "Peaje", "1", "100")}
	docs["43"] = e43

	e44 := baseDoc(44, "E440000000001")
	e44.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e44.Encabezado.IdDoc.TipoIngresos, e44.Encabezado.IdDoc.TipoPago = "01", 2
	e44.Encabezado.IdDoc.FechaLimitePago = "31-10-2026"
	e44.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Zona Franca, S.A."}
	e44.DetallesItems.Item = []Item{item(4, 1, "Materia prima", "5", "1000")}
	docs["44"] = e44

	e45 := baseDoc(45, "E450000000001")
	e45.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e45.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	e45.Encabezado.IdDoc.TipoIngresos, e45.Encabezado.IdDoc.TipoPago = "01", 1
	e45.Encabezado.Comprador = &Comprador{RNCComprador: "401007551", RazonSocialComprador: "Ministerio de Ejemplo"}
	e45.DetallesItems.Item = []Item{item(1, 1, "Papel", "100", "250")}
	docs["45"] = e45

	e46 := baseDoc(46, "E460000000001")
	e46.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e46.Encabezado.IdDoc.TipoIngresos, e46.Encabezado.IdDoc.TipoPago = "01", 1
	e46.Encabezado.Comprador = &Comprador{IdentificadorExtranjero: "US123456", RazonSocialComprador: "Importer LLC", PaisComprador: "ESTADOS UNIDOS"}
	e46.Encabezado.OtraMoneda = &OtraMoneda{TipoMoneda: "USD", TipoCambio: decimal.MustParse("60.2500")}
	it46 := item(3, 1, "Cacao", "1000", "250")
	e46.DetallesItems.Item = []Item{it46}
	docs["46"] = e46

	e47 := baseDoc(47, "E470000000001")
	e47.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	e47.Encabezado.Comprador = &Comprador{IdentificadorExtranjero: "ES-B12345", RazonSocialComprador: "Consultora Europea"}
	it47 := item(4, 2, "Licencia de software", "1", "10000")
	it47.Retencion = &Retencion{IndicadorAgenteRetencionoPercepcion: 1, MontoISRRetenido: dp("2700.00")}
	e47.DetallesItems.Item = []Item{it47}
	docs["47"] = e47

	// ISC alcohol (Informe Técnico example) + propina legal + global
	// discount, priced without ITBIS.
	isc := baseDoc(31, "E310000000002")
	isc.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	isc.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	isc.Encabezado.IdDoc.TipoIngresos, isc.Encabezado.IdDoc.TipoPago = "01", 1
	isc.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Colmado, S.R.L."}
	ron := item(1, 1, "RON 16/700 ML", "1", "1063.97")
	ron.UnidadMedida = 6
	ron.CantidadReferencia, ron.UnidadReferencia = dp("16"), 5
	ron.TablaSubcantidad = &TablaSubcantidad{SubcantidadItem: []SubcantidadItem{{Subcantidad: decimal.MustParse("0.65"), CodigoSubcantidad: 24}}}
	ron.GradosAlcohol = dp("4.30")
	ron.PrecioUnitarioReferencia = dp("80.00")
	ron.TablaImpuestoAdicional = &TablaImpuestoAdicional{ImpuestoAdicional: []ImpuestoAdicionalItem{{"014"}, {"031"}}}
	servicio := item(1, 2, "Servicio de mesa", "1", "500")
	servicio.TablaImpuestoAdicional = &TablaImpuestoAdicional{ImpuestoAdicional: []ImpuestoAdicionalItem{{"001"}}}
	isc.DetallesItems.Item = []Item{ron, servicio}
	isc.Encabezado.Totales.ImpuestosAdicionales = &ImpuestosAdicionales{ImpuestoAdicional: []ImpuestoAdicionalTotal{{TipoImpuesto: "014", TasaImpuestoAdicional: dp("617.93")}}}
	isc.DescuentosORecargos = &DescuentosORecargos{DescuentoORecargo: []DescuentoORecargo{{
		TipoAjuste: "D", TipoValor: "%", ValorDescuentooRecargo: dp("5"), DescripcionDescuentooRecargo: "Pronto pago",
		IndicadorFacturacionDescuentooRecargo: intPtr(1),
	}}}
	docs["isc"] = isc

	// Pagination over 5 lines, 2 per page.
	pag := baseDoc(31, "E310000000003")
	pag.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	pag.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	pag.Encabezado.IdDoc.TipoIngresos, pag.Encabezado.IdDoc.TipoPago = "01", 1
	pag.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Cliente, S.A."}
	for i := 0; i < 5; i++ {
		ind := IndicadorFacturacion(1)
		if i%2 == 1 {
			ind = 4
		}
		pag.DetallesItems.Item = append(pag.DetallesItems.Item, item(ind, 1, "Artículo", "3", "33.33"))
	}
	Paginar(pag, 2)
	docs["paginacion"] = pag

	// Foreign currency with line detail.
	usd := baseDoc(31, "E310000000004")
	usd.Encabezado.IdDoc.FechaVencimientoSecuencia = "31-12-2028"
	usd.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	usd.Encabezado.IdDoc.TipoIngresos, usd.Encabezado.IdDoc.TipoPago = "01", 1
	usd.Encabezado.Comprador = &Comprador{RNCComprador: "101672919", RazonSocialComprador: "Cliente, S.A."}
	usd.Encabezado.OtraMoneda = &OtraMoneda{TipoMoneda: "USD", TipoCambio: decimal.MustParse("60.0000")}
	ui := item(1, 2, "Consultoría", "2", "6000")
	ui.OtraMonedaDetalle = &OtraMonedaDetalle{PrecioOtraMoneda: decimal.MustParse("100")}
	usd.DetallesItems.Item = []Item{ui}
	docs["otramoneda"] = usd
	return docs
}

func TestConformanceAllTypes(t *testing.T) {
	xmllint, _ := exec.LookPath("xmllint")
	schemaDir := ""
	if xmllint != "" {
		schemaDir = patchedSchemas(t)
	}
	for name, e := range sampleDocs() {
		t.Run(name, func(t *testing.T) {
			if err := Complete(e); err != nil {
				t.Fatal(err)
			}
			issues := Validate(e)
			if issues.HasErrors() {
				t.Fatalf("validation errors: %s", issues.Error())
			}
			b, err := Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			if xmllint == "" {
				return
			}
			// The ECF schemas require the trailing signature; add a
			// placeholder element for the structural cross-check.
			signedLike := strings.Replace(string(b), "</ECF>", "<Signature/></ECF>", 1)
			f := filepath.Join(t.TempDir(), "doc.xml")
			_ = os.WriteFile(f, []byte(signedLike), 0o600)
			xsdFile := filepath.Join(schemaDir, "ECF-"+e.Encabezado.IdDoc.TipoeCF.String()+".xsd")
			out, err := exec.Command(xmllint, "--noout", "--schema", xsdFile, f).CombinedOutput()
			if err != nil {
				t.Fatalf("xmllint rejected %s:\n%s\n%s", name, out, b)
			}
		})
	}
}

// patchedSchemas copies the official XSDs fixing two defects that strict
// processors such as libxml2 reject (DGII's .NET stack tolerates them): a
// type name with a leading space in ECF-31 and the non-XSD "(?:" group in
// RFCE-32.
func patchedSchemas(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	src := "../xsd/schemas"
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, en := range entries {
		b, _ := os.ReadFile(filepath.Join(src, en.Name()))
		s := strings.ReplaceAll(string(b), `name=" IndicadorServicioTodoIncluidoType"`, `name="IndicadorServicioTodoIncluidoType"`)
		s = strings.ReplaceAll(s, "(?:", "(")
		_ = os.WriteFile(filepath.Join(dir, en.Name()), []byte(s), 0o600)
	}
	return dir
}

func TestISCInformeTecnicoExample(t *testing.T) {
	e := sampleDocs()["isc"]
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	byCode := map[string]ImpuestoAdicionalTotal{}
	for _, i := range e.Encabezado.Totales.ImpuestosAdicionales.ImpuestoAdicional {
		byCode[i.TipoImpuesto] = i
	}
	// Informe Técnico §14: 617.93 × 4.30% × 16 × 0.65 × 1 = 276.34
	if got := byCode["014"].MontoImpuestoSelectivoConsumoEspecifico.String(); got != "276.34" {
		t.Errorf("ISC específico = %s, want 276.34", got)
	}
	// Ad valorem ≈ 73.49 (DGII rounds intermediate steps; tolerance ±1).
	adv := *byCode["031"].MontoImpuestoSelectivoConsumoAdvalorem
	if adv.Sub(decimal.MustParse("73.49")).Abs().Cmp(decimal.FromInt(1)) > 0 {
		t.Errorf("ISC ad valorem = %s, want ≈ 73.49", adv)
	}
	// Propina legal 10% over the service line net of the 5% global discount
	// apportioned by line share.
	if byCode["001"].OtrosImpuestosAdicionales == nil {
		t.Fatal("missing propina")
	}
	// ITBIS1 base includes ISC.
	tot := e.Encabezado.Totales
	want := mul(tot.MontoGravadoI1.Add(byCode["014"].MontoImpuestoSelectivoConsumoEspecifico.Add(adv)), TasaITBIS1).Round(2)
	if tot.TotalITBIS1.Sub(want).Abs().Cmp(decimal.MustParse("0.02")) > 0 {
		t.Errorf("TotalITBIS1 = %s, want ≈ %s", tot.TotalITBIS1, want)
	}
}

func TestOtrosImpuestosNorma1007(t *testing.T) {
	// Informe Técnico §15: CDT with IndicadorMontoGravado=1 and Norma 10-07:
	// (500 / (1 + 0.18 + 0.02 + 0.10)) × 0.02 = 7.69
	e := baseDoc(31, "E310000000005")
	e.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(1)
	it := item(1, 2, "Servicio telecom", "1", "500")
	it.TablaImpuestoAdicional = &TablaImpuestoAdicional{ImpuestoAdicional: []ImpuestoAdicionalItem{{"002"}, {"004"}}}
	e.DetallesItems.Item = []Item{it}
	e.DescuentosORecargos = &DescuentosORecargos{DescuentoORecargo: []DescuentoORecargo{{
		TipoAjuste: "D", TipoValor: "$", MontoDescuentooRecargo: decimal.MustParse("100"), IndicadorNorma1007: intPtr(1),
		IndicadorFacturacionDescuentooRecargo: intPtr(1),
	}}}
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	for _, i := range e.Encabezado.Totales.ImpuestosAdicionales.ImpuestoAdicional {
		if i.TipoImpuesto == "002" && i.OtrosImpuestosAdicionales.String() != "7.69" {
			t.Errorf("CDT = %s, want 7.69", i.OtrosImpuestosAdicionales)
		}
	}
}

func TestOtrosImpuestosDescuentoGlobal(t *testing.T) {
	// Informe Técnico §15 example: lines 200 (002), 300 (002), 130; global
	// discount 100 → (500 − 79.37) × 0.02 = 8.41
	e := baseDoc(31, "E310000000006")
	e.Encabezado.IdDoc.IndicadorMontoGravado = intPtr(0)
	a := item(1, 2, "A", "1", "200")
	a.TablaImpuestoAdicional = &TablaImpuestoAdicional{ImpuestoAdicional: []ImpuestoAdicionalItem{{"002"}}}
	b := item(1, 2, "B", "1", "300")
	b.TablaImpuestoAdicional = &TablaImpuestoAdicional{ImpuestoAdicional: []ImpuestoAdicionalItem{{"002"}}}
	c := item(1, 2, "C", "1", "130")
	e.DetallesItems.Item = []Item{a, b, c}
	e.DescuentosORecargos = &DescuentosORecargos{DescuentoORecargo: []DescuentoORecargo{{
		TipoAjuste: "D", TipoValor: "$", MontoDescuentooRecargo: decimal.MustParse("100"), IndicadorFacturacionDescuentooRecargo: intPtr(1),
	}}}
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	got := e.Encabezado.Totales.ImpuestosAdicionales.ImpuestoAdicional[0].OtrosImpuestosAdicionales.String()
	if got != "8.41" {
		t.Errorf("CDT = %s, want 8.41", got)
	}
}

func TestPaginacionTotals(t *testing.T) {
	e := sampleDocs()["paginacion"]
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	if e.Encabezado.IdDoc.TotalPaginas != 3 {
		t.Fatalf("pages = %d", e.Encabezado.IdDoc.TotalPaginas)
	}
	sum := decimal.Zero
	for _, p := range e.Paginacion.Pagina {
		sum = sum.Add(*p.MontoSubtotalPagina)
	}
	if sum.Sub(e.Encabezado.Totales.MontoTotal).Abs().Cmp(decimal.FromInt(5)) > 0 {
		t.Fatalf("Σ pages %s vs total %s", sum, e.Encabezado.Totales.MontoTotal)
	}
	// Break coverage.
	e.Paginacion.Pagina[1].NoLineaDesde = 4
	if !Validate(e).HasErrors() {
		t.Fatal("non-contiguous pagination must fail")
	}
}

func TestToleranceRule(t *testing.T) {
	e := sampleDocs()["31"]
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	// Informe Técnico §12: a 0.99 line difference is within tolerance.
	e.DetallesItems.Item[0].MontoItem = e.DetallesItems.Item[0].MontoItem.Add(decimal.MustParse("0.99"))
	e.Encabezado.Totales.MontoGravadoI1 = decPtr(e.DetallesItems.Item[0].MontoItem)
	e.Encabezado.Totales.MontoGravadoTotal = decPtr(e.DetallesItems.Item[0].MontoItem)
	if issues := Validate(e); issues.HasErrors() {
		t.Fatalf("within tolerance must pass: %s", issues.Error())
	}
	e.DetallesItems.Item[0].MontoItem = e.DetallesItems.Item[0].MontoItem.Add(decimal.MustParse("5"))
	if !Validate(e).HasErrors() {
		t.Fatal("beyond tolerance must fail in strict mode")
	}
	if ValidateWith(e, Options{}).HasErrors() {
		t.Fatal("beyond tolerance must only warn in non-strict mode (aceptado condicional)")
	}
}

func TestSchemaCatchesTypeSpecificFields(t *testing.T) {
	e := sampleDocs()["43"]
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	// Type 43 has no buyer section in its XSD.
	e.Encabezado.Comprador = &Comprador{RazonSocialComprador: "X"}
	found := false
	for _, i := range SchemaIssues(e) {
		if strings.Contains(i.Field, "Comprador") {
			found = true
		}
	}
	if !found {
		t.Fatalf("XSD must reject Comprador in type 43")
	}
}

func xmllintCheck(t *testing.T, schema string, doc []byte, signed bool) {
	t.Helper()
	xmllint, err := exec.LookPath("xmllint")
	if err != nil {
		return
	}
	dir := patchedSchemas(t)
	s := string(doc)
	if signed {
		// Wildcard placeholder for the signature.
		i := strings.LastIndex(s, "</")
		s = s[:i] + "<Signature/>" + s[i:]
	}
	f := filepath.Join(t.TempDir(), "doc.xml")
	_ = os.WriteFile(f, []byte(s), 0o600)
	out, err := exec.Command(xmllint, "--noout", "--schema", filepath.Join(dir, schema+".xsd"), f).CombinedOutput()
	if err != nil {
		t.Fatalf("xmllint rejected:\n%s\n%s", out, s)
	}
}

func TestAuxiliaryFormats(t *testing.T) {
	e := sampleDocs()["32"]
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	rfce := NewRFCE(e, "Ab3+9z")
	if is := ValidateRFCE(rfce); is.HasErrors() {
		t.Fatalf("RFCE: %s", is.Error())
	}
	b, _ := Marshal(rfce)
	xmllintCheck(t, "RFCE-32", b, true)

	arecf := &ARECF{DetalleAcusedeRecibo: DetalleAcusedeRecibo{Version: Version, RNCEmisor: "131880738", RNCComprador: "101672919",
		ENCF: "E310000000001", Estado: AcuseNoRecibido, CodigoMotivoNoRecibido: MotivoEnvioDuplicado, FechaHoraAcuseRecibo: "01-10-2026 10:00:00"}}
	if is := ValidateARECF(arecf); is.HasErrors() {
		t.Fatalf("ARECF: %s", is.Error())
	}
	b, _ = Marshal(arecf)
	xmllintCheck(t, "ARECF", b, false)
	arecf.DetalleAcusedeRecibo.CodigoMotivoNoRecibido = 0
	if !ValidateARECF(arecf).HasErrors() {
		t.Fatal("ARECF no recibido without motivo must fail")
	}

	ref := sampleDocs()["31"]
	_ = Complete(ref)
	acecf := &ACECF{DetalleAprobacionComercial: DetalleAprobacionComercial{Version: Version, RNCEmisor: "131880738", ENCF: "E310000000001",
		FechaEmision: "01-10-2026", MontoTotal: ref.Encabezado.Totales.MontoTotal, RNCComprador: "101672919",
		Estado: AprobacionAceptado, FechaHoraAprobacionComercial: "02-10-2026 09:00:00"}}
	if is := ValidateACECF(acecf, ref); is.HasErrors() {
		t.Fatalf("ACECF: %s", is.Error())
	}
	b, _ = Marshal(acecf)
	xmllintCheck(t, "ACECF", b, false)
	acecf.DetalleAprobacionComercial.MontoTotal = decimal.FromInt(1)
	if !ValidateACECF(acecf, ref).HasErrors() {
		t.Fatal("ACECF with mismatching total must fail")
	}

	anecf := &ANECF{Encabezado: ANECFEncabezado{RncEmisor: "131880738", FechaHoraAnulacioneNCF: "01-10-2026 08:50:15"},
		DetalleAnulacion: DetalleAnulacion{Anulacion: []Anulacion{
			{TipoeCF: 31, TablaRangoSecuenciasAnuladaseNCF: TablaRangoSecuenciasAnuladaseNCF{Secuencias: []Secuencias{
				{"E310000000001", "E310000000001"}, {"E310000000005", "E310000000050"}}}},
			{TipoeCF: 44, TablaRangoSecuenciasAnuladaseNCF: TablaRangoSecuenciasAnuladaseNCF{Secuencias: []Secuencias{
				{"E440000000010", "E440000000046"}}}},
		}}}
	if err := anecf.Fill(); err != nil {
		t.Fatal(err)
	}
	// Formato Anulación, Anexo I: 47 + 37 = 84.
	if anecf.Encabezado.CantidadeNCFAnulados != 84 {
		t.Fatalf("count = %d, want 84", anecf.Encabezado.CantidadeNCFAnulados)
	}
	if is := ValidateANECF(anecf); is.HasErrors() {
		t.Fatalf("ANECF: %s", is.Error())
	}
	b, _ = Marshal(anecf)
	xmllintCheck(t, "ANECF", b, true)
}

func TestExamplesAreValid(t *testing.T) {
	files, _ := filepath.Glob("../../examples/*.json")
	if len(files) < 3 {
		t.Fatalf("examples missing")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		var e ECF
		dec := json.NewDecoder(bytes.NewReader(b))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&e); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if err := Complete(&e); err != nil {
			t.Fatal(err)
		}
		e.FechaHoraFirma = "01-10-2026 10:00:00"
		if is := Validate(&e); is.HasErrors() {
			t.Fatalf("%s: %s", f, is.Error())
		}
	}
}
