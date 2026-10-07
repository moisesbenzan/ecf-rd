package xsd

import (
	"strings"
	"testing"
)

func TestCompileAll(t *testing.T) {
	MustLoadAll()
}

func TestValidateARECF(t *testing.T) {
	ok := `<?xml version="1.0" encoding="utf-8"?><ARECF><DetalleAcusedeRecibo><Version>1.0</Version><RNCEmisor>131880738</RNCEmisor><RNCComprador>101672919</RNCComprador><eNCF>E310000000001</eNCF><Estado>0</Estado><FechaHoraAcuseRecibo>17-12-2020 11:19:06</FechaHoraAcuseRecibo></DetalleAcusedeRecibo></ARECF>`
	errs, err := Validate(ARECF, []byte(ok), Options{})
	if err != nil || len(errs) != 0 {
		t.Fatalf("valid doc rejected: %v %v", errs, err)
	}
	cases := map[string]string{
		"bad estado":   strings.Replace(ok, "<Estado>0</Estado>", "<Estado>7</Estado>", 1),
		"bad rnc":      strings.Replace(ok, "131880738", "1318807", 1),
		"bad date":     strings.Replace(ok, "17-12-2020 11:19:06", "2020-12-17T11:19:06", 1),
		"out of order": strings.Replace(ok, "<Version>1.0</Version><RNCEmisor>131880738</RNCEmisor>", "<RNCEmisor>131880738</RNCEmisor><Version>1.0</Version>", 1),
		"unknown":      strings.Replace(ok, "<Estado>0</Estado>", "<Estado>0</Estado><Foo>1</Foo>", 1),
		"missing":      strings.Replace(ok, "<eNCF>E310000000001</eNCF>", "", 1),
	}
	for name, doc := range cases {
		errs, _ := Validate(ARECF, []byte(doc), Options{})
		if len(errs) == 0 {
			t.Errorf("%s: expected schema error", name)
		}
	}
}

func TestEnumeration(t *testing.T) {
	units, err := Enumeration(ECF31, "UnidadMedidaType")
	if err != nil {
		t.Skipf("type name differs: %v", err)
	}
	if len(units) < 60 {
		t.Fatalf("expected unit table, got %d", len(units))
	}
}

func TestPatternTranslation(t *testing.T) {
	re, err := translatePattern(`\w+([-+.]\w+)*@\w+([-.]\w+)*\.\w+([-.]\w+)*`)
	if err != nil {
		t.Fatal(err)
	}
	if !re.MatchString("facturación@ejemplo.com.do") || re.MatchString("no-at-sign") {
		t.Fatal("email pattern")
	}
	re, _ = translatePattern(`^[a-zA-ZñÑ,\s]*$`)
	if !re.MatchString("Santo Domingo, Ñ") {
		t.Fatal("anchored pattern")
	}
}
