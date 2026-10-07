package emision

import (
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/ecf"
)

func TestAnuladoEn(t *testing.T) {
	a := &ecf.ANECF{DetalleAnulacion: ecf.DetalleAnulacion{Anulacion: []ecf.Anulacion{{
		TipoeCF: 31,
		TablaRangoSecuenciasAnuladaseNCF: ecf.TablaRangoSecuenciasAnuladaseNCF{Secuencias: []ecf.Secuencias{
			{SecuenciaeNCFDesde: "E310000000005", SecuenciaeNCFHasta: "E310000000050"}}},
	}}}}
	for encf, want := range map[string]bool{
		"E310000000005": true, "E310000000050": true, "E310000000004": false,
		"E320000000010": false, "F310000000010": false,
	} {
		if got := AnuladoEn(a, encf); got != want {
			t.Errorf("%s: got %v want %v", encf, got, want)
		}
	}
}
