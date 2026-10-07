package ecf

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

func load(t *testing.T, name string) *ECF {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var e ECF
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		t.Fatal(err)
	}
	return &e
}

func mustValid(t *testing.T, e *ECF) {
	t.Helper()
	if issues := Validate(e); issues.HasErrors() {
		t.Fatalf("unexpected validation errors: %v", issues.Error())
	}
}

func TestCompleteFactura31(t *testing.T) {
	e := load(t, "factura-credito-fiscal-31.json")
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	mustValid(t, e)
	tot := e.Encabezado.Totales
	want := map[string]string{
		"MontoGravadoI1":    "3500.00",
		"MontoGravadoTotal": "3500.00",
		"TotalITBIS1":       "630.00",
		"TotalITBIS":        "630.00",
		"MontoExento":       "472.00",
		"MontoTotal":        "4602.00",
	}
	got := map[string]string{
		"MontoGravadoI1":    tot.MontoGravadoI1.String(),
		"MontoGravadoTotal": tot.MontoGravadoTotal.String(),
		"TotalITBIS1":       tot.TotalITBIS1.String(),
		"TotalITBIS":        tot.TotalITBIS.String(),
		"MontoExento":       tot.MontoExento.String(),
		"MontoTotal":        tot.MontoTotal.String(),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %s, want %s", k, got[k], v)
		}
	}
	if *tot.ITBIS1 != 18 {
		t.Errorf("ITBIS1 = %d", *tot.ITBIS1)
	}
	if e.DetallesItems.Item[1].MontoItem.String() != "472.00" || e.DetallesItems.Item[1].NumeroLinea != 2 {
		t.Errorf("line 2 not completed: %+v", e.DetallesItems.Item[1])
	}
}

func TestCompleteConsumoMontoIncluido(t *testing.T) {
	e := load(t, "factura-consumo-32.json")
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	mustValid(t, e)
	tot := e.Encabezado.Totales
	if tot.MontoGravadoI1.String() != "1000.00" || tot.TotalITBIS1.String() != "180.00" || tot.MontoTotal.String() != "1180.00" {
		t.Fatalf("totals: base %s itbis %s total %s", tot.MontoGravadoI1, tot.TotalITBIS1, tot.MontoTotal)
	}
}

func TestMarshalOrderAndOmitEmpty(t *testing.T) {
	e := load(t, "factura-credito-fiscal-31.json")
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	e.FechaHoraFirma = "01-10-2026 10:15:00"
	b, err := Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, `<?xml version="1.0" encoding="utf-8"?><ECF><Encabezado><Version>1.0</Version><IdDoc><TipoeCF>31</TipoeCF><eNCF>E310000000001</eNCF>`) {
		t.Fatalf("unexpected start: %s", s[:200])
	}
	// Elements must follow the XSD sequence.
	order := []string{"<Encabezado>", "<IdDoc>", "<Emisor>", "<Comprador>", "<Totales>", "<MontoGravadoTotal>",
		"<MontoGravadoI1>", "<MontoExento>", "<ITBIS1>", "<TotalITBIS>", "<TotalITBIS1>", "<MontoTotal>",
		"</Encabezado>", "<DetallesItems>", "<NumeroLinea>1</NumeroLinea>", "<IndicadorFacturacion>1",
		"<NombreItem>", "<CantidadItem>10.00</CantidadItem>", "<PrecioUnitarioItem>350.00", "<MontoItem>3500.00",
		"</DetallesItems>", "<FechaHoraFirma>"}
	pos := 0
	for _, tag := range order {
		i := strings.Index(s[pos:], tag)
		if i < 0 {
			t.Fatalf("missing or out of order %q in\n%s", tag, s)
		}
		pos += i
	}
	for _, absent := range []string{"<Subtotales", "<Paginacion", "<InformacionReferencia", "<Transporte", "<OtraMoneda"} {
		if strings.Contains(s, absent) {
			t.Errorf("unexpected %s", absent)
		}
	}
	if m := regexp.MustCompile(`<(\w+)(\s[^>]*)?></(\w+)>|<\w+/>`).FindString(s); m != "" {
		t.Errorf("empty element found: %s", m)
	}
	back, err := Unmarshal(b)
	if err != nil {
		t.Fatal(err)
	}
	if !back.Encabezado.Totales.MontoTotal.Equal(e.Encabezado.Totales.MontoTotal) || back.Encabezado.IdDoc.ENCF != "E310000000001" {
		t.Fatalf("round trip mismatch")
	}
	mustValid(t, back)
}

func TestValidationRules(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(e *ECF)
		field  string
	}{
		{"encf type mismatch", func(e *ECF) { e.Encabezado.IdDoc.ENCF = "E320000000001" }, "IdDoc.eNCF"},
		{"bad encf", func(e *ECF) { e.Encabezado.IdDoc.ENCF = "B0100000001" }, "IdDoc.eNCF"},
		{"missing vencimiento", func(e *ECF) { e.Encabezado.IdDoc.FechaVencimientoSecuencia = "" }, "IdDoc.FechaVencimientoSecuencia"},
		{"credit without due date", func(e *ECF) { e.Encabezado.IdDoc.FechaLimitePago = "" }, "IdDoc.FechaLimitePago"},
		{"bad date", func(e *ECF) { e.Encabezado.Emisor.FechaEmision = "2026-10-31" }, "Emisor.FechaEmision"},
		{"bad rnc", func(e *ECF) { e.Encabezado.Emisor.RNCEmisor = "12345" }, "Emisor.RNCEmisor"},
		{"missing comprador rnc", func(e *ECF) { e.Encabezado.Comprador.RNCComprador = "" }, "Comprador.RNCComprador"},
		{"wrong total", func(e *ECF) { e.Encabezado.Totales.MontoTotal = decimal.MustParse("1.00") }, "Totales.MontoTotal"},
		{"wrong line", func(e *ECF) { e.DetallesItems.Item[0].MontoItem = decimal.MustParse("1.00") }, "DetallesItems.Item[0].MontoItem"},
		{"three decimals", func(e *ECF) {
			d := decimal.MustParse("1.005")
			e.DetallesItems.Item[1].DescuentoMonto = &d
		}, "DetallesItems.Item[1].DescuentoMonto"},
		{"nota credito indicator on 31", func(e *ECF) { e.Encabezado.IdDoc.IndicadorNotaCredito = intPtr(0) }, "IdDoc.IndicadorNotaCredito"},
		{"forma pago 5 on 31", func(e *ECF) { e.Encabezado.IdDoc.TablaFormasPago.FormaDePago[0].FormaPago = 5 }, "IdDoc.TablaFormasPago.FormaDePago[0].FormaPago"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := load(t, "factura-credito-fiscal-31.json")
			if err := Complete(e); err != nil {
				t.Fatal(err)
			}
			c.mutate(e)
			issues := Validate(e)
			for _, i := range issues {
				if i.Field == c.field && i.Severity == SeverityError {
					return
				}
			}
			t.Fatalf("expected error on %s, got %+v", c.field, issues)
		})
	}
}

func TestNotaCredito(t *testing.T) {
	e := load(t, "factura-credito-fiscal-31.json")
	e.Encabezado.IdDoc.TipoeCF = NotaCredito
	e.Encabezado.IdDoc.ENCF = "E340000000001"
	e.Encabezado.IdDoc.FechaVencimientoSecuencia = ""
	e.Encabezado.IdDoc.TablaFormasPago = nil
	e.InformacionReferencia = &InformacionReferencia{
		NCFModificado:      "E310000000001",
		FechaNCFModificado: "01-08-2026",
		CodigoModificacion: 3,
		RazonModificacion:  "Corrección de precio",
	}
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	mustValid(t, e)
	if e.Encabezado.IdDoc.IndicadorNotaCredito == nil || *e.Encabezado.IdDoc.IndicadorNotaCredito != 1 {
		t.Fatalf("IndicadorNotaCredito should be 1 (> 30 days)")
	}
	e.InformacionReferencia = nil
	if !Validate(e).HasErrors() {
		t.Fatal("type 34 without InformacionReferencia must fail")
	}
}

func TestConsumoUmbralRequiresBuyer(t *testing.T) {
	e := load(t, "factura-consumo-32.json")
	e.DetallesItems.Item[0].PrecioUnitarioItem = decimal.MustParse("150000")
	e.Encabezado.IdDoc.TablaFormasPago = nil
	if err := Complete(e); err != nil {
		t.Fatal(err)
	}
	issues := Validate(e)
	found := false
	for _, i := range issues {
		if i.Field == "Comprador.RNCComprador" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected buyer requirement above threshold, got %+v", issues)
	}
}

func TestENCFAndRNC(t *testing.T) {
	n, err := ParseENCF("E310000000123")
	if err != nil || n.Tipo != 31 || n.Secuencia != 123 || n.String() != "E310000000123" {
		t.Fatalf("parse: %+v %v", n, err)
	}
	if _, err := ParseENCF("E990000000001"); err == nil {
		t.Fatal("type 99 must fail")
	}
	if !ValidRNCCheckDigit("131880738") || !ValidRNCCheckDigit("101672919") {
		t.Error("known valid RNCs rejected")
	}
	if ValidRNCCheckDigit("131880739") {
		t.Error("invalid RNC accepted")
	}
	if !ValidRNCCheckDigit("00113918205") {
		t.Error("valid cédula rejected")
	}
}

func TestANECFFill(t *testing.T) {
	a := &ANECF{
		Encabezado: ANECFEncabezado{RncEmisor: "131880738", FechaHoraAnulacioneNCF: "31-10-2026 10:00:00"},
		DetalleAnulacion: DetalleAnulacion{Anulacion: []Anulacion{{
			TipoeCF: FacturaCreditoFiscal,
			TablaRangoSecuenciasAnuladaseNCF: TablaRangoSecuenciasAnuladaseNCF{Secuencias: []Secuencias{
				{SecuenciaeNCFDesde: "E310000000010", SecuenciaeNCFHasta: "E310000000019"},
				{SecuenciaeNCFDesde: "E310000000050", SecuenciaeNCFHasta: "E310000000050"},
			}},
		}}},
	}
	if err := a.Fill(); err != nil {
		t.Fatal(err)
	}
	if a.Encabezado.CantidadeNCFAnulados != 11 || a.DetalleAnulacion.Anulacion[0].NoLinea != 1 {
		t.Fatalf("fill: %+v", a)
	}
	a.DetalleAnulacion.Anulacion[0].TablaRangoSecuenciasAnuladaseNCF.Secuencias[0].SecuenciaeNCFHasta = "E310000000001"
	if err := a.Fill(); err == nil {
		t.Fatal("inverted range must fail")
	}
}

func TestTimbreURL(t *testing.T) {
	e := load(t, "factura-credito-fiscal-31.json")
	_ = Complete(e)
	e.FechaHoraFirma = "01-10-2026 10:15:00"
	u := TimbreURL("https://ecf.dgii.gov.do/testecf/consultatimbre", e, "a+b/c.")
	want := "https://ecf.dgii.gov.do/testecf/consultatimbre?rncemisor=131880738&rnccomprador=101672919&encf=E310000000001&fechaemision=01-10-2026&montototal=4602.00&fechafirma=01-10-2026%2010:15:00&codigoseguridad=a%2Bb%2Fc%2E"
	if u != want {
		t.Fatalf("got  %s\nwant %s", u, want)
	}
}

func BenchmarkCompleteValidateMarshal(b *testing.B) {
	raw, _ := os.ReadFile("testdata/factura-credito-fiscal-31.json")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		var e ECF
		_ = json.Unmarshal(raw, &e)
		_ = Complete(&e)
		_ = Validate(&e)
		if _, err := Marshal(&e); err != nil {
			b.Fatal(err)
		}
	}
}
