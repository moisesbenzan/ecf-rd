package ecf

import (
	"sort"
	"strconv"
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/xsd"
)

func sameSet(t *testing.T, name string, xsdVals, goVals []string) {
	t.Helper()
	sort.Strings(xsdVals)
	sort.Strings(goVals)
	if len(xsdVals) != len(goVals) {
		t.Fatalf("%s: XSD has %d codes, Go table %d", name, len(xsdVals), len(goVals))
	}
	for i := range xsdVals {
		if xsdVals[i] != goVals[i] {
			t.Fatalf("%s: mismatch %q vs %q", name, xsdVals[i], goVals[i])
		}
	}
}

func TestTablesMatchXSD(t *testing.T) {
	enum := func(typ string) []string {
		v, err := xsd.Enumeration(xsd.ECF31, typ)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var units, geo, mon, imp []string
	for k := range UnidadesMedida {
		units = append(units, strconv.Itoa(k))
	}
	for k := range ProvinciasMunicipios {
		geo = append(geo, k)
	}
	for k := range Monedas {
		mon = append(mon, k)
	}
	for k := range TablaImpuestos {
		imp = append(imp, k)
	}
	sameSet(t, "Tabla IV", enum("UnidadMedidaType"), units)
	sameSet(t, "Tabla III", enum("ProvinciaMunicipioType"), geo)
	sameSet(t, "Tabla II", enum("TipoMonedaType"), mon)
	sameSet(t, "Tabla I", enum("CodificacionTipoImpuestosType"), imp)
}
