package ecf

import (
	"encoding/xml"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// RFCE is the "Resumen Factura de Consumo Electrónica": the summary sent to
// DGII for type 32 receipts whose total is below RD$250,000.00.
type RFCE struct {
	XMLName    xml.Name       `xml:"RFCE" json:"-"`
	Encabezado RFCEEncabezado `xml:"Encabezado" json:"Encabezado"`
}

// RFCEEncabezado is the RFCE header.
type RFCEEncabezado struct {
	Version            string        `xml:"Version" json:"Version"`
	IdDoc              RFCEIdDoc     `xml:"IdDoc" json:"IdDoc"`
	Emisor             RFCEEmisor    `xml:"Emisor" json:"Emisor"`
	Comprador          RFCEComprador `xml:"Comprador" json:"Comprador"`
	Totales            RFCETotales   `xml:"Totales" json:"Totales"`
	CodigoSeguridadeCF string        `xml:"CodigoSeguridadeCF" json:"CodigoSeguridadeCF"`
}

// RFCEIdDoc identifies the summarized receipt.
type RFCEIdDoc struct {
	TipoeCF         TipoECF          `xml:"TipoeCF" json:"TipoeCF"`
	ENCF            string           `xml:"eNCF" json:"eNCF"`
	TipoIngresos    string           `xml:"TipoIngresos" json:"TipoIngresos"`
	TipoPago        int              `xml:"TipoPago" json:"TipoPago"`
	TablaFormasPago *TablaFormasPago `xml:"TablaFormasPago,omitempty" json:"TablaFormasPago,omitempty"`
}

// RFCEEmisor is the issuer block of the RFCE.
type RFCEEmisor struct {
	RNCEmisor         string `xml:"RNCEmisor" json:"RNCEmisor"`
	RazonSocialEmisor string `xml:"RazonSocialEmisor" json:"RazonSocialEmisor"`
	FechaEmision      string `xml:"FechaEmision" json:"FechaEmision"`
}

// RFCEComprador is the buyer block of the RFCE.
type RFCEComprador struct {
	RNCComprador            string `xml:"RNCComprador,omitempty" json:"RNCComprador,omitempty"`
	IdentificadorExtranjero string `xml:"IdentificadorExtranjero,omitempty" json:"IdentificadorExtranjero,omitempty"`
	RazonSocialComprador    string `xml:"RazonSocialComprador,omitempty" json:"RazonSocialComprador,omitempty"`
}

// RFCETotales is the totals block of the RFCE.
type RFCETotales struct {
	MontoGravadoTotal      Dec             `xml:"MontoGravadoTotal,omitempty" json:"MontoGravadoTotal,omitempty"`
	MontoGravadoI1         Dec             `xml:"MontoGravadoI1,omitempty" json:"MontoGravadoI1,omitempty"`
	MontoGravadoI2         Dec             `xml:"MontoGravadoI2,omitempty" json:"MontoGravadoI2,omitempty"`
	MontoGravadoI3         Dec             `xml:"MontoGravadoI3,omitempty" json:"MontoGravadoI3,omitempty"`
	MontoExento            Dec             `xml:"MontoExento,omitempty" json:"MontoExento,omitempty"`
	TotalITBIS             Dec             `xml:"TotalITBIS,omitempty" json:"TotalITBIS,omitempty"`
	TotalITBIS1            Dec             `xml:"TotalITBIS1,omitempty" json:"TotalITBIS1,omitempty"`
	TotalITBIS2            Dec             `xml:"TotalITBIS2,omitempty" json:"TotalITBIS2,omitempty"`
	TotalITBIS3            Dec             `xml:"TotalITBIS3,omitempty" json:"TotalITBIS3,omitempty"`
	MontoImpuestoAdicional Dec             `xml:"MontoImpuestoAdicional,omitempty" json:"MontoImpuestoAdicional,omitempty"`
	ImpuestosAdicionales   *RFCEImpuestos  `xml:"ImpuestosAdicionales,omitempty" json:"ImpuestosAdicionales,omitempty"`
	MontoTotal             decimal.Decimal `xml:"MontoTotal" json:"MontoTotal"`
	MontoNoFacturable      Dec             `xml:"MontoNoFacturable,omitempty" json:"MontoNoFacturable,omitempty"`
	MontoPeriodo           Dec             `xml:"MontoPeriodo,omitempty" json:"MontoPeriodo,omitempty"`
}

// RFCEImpuestos lists additional taxes in the RFCE (no rate element).
type RFCEImpuestos struct {
	ImpuestoAdicional []RFCEImpuestoAdicional `xml:"ImpuestoAdicional" json:"ImpuestoAdicional"`
}

// RFCEImpuestoAdicional is an additional tax entry in the RFCE.
type RFCEImpuestoAdicional struct {
	TipoImpuesto                            string `xml:"TipoImpuesto" json:"TipoImpuesto"`
	MontoImpuestoSelectivoConsumoEspecifico Dec    `xml:"MontoImpuestoSelectivoConsumoEspecifico,omitempty" json:"MontoImpuestoSelectivoConsumoEspecifico,omitempty"`
	MontoImpuestoSelectivoConsumoAdvalorem  Dec    `xml:"MontoImpuestoSelectivoConsumoAdvalorem,omitempty" json:"MontoImpuestoSelectivoConsumoAdvalorem,omitempty"`
	OtrosImpuestosAdicionales               Dec    `xml:"OtrosImpuestosAdicionales,omitempty" json:"OtrosImpuestosAdicionales,omitempty"`
}

// NewRFCE builds the consumer-invoice summary for a signed type 32 e-CF.
// codigoSeguridad is the 6-character security code taken from the e-CF
// signature (see xmldsig.SecurityCode).
func NewRFCE(e *ECF, codigoSeguridad string) *RFCE {
	h := &e.Encabezado
	r := &RFCE{Encabezado: RFCEEncabezado{
		Version: Version,
		IdDoc: RFCEIdDoc{
			TipoeCF:         h.IdDoc.TipoeCF,
			ENCF:            h.IdDoc.ENCF,
			TipoIngresos:    h.IdDoc.TipoIngresos,
			TipoPago:        h.IdDoc.TipoPago,
			TablaFormasPago: h.IdDoc.TablaFormasPago,
		},
		Emisor: RFCEEmisor{
			RNCEmisor:         h.Emisor.RNCEmisor,
			RazonSocialEmisor: h.Emisor.RazonSocialEmisor,
			FechaEmision:      h.Emisor.FechaEmision,
		},
		Totales: RFCETotales{
			MontoGravadoTotal:      h.Totales.MontoGravadoTotal,
			MontoGravadoI1:         h.Totales.MontoGravadoI1,
			MontoGravadoI2:         h.Totales.MontoGravadoI2,
			MontoGravadoI3:         h.Totales.MontoGravadoI3,
			MontoExento:            h.Totales.MontoExento,
			TotalITBIS:             h.Totales.TotalITBIS,
			TotalITBIS1:            h.Totales.TotalITBIS1,
			TotalITBIS2:            h.Totales.TotalITBIS2,
			TotalITBIS3:            h.Totales.TotalITBIS3,
			MontoImpuestoAdicional: h.Totales.MontoImpuestoAdicional,
			MontoTotal:             h.Totales.MontoTotal,
			MontoNoFacturable:      h.Totales.MontoNoFacturable,
			MontoPeriodo:           h.Totales.MontoPeriodo,
		},
		CodigoSeguridadeCF: codigoSeguridad,
	}}
	if c := h.Comprador; c != nil {
		r.Encabezado.Comprador = RFCEComprador{
			RNCComprador:            c.RNCComprador,
			IdentificadorExtranjero: c.IdentificadorExtranjero,
			RazonSocialComprador:    c.RazonSocialComprador,
		}
	}
	if ia := h.Totales.ImpuestosAdicionales; ia != nil {
		r.Encabezado.Totales.ImpuestosAdicionales = &RFCEImpuestos{}
		for _, i := range ia.ImpuestoAdicional {
			r.Encabezado.Totales.ImpuestosAdicionales.ImpuestoAdicional = append(
				r.Encabezado.Totales.ImpuestosAdicionales.ImpuestoAdicional,
				RFCEImpuestoAdicional{
					TipoImpuesto:                            i.TipoImpuesto,
					MontoImpuestoSelectivoConsumoEspecifico: i.MontoImpuestoSelectivoConsumoEspecifico,
					MontoImpuestoSelectivoConsumoAdvalorem:  i.MontoImpuestoSelectivoConsumoAdvalorem,
					OtrosImpuestosAdicionales:               i.OtrosImpuestosAdicionales,
				})
		}
	}
	return r
}

// Acuse de recibo states.
const (
	AcuseRecibido   = 0
	AcuseNoRecibido = 1
)

// Reasons for not receiving an e-CF (CodigoMotivoNoRecibido).
const (
	MotivoErrorEspecificacion = 1
	MotivoErrorFirma          = 2
	MotivoEnvioDuplicado      = 3
	MotivoRNCNoCorresponde    = 4
)

// ARECF is the receipt acknowledgement ("Acuse de Recibo") a receiver
// returns, signed, when an e-CF is delivered to it.
type ARECF struct {
	XMLName              xml.Name             `xml:"ARECF" json:"-"`
	DetalleAcusedeRecibo DetalleAcusedeRecibo `xml:"DetalleAcusedeRecibo" json:"DetalleAcusedeRecibo"`
}

// DetalleAcusedeRecibo is the body of an ARECF.
type DetalleAcusedeRecibo struct {
	Version                string `xml:"Version" json:"Version"`
	RNCEmisor              string `xml:"RNCEmisor" json:"RNCEmisor"`
	RNCComprador           string `xml:"RNCComprador" json:"RNCComprador"`
	ENCF                   string `xml:"eNCF" json:"eNCF"`
	Estado                 int    `xml:"Estado" json:"Estado"`
	CodigoMotivoNoRecibido int    `xml:"CodigoMotivoNoRecibido,omitempty" json:"CodigoMotivoNoRecibido,omitempty"`
	FechaHoraAcuseRecibo   string `xml:"FechaHoraAcuseRecibo" json:"FechaHoraAcuseRecibo"`
}

// Aprobación comercial states.
const (
	AprobacionAceptado  = 1
	AprobacionRechazado = 2
)

// ACECF is the commercial approval ("Aprobación Comercial") a receiver sends
// to the issuer and to DGII to accept or reject an e-CF.
type ACECF struct {
	XMLName                    xml.Name                   `xml:"ACECF" json:"-"`
	DetalleAprobacionComercial DetalleAprobacionComercial `xml:"DetalleAprobacionComercial" json:"DetalleAprobacionComercial"`
}

// DetalleAprobacionComercial is the body of an ACECF.
type DetalleAprobacionComercial struct {
	Version                      string          `xml:"Version" json:"Version"`
	RNCEmisor                    string          `xml:"RNCEmisor" json:"RNCEmisor"`
	ENCF                         string          `xml:"eNCF" json:"eNCF"`
	FechaEmision                 string          `xml:"FechaEmision" json:"FechaEmision"`
	MontoTotal                   decimal.Decimal `xml:"MontoTotal" json:"MontoTotal"`
	RNCComprador                 string          `xml:"RNCComprador" json:"RNCComprador"`
	Estado                       int             `xml:"Estado" json:"Estado"`
	DetalleMotivoRechazo         string          `xml:"DetalleMotivoRechazo,omitempty" json:"DetalleMotivoRechazo,omitempty"`
	FechaHoraAprobacionComercial string          `xml:"FechaHoraAprobacionComercial" json:"FechaHoraAprobacionComercial"`
}

// ANECF is a request to void ("anular") unused e-NCF sequence ranges.
type ANECF struct {
	XMLName          xml.Name         `xml:"ANECF" json:"-"`
	Encabezado       ANECFEncabezado  `xml:"Encabezado" json:"Encabezado"`
	DetalleAnulacion DetalleAnulacion `xml:"DetalleAnulacion" json:"DetalleAnulacion"`
}

// ANECFEncabezado is the ANECF header.
type ANECFEncabezado struct {
	Version                string `xml:"Version" json:"Version"`
	RncEmisor              string `xml:"RncEmisor" json:"RncEmisor"`
	CantidadeNCFAnulados   int64  `xml:"CantidadeNCFAnulados" json:"CantidadeNCFAnulados"`
	FechaHoraAnulacioneNCF string `xml:"FechaHoraAnulacioneNCF" json:"FechaHoraAnulacioneNCF"`
}

// DetalleAnulacion lists up to 10 Anulacion entries.
type DetalleAnulacion struct {
	Anulacion []Anulacion `xml:"Anulacion" json:"Anulacion"`
}

// Anulacion voids ranges of a single e-CF type.
type Anulacion struct {
	NoLinea                          int                              `xml:"NoLinea" json:"NoLinea"`
	TipoeCF                          TipoECF                          `xml:"TipoeCF" json:"TipoeCF"`
	TablaRangoSecuenciasAnuladaseNCF TablaRangoSecuenciasAnuladaseNCF `xml:"TablaRangoSecuenciasAnuladaseNCF" json:"TablaRangoSecuenciasAnuladaseNCF"`
	CantidadeNCFAnulados             int64                            `xml:"CantidadeNCFAnulados" json:"CantidadeNCFAnulados"`
}

// TablaRangoSecuenciasAnuladaseNCF holds the voided ranges.
type TablaRangoSecuenciasAnuladaseNCF struct {
	Secuencias []Secuencias `xml:"Secuencias" json:"Secuencias"`
}

// Secuencias is an inclusive e-NCF range.
type Secuencias struct {
	SecuenciaeNCFDesde string `xml:"SecuenciaeNCFDesde" json:"SecuenciaeNCFDesde"`
	SecuenciaeNCFHasta string `xml:"SecuenciaeNCFHasta" json:"SecuenciaeNCFHasta"`
}

// Fill computes the per-line and header counts of an ANECF and sets Version
// and NoLinea. It returns an error for malformed or inverted ranges.
func (a *ANECF) Fill() error {
	a.Encabezado.Version = Version
	var total int64
	for i := range a.DetalleAnulacion.Anulacion {
		an := &a.DetalleAnulacion.Anulacion[i]
		an.NoLinea = i + 1
		var count int64
		for _, s := range an.TablaRangoSecuenciasAnuladaseNCF.Secuencias {
			d, err := ParseENCF(s.SecuenciaeNCFDesde)
			if err != nil {
				return err
			}
			h, err := ParseENCF(s.SecuenciaeNCFHasta)
			if err != nil {
				return err
			}
			if d.Tipo != an.TipoeCF || h.Tipo != an.TipoeCF {
				return errRangeType(an.TipoeCF, s)
			}
			if d.Secuencia > h.Secuencia {
				return errRangeOrder(s)
			}
			count += h.Secuencia - d.Secuencia + 1
		}
		an.CantidadeNCFAnulados = count
		total += count
	}
	a.Encabezado.CantidadeNCFAnulados = total
	return nil
}
