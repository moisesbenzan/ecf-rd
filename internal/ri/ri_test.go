package ri

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
)

func dp(s string) *decimal.Decimal { v := decimal.MustParse(s); return &v }

func sample(lines int) *ecf.ECF {
	e := &ecf.ECF{FechaHoraFirma: "01-10-2026 10:00:00"}
	h := &e.Encabezado
	h.IdDoc = ecf.IdDoc{TipoeCF: 31, ENCF: "E310000000001", FechaVencimientoSecuencia: "31-12-2028", TipoIngresos: "01", TipoPago: 1}
	zero := 0
	h.IdDoc.IndicadorMontoGravado = &zero
	h.Emisor = ecf.Emisor{RNCEmisor: "131880738", RazonSocialEmisor: "Comercial Ejemplo, S.R.L.", NombreComercial: "Comercial Ejemplo",
		DireccionEmisor: "Av. 27 de Febrero No. 100", Municipio: "010101", Provincia: "010000", FechaEmision: "01-10-2026"}
	h.Comprador = &ecf.Comprador{RNCComprador: "101672919", RazonSocialComprador: "Cliente Corporativo, S.A."}
	for i := 0; i < lines; i++ {
		ind := ecf.IndicadorFacturacion(1)
		if i%3 == 2 {
			ind = ecf.Exento
		}
		e.DetallesItems.Item = append(e.DetallesItems.Item, ecf.Item{IndicadorFacturacion: ind, NombreItem: "Artículo de prueba con descripción",
			IndicadorBienoServicio: 1, CantidadItem: decimal.FromInt(int64(i%4 + 1)), PrecioUnitarioItem: decimal.MustParse("125.50"), UnidadMedida: 43})
	}
	return e
}

func TestQRVersion(t *testing.T) {
	// RFCE timbre URL: fits version 8.
	short := "https://fc.dgii.gov.do/ecf/consultatimbrefc?rncemisor=131880738&encf=E320000000064&montototal=6225.09&codigoseguridad=uabnyh"
	if _, v, err := QR(short); err != nil || v != QRVersion {
		t.Fatalf("version %d, want %d: %v", v, QRVersion, err)
	}
	// Full e-CF timbre URL (203 bytes) exceeds version 8 capacity (192).
	long := "https://ecf.dgii.gov.do/ecf/consultatimbre?rncemisor=131880738&rnccomprador=101672919&encf=E310000000001&fechaemision=01-10-2026&montototal=4602.00&fechafirma=01-10-2026%2010:00:00&codigoseguridad=inXLSA"
	if _, v, err := QR(long); err != nil || v != 9 {
		t.Fatalf("version %d, want 9: %v", v, err)
	}
}

func TestRender(t *testing.T) {
	out := os.Getenv("RI_OUT")
	for name, e := range map[string]*ecf.ECF{"simple": sample(3), "paginado": sample(30)} {
		if name == "paginado" {
			ecf.Paginar(e, 12)
		}
		if err := ecf.Complete(e); err != nil {
			t.Fatal(err)
		}
		if is := ecf.Validate(e); is.HasErrors() {
			t.Fatal(is.Error())
		}
		pdf, err := Render(e, Opciones{CodigoSeguridad: "inXLSA", URLTimbre: ecf.TimbreURL("https://ecf.dgii.gov.do/testecf/consultatimbre", e, "inXLSA"), Contingencia: name == "simple"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
			t.Fatal("not a PDF")
		}
		if out != "" {
			_ = os.WriteFile(filepath.Join(out, name+".pdf"), pdf, 0o644)
		}
	}
}
