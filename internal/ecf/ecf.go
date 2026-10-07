// Package ecf models the DGII Comprobante Fiscal Electrónico (e-CF) v1.0
// formats and implements their business validations.
//
// Struct field order mirrors the element order of the official XSDs
// (e-CF 31..47 v1.0), since XML Schema sequences are order-sensitive. The
// model is the union of all e-CF types; fields that do not apply to a given
// TipoeCF must be left empty and are rejected by Validate when present.
//
// JSON keys are identical to the XML tag names so that API clients can work
// directly from the DGII "Formato Comprobante Fiscal Electrónico" document.
package ecf

import (
	"encoding/xml"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// Dec is a shorthand for an optional decimal value.
type Dec = *decimal.Decimal

// ECF is the root element of an electronic fiscal receipt.
type ECF struct {
	XMLName               xml.Name               `xml:"ECF" json:"-"`
	Encabezado            Encabezado             `xml:"Encabezado" json:"Encabezado"`
	DetallesItems         DetallesItems          `xml:"DetallesItems" json:"DetallesItems"`
	Subtotales            *Subtotales            `xml:"Subtotales,omitempty" json:"Subtotales,omitempty"`
	DescuentosORecargos   *DescuentosORecargos   `xml:"DescuentosORecargos,omitempty" json:"DescuentosORecargos,omitempty"`
	Paginacion            *Paginacion            `xml:"Paginacion,omitempty" json:"Paginacion,omitempty"`
	InformacionReferencia *InformacionReferencia `xml:"InformacionReferencia,omitempty" json:"InformacionReferencia,omitempty"`
	// FechaHoraFirma has the format dd-MM-yyyy HH:mm:ss (GMT-4). It is set
	// automatically at signing time when empty.
	FechaHoraFirma string `xml:"FechaHoraFirma,omitempty" json:"FechaHoraFirma,omitempty"`
}

// Encabezado is section A of the e-CF.
type Encabezado struct {
	Version                  string                    `xml:"Version" json:"Version"`
	IdDoc                    IdDoc                     `xml:"IdDoc" json:"IdDoc"`
	Emisor                   Emisor                    `xml:"Emisor" json:"Emisor"`
	Comprador                *Comprador                `xml:"Comprador,omitempty" json:"Comprador,omitempty"`
	InformacionesAdicionales *InformacionesAdicionales `xml:"InformacionesAdicionales,omitempty" json:"InformacionesAdicionales,omitempty"`
	Transporte               *Transporte               `xml:"Transporte,omitempty" json:"Transporte,omitempty"`
	Totales                  Totales                   `xml:"Totales" json:"Totales"`
	OtraMoneda               *OtraMoneda               `xml:"OtraMoneda,omitempty" json:"OtraMoneda,omitempty"`
}

// IdDoc identifies the document.
type IdDoc struct {
	TipoeCF                   TipoECF          `xml:"TipoeCF" json:"TipoeCF"`
	ENCF                      string           `xml:"eNCF" json:"eNCF"`
	FechaVencimientoSecuencia string           `xml:"FechaVencimientoSecuencia,omitempty" json:"FechaVencimientoSecuencia,omitempty"`
	IndicadorNotaCredito      *int             `xml:"IndicadorNotaCredito,omitempty" json:"IndicadorNotaCredito,omitempty"`
	IndicadorEnvioDiferido    *int             `xml:"IndicadorEnvioDiferido,omitempty" json:"IndicadorEnvioDiferido,omitempty"`
	IndicadorMontoGravado     *int             `xml:"IndicadorMontoGravado,omitempty" json:"IndicadorMontoGravado,omitempty"`
	IndicadorServicioTodoIncl *int             `xml:"IndicadorServicioTodoIncluido,omitempty" json:"IndicadorServicioTodoIncluido,omitempty"`
	TipoIngresos              string           `xml:"TipoIngresos,omitempty" json:"TipoIngresos,omitempty"`
	TipoPago                  int              `xml:"TipoPago,omitempty" json:"TipoPago,omitempty"`
	FechaLimitePago           string           `xml:"FechaLimitePago,omitempty" json:"FechaLimitePago,omitempty"`
	TerminoPago               string           `xml:"TerminoPago,omitempty" json:"TerminoPago,omitempty"`
	TablaFormasPago           *TablaFormasPago `xml:"TablaFormasPago,omitempty" json:"TablaFormasPago,omitempty"`
	TipoCuentaPago            string           `xml:"TipoCuentaPago,omitempty" json:"TipoCuentaPago,omitempty"`
	NumeroCuentaPago          string           `xml:"NumeroCuentaPago,omitempty" json:"NumeroCuentaPago,omitempty"`
	BancoPago                 string           `xml:"BancoPago,omitempty" json:"BancoPago,omitempty"`
	FechaDesde                string           `xml:"FechaDesde,omitempty" json:"FechaDesde,omitempty"`
	FechaHasta                string           `xml:"FechaHasta,omitempty" json:"FechaHasta,omitempty"`
	TotalPaginas              int              `xml:"TotalPaginas,omitempty" json:"TotalPaginas,omitempty"`
}

// TablaFormasPago lists up to 7 payment methods.
type TablaFormasPago struct {
	FormaDePago []FormaDePago `xml:"FormaDePago" json:"FormaDePago"`
}

// FormaDePago is a payment method / amount pair.
type FormaDePago struct {
	FormaPago int             `xml:"FormaPago" json:"FormaPago"`
	MontoPago decimal.Decimal `xml:"MontoPago" json:"MontoPago"`
}

// Emisor holds the issuer data.
type Emisor struct {
	RNCEmisor                  string               `xml:"RNCEmisor" json:"RNCEmisor"`
	RazonSocialEmisor          string               `xml:"RazonSocialEmisor" json:"RazonSocialEmisor"`
	NombreComercial            string               `xml:"NombreComercial,omitempty" json:"NombreComercial,omitempty"`
	Sucursal                   string               `xml:"Sucursal,omitempty" json:"Sucursal,omitempty"`
	DireccionEmisor            string               `xml:"DireccionEmisor" json:"DireccionEmisor"`
	Municipio                  string               `xml:"Municipio,omitempty" json:"Municipio,omitempty"`
	Provincia                  string               `xml:"Provincia,omitempty" json:"Provincia,omitempty"`
	TablaTelefonoEmisor        *TablaTelefonoEmisor `xml:"TablaTelefonoEmisor,omitempty" json:"TablaTelefonoEmisor,omitempty"`
	CorreoEmisor               string               `xml:"CorreoEmisor,omitempty" json:"CorreoEmisor,omitempty"`
	WebSite                    string               `xml:"WebSite,omitempty" json:"WebSite,omitempty"`
	ActividadEconomica         string               `xml:"ActividadEconomica,omitempty" json:"ActividadEconomica,omitempty"`
	CodigoVendedor             string               `xml:"CodigoVendedor,omitempty" json:"CodigoVendedor,omitempty"`
	NumeroFacturaInterna       string               `xml:"NumeroFacturaInterna,omitempty" json:"NumeroFacturaInterna,omitempty"`
	NumeroPedidoInterno        string               `xml:"NumeroPedidoInterno,omitempty" json:"NumeroPedidoInterno,omitempty"`
	ZonaVenta                  string               `xml:"ZonaVenta,omitempty" json:"ZonaVenta,omitempty"`
	RutaVenta                  string               `xml:"RutaVenta,omitempty" json:"RutaVenta,omitempty"`
	InformacionAdicionalEmisor string               `xml:"InformacionAdicionalEmisor,omitempty" json:"InformacionAdicionalEmisor,omitempty"`
	FechaEmision               string               `xml:"FechaEmision" json:"FechaEmision"`
}

// TablaTelefonoEmisor holds up to 3 phone numbers (xxx-xxx-xxxx).
type TablaTelefonoEmisor struct {
	TelefonoEmisor []string `xml:"TelefonoEmisor" json:"TelefonoEmisor"`
}

// Comprador holds the buyer data.
type Comprador struct {
	RNCComprador                  string `xml:"RNCComprador,omitempty" json:"RNCComprador,omitempty"`
	IdentificadorExtranjero       string `xml:"IdentificadorExtranjero,omitempty" json:"IdentificadorExtranjero,omitempty"`
	RazonSocialComprador          string `xml:"RazonSocialComprador,omitempty" json:"RazonSocialComprador,omitempty"`
	ContactoComprador             string `xml:"ContactoComprador,omitempty" json:"ContactoComprador,omitempty"`
	CorreoComprador               string `xml:"CorreoComprador,omitempty" json:"CorreoComprador,omitempty"`
	DireccionComprador            string `xml:"DireccionComprador,omitempty" json:"DireccionComprador,omitempty"`
	MunicipioComprador            string `xml:"MunicipioComprador,omitempty" json:"MunicipioComprador,omitempty"`
	ProvinciaComprador            string `xml:"ProvinciaComprador,omitempty" json:"ProvinciaComprador,omitempty"`
	PaisComprador                 string `xml:"PaisComprador,omitempty" json:"PaisComprador,omitempty"`
	FechaEntrega                  string `xml:"FechaEntrega,omitempty" json:"FechaEntrega,omitempty"`
	ContactoEntrega               string `xml:"ContactoEntrega,omitempty" json:"ContactoEntrega,omitempty"`
	DireccionEntrega              string `xml:"DireccionEntrega,omitempty" json:"DireccionEntrega,omitempty"`
	TelefonoAdicional             string `xml:"TelefonoAdicional,omitempty" json:"TelefonoAdicional,omitempty"`
	FechaOrdenCompra              string `xml:"FechaOrdenCompra,omitempty" json:"FechaOrdenCompra,omitempty"`
	NumeroOrdenCompra             string `xml:"NumeroOrdenCompra,omitempty" json:"NumeroOrdenCompra,omitempty"`
	CodigoInternoComprador        string `xml:"CodigoInternoComprador,omitempty" json:"CodigoInternoComprador,omitempty"`
	ResponsablePago               string `xml:"ResponsablePago,omitempty" json:"ResponsablePago,omitempty"`
	InformacionAdicionalComprador string `xml:"InformacionAdicionalComprador,omitempty" json:"InformacionAdicionalComprador,omitempty"`
}

// InformacionesAdicionales carries shipping / export information.
type InformacionesAdicionales struct {
	FechaEmbarque           string `xml:"FechaEmbarque,omitempty" json:"FechaEmbarque,omitempty"`
	NumeroEmbarque          string `xml:"NumeroEmbarque,omitempty" json:"NumeroEmbarque,omitempty"`
	NumeroContenedor        string `xml:"NumeroContenedor,omitempty" json:"NumeroContenedor,omitempty"`
	NumeroReferencia        string `xml:"NumeroReferencia,omitempty" json:"NumeroReferencia,omitempty"`
	NombrePuertoEmbarque    string `xml:"NombrePuertoEmbarque,omitempty" json:"NombrePuertoEmbarque,omitempty"`
	CondicionesEntrega      string `xml:"CondicionesEntrega,omitempty" json:"CondicionesEntrega,omitempty"`
	TotalFob                Dec    `xml:"TotalFob,omitempty" json:"TotalFob,omitempty"`
	Seguro                  Dec    `xml:"Seguro,omitempty" json:"Seguro,omitempty"`
	Flete                   Dec    `xml:"Flete,omitempty" json:"Flete,omitempty"`
	OtrosGastos             Dec    `xml:"OtrosGastos,omitempty" json:"OtrosGastos,omitempty"`
	TotalCif                Dec    `xml:"TotalCif,omitempty" json:"TotalCif,omitempty"`
	RegimenAduanero         string `xml:"RegimenAduanero,omitempty" json:"RegimenAduanero,omitempty"`
	NombrePuertoSalida      string `xml:"NombrePuertoSalida,omitempty" json:"NombrePuertoSalida,omitempty"`
	NombrePuertoDesembarque string `xml:"NombrePuertoDesembarque,omitempty" json:"NombrePuertoDesembarque,omitempty"`
	PesoBruto               Dec    `xml:"PesoBruto,omitempty" json:"PesoBruto,omitempty"`
	PesoNeto                Dec    `xml:"PesoNeto,omitempty" json:"PesoNeto,omitempty"`
	UnidadPesoBruto         int    `xml:"UnidadPesoBruto,omitempty" json:"UnidadPesoBruto,omitempty"`
	UnidadPesoNeto          int    `xml:"UnidadPesoNeto,omitempty" json:"UnidadPesoNeto,omitempty"`
	CantidadBulto           Dec    `xml:"CantidadBulto,omitempty" json:"CantidadBulto,omitempty"`
	UnidadBulto             int    `xml:"UnidadBulto,omitempty" json:"UnidadBulto,omitempty"`
	VolumenBulto            Dec    `xml:"VolumenBulto,omitempty" json:"VolumenBulto,omitempty"`
	UnidadVolumen           int    `xml:"UnidadVolumen,omitempty" json:"UnidadVolumen,omitempty"`
}

// Transporte carries transport information.
type Transporte struct {
	ViaTransporte        string `xml:"ViaTransporte,omitempty" json:"ViaTransporte,omitempty"`
	PaisOrigen           string `xml:"PaisOrigen,omitempty" json:"PaisOrigen,omitempty"`
	DireccionDestino     string `xml:"DireccionDestino,omitempty" json:"DireccionDestino,omitempty"`
	PaisDestino          string `xml:"PaisDestino,omitempty" json:"PaisDestino,omitempty"`
	RNCIdentificacionCia string `xml:"RNCIdentificacionCompaniaTransportista,omitempty" json:"RNCIdentificacionCompaniaTransportista,omitempty"`
	NombreCompaniaTransp string `xml:"NombreCompaniaTransportista,omitempty" json:"NombreCompaniaTransportista,omitempty"`
	NumeroViaje          string `xml:"NumeroViaje,omitempty" json:"NumeroViaje,omitempty"`
	Conductor            string `xml:"Conductor,omitempty" json:"Conductor,omitempty"`
	DocumentoTransporte  string `xml:"DocumentoTransporte,omitempty" json:"DocumentoTransporte,omitempty"`
	Ficha                string `xml:"Ficha,omitempty" json:"Ficha,omitempty"`
	Placa                string `xml:"Placa,omitempty" json:"Placa,omitempty"`
	RutaTransporte       string `xml:"RutaTransporte,omitempty" json:"RutaTransporte,omitempty"`
	ZonaTransporte       string `xml:"ZonaTransporte,omitempty" json:"ZonaTransporte,omitempty"`
	NumeroAlbaran        string `xml:"NumeroAlbaran,omitempty" json:"NumeroAlbaran,omitempty"`
}

// Totales holds the document totals.
type Totales struct {
	MontoGravadoTotal      Dec                   `xml:"MontoGravadoTotal,omitempty" json:"MontoGravadoTotal,omitempty"`
	MontoGravadoI1         Dec                   `xml:"MontoGravadoI1,omitempty" json:"MontoGravadoI1,omitempty"`
	MontoGravadoI2         Dec                   `xml:"MontoGravadoI2,omitempty" json:"MontoGravadoI2,omitempty"`
	MontoGravadoI3         Dec                   `xml:"MontoGravadoI3,omitempty" json:"MontoGravadoI3,omitempty"`
	MontoExento            Dec                   `xml:"MontoExento,omitempty" json:"MontoExento,omitempty"`
	ITBIS1                 *int                  `xml:"ITBIS1,omitempty" json:"ITBIS1,omitempty"`
	ITBIS2                 *int                  `xml:"ITBIS2,omitempty" json:"ITBIS2,omitempty"`
	ITBIS3                 *int                  `xml:"ITBIS3,omitempty" json:"ITBIS3,omitempty"`
	TotalITBIS             Dec                   `xml:"TotalITBIS,omitempty" json:"TotalITBIS,omitempty"`
	TotalITBIS1            Dec                   `xml:"TotalITBIS1,omitempty" json:"TotalITBIS1,omitempty"`
	TotalITBIS2            Dec                   `xml:"TotalITBIS2,omitempty" json:"TotalITBIS2,omitempty"`
	TotalITBIS3            Dec                   `xml:"TotalITBIS3,omitempty" json:"TotalITBIS3,omitempty"`
	MontoImpuestoAdicional Dec                   `xml:"MontoImpuestoAdicional,omitempty" json:"MontoImpuestoAdicional,omitempty"`
	ImpuestosAdicionales   *ImpuestosAdicionales `xml:"ImpuestosAdicionales,omitempty" json:"ImpuestosAdicionales,omitempty"`
	MontoTotal             decimal.Decimal       `xml:"MontoTotal" json:"MontoTotal"`
	MontoNoFacturable      Dec                   `xml:"MontoNoFacturable,omitempty" json:"MontoNoFacturable,omitempty"`
	MontoPeriodo           Dec                   `xml:"MontoPeriodo,omitempty" json:"MontoPeriodo,omitempty"`
	SaldoAnterior          Dec                   `xml:"SaldoAnterior,omitempty" json:"SaldoAnterior,omitempty"`
	MontoAvancePago        Dec                   `xml:"MontoAvancePago,omitempty" json:"MontoAvancePago,omitempty"`
	ValorPagar             Dec                   `xml:"ValorPagar,omitempty" json:"ValorPagar,omitempty"`
	TotalITBISRetenido     Dec                   `xml:"TotalITBISRetenido,omitempty" json:"TotalITBISRetenido,omitempty"`
	TotalISRRetencion      Dec                   `xml:"TotalISRRetencion,omitempty" json:"TotalISRRetencion,omitempty"`
	TotalITBISPercepcion   Dec                   `xml:"TotalITBISPercepcion,omitempty" json:"TotalITBISPercepcion,omitempty"`
	TotalISRPercepcion     Dec                   `xml:"TotalISRPercepcion,omitempty" json:"TotalISRPercepcion,omitempty"`
}

// ImpuestosAdicionales lists up to 20 additional taxes.
type ImpuestosAdicionales struct {
	ImpuestoAdicional []ImpuestoAdicionalTotal `xml:"ImpuestoAdicional" json:"ImpuestoAdicional"`
}

// ImpuestoAdicionalTotal is an additional tax totalled at document level.
type ImpuestoAdicionalTotal struct {
	TipoImpuesto                            string `xml:"TipoImpuesto" json:"TipoImpuesto"`
	TasaImpuestoAdicional                   Dec    `xml:"TasaImpuestoAdicional,omitempty" json:"TasaImpuestoAdicional,omitempty"`
	MontoImpuestoSelectivoConsumoEspecifico Dec    `xml:"MontoImpuestoSelectivoConsumoEspecifico,omitempty" json:"MontoImpuestoSelectivoConsumoEspecifico,omitempty"`
	MontoImpuestoSelectivoConsumoAdvalorem  Dec    `xml:"MontoImpuestoSelectivoConsumoAdvalorem,omitempty" json:"MontoImpuestoSelectivoConsumoAdvalorem,omitempty"`
	OtrosImpuestosAdicionales               Dec    `xml:"OtrosImpuestosAdicionales,omitempty" json:"OtrosImpuestosAdicionales,omitempty"`
}

// OtraMoneda carries the totals expressed in a foreign currency.
type OtraMoneda struct {
	TipoMoneda                       string                          `xml:"TipoMoneda" json:"TipoMoneda"`
	TipoCambio                       decimal.Decimal                 `xml:"TipoCambio" json:"TipoCambio"`
	MontoGravadoTotalOtraMoneda      Dec                             `xml:"MontoGravadoTotalOtraMoneda,omitempty" json:"MontoGravadoTotalOtraMoneda,omitempty"`
	MontoGravado1OtraMoneda          Dec                             `xml:"MontoGravado1OtraMoneda,omitempty" json:"MontoGravado1OtraMoneda,omitempty"`
	MontoGravado2OtraMoneda          Dec                             `xml:"MontoGravado2OtraMoneda,omitempty" json:"MontoGravado2OtraMoneda,omitempty"`
	MontoGravado3OtraMoneda          Dec                             `xml:"MontoGravado3OtraMoneda,omitempty" json:"MontoGravado3OtraMoneda,omitempty"`
	MontoExentoOtraMoneda            Dec                             `xml:"MontoExentoOtraMoneda,omitempty" json:"MontoExentoOtraMoneda,omitempty"`
	TotalITBISOtraMoneda             Dec                             `xml:"TotalITBISOtraMoneda,omitempty" json:"TotalITBISOtraMoneda,omitempty"`
	TotalITBIS1OtraMoneda            Dec                             `xml:"TotalITBIS1OtraMoneda,omitempty" json:"TotalITBIS1OtraMoneda,omitempty"`
	TotalITBIS2OtraMoneda            Dec                             `xml:"TotalITBIS2OtraMoneda,omitempty" json:"TotalITBIS2OtraMoneda,omitempty"`
	TotalITBIS3OtraMoneda            Dec                             `xml:"TotalITBIS3OtraMoneda,omitempty" json:"TotalITBIS3OtraMoneda,omitempty"`
	MontoImpuestoAdicionalOtraMoneda Dec                             `xml:"MontoImpuestoAdicionalOtraMoneda,omitempty" json:"MontoImpuestoAdicionalOtraMoneda,omitempty"`
	ImpuestosAdicionalesOtraMoneda   *ImpuestosAdicionalesOtraMoneda `xml:"ImpuestosAdicionalesOtraMoneda,omitempty" json:"ImpuestosAdicionalesOtraMoneda,omitempty"`
	MontoTotalOtraMoneda             decimal.Decimal                 `xml:"MontoTotalOtraMoneda" json:"MontoTotalOtraMoneda"`
}

// ImpuestosAdicionalesOtraMoneda lists additional taxes in foreign currency.
type ImpuestosAdicionalesOtraMoneda struct {
	ImpuestoAdicionalOtraMoneda []ImpuestoAdicionalOtraMoneda `xml:"ImpuestoAdicionalOtraMoneda" json:"ImpuestoAdicionalOtraMoneda"`
}

// ImpuestoAdicionalOtraMoneda is an additional tax in foreign currency.
type ImpuestoAdicionalOtraMoneda struct {
	TipoImpuestoOtraMoneda                            string `xml:"TipoImpuestoOtraMoneda" json:"TipoImpuestoOtraMoneda"`
	TasaImpuestoAdicionalOtraMoneda                   Dec    `xml:"TasaImpuestoAdicionalOtraMoneda,omitempty" json:"TasaImpuestoAdicionalOtraMoneda,omitempty"`
	MontoImpuestoSelectivoConsumoEspecificoOtraMoneda Dec    `xml:"MontoImpuestoSelectivoConsumoEspecificoOtraMoneda,omitempty" json:"MontoImpuestoSelectivoConsumoEspecificoOtraMoneda,omitempty"`
	MontoImpuestoSelectivoConsumoAdvaloremOtraMoneda  Dec    `xml:"MontoImpuestoSelectivoConsumoAdvaloremOtraMoneda,omitempty" json:"MontoImpuestoSelectivoConsumoAdvaloremOtraMoneda,omitempty"`
	OtrosImpuestosAdicionalesOtraMoneda               Dec    `xml:"OtrosImpuestosAdicionalesOtraMoneda,omitempty" json:"OtrosImpuestosAdicionalesOtraMoneda,omitempty"`
}

// DetallesItems is section B: one Item per line (1..1000).
type DetallesItems struct {
	Item []Item `xml:"Item" json:"Item"`
}

// Item is a line of goods or services.
type Item struct {
	NumeroLinea              int                     `xml:"NumeroLinea" json:"NumeroLinea"`
	TablaCodigosItem         *TablaCodigosItem       `xml:"TablaCodigosItem,omitempty" json:"TablaCodigosItem,omitempty"`
	IndicadorFacturacion     IndicadorFacturacion    `xml:"IndicadorFacturacion" json:"IndicadorFacturacion"`
	Retencion                *Retencion              `xml:"Retencion,omitempty" json:"Retencion,omitempty"`
	NombreItem               string                  `xml:"NombreItem" json:"NombreItem"`
	IndicadorBienoServicio   int                     `xml:"IndicadorBienoServicio" json:"IndicadorBienoServicio"`
	DescripcionItem          string                  `xml:"DescripcionItem,omitempty" json:"DescripcionItem,omitempty"`
	CantidadItem             decimal.Decimal         `xml:"CantidadItem" json:"CantidadItem"`
	UnidadMedida             int                     `xml:"UnidadMedida,omitempty" json:"UnidadMedida,omitempty"`
	CantidadReferencia       Dec                     `xml:"CantidadReferencia,omitempty" json:"CantidadReferencia,omitempty"`
	UnidadReferencia         int                     `xml:"UnidadReferencia,omitempty" json:"UnidadReferencia,omitempty"`
	TablaSubcantidad         *TablaSubcantidad       `xml:"TablaSubcantidad,omitempty" json:"TablaSubcantidad,omitempty"`
	GradosAlcohol            Dec                     `xml:"GradosAlcohol,omitempty" json:"GradosAlcohol,omitempty"`
	PrecioUnitarioReferencia Dec                     `xml:"PrecioUnitarioReferencia,omitempty" json:"PrecioUnitarioReferencia,omitempty"`
	FechaElaboracion         string                  `xml:"FechaElaboracion,omitempty" json:"FechaElaboracion,omitempty"`
	FechaVencimientoItem     string                  `xml:"FechaVencimientoItem,omitempty" json:"FechaVencimientoItem,omitempty"`
	Mineria                  *Mineria                `xml:"Mineria,omitempty" json:"Mineria,omitempty"`
	PrecioUnitarioItem       decimal.Decimal         `xml:"PrecioUnitarioItem" json:"PrecioUnitarioItem"`
	DescuentoMonto           Dec                     `xml:"DescuentoMonto,omitempty" json:"DescuentoMonto,omitempty"`
	TablaSubDescuento        *TablaSubDescuento      `xml:"TablaSubDescuento,omitempty" json:"TablaSubDescuento,omitempty"`
	RecargoMonto             Dec                     `xml:"RecargoMonto,omitempty" json:"RecargoMonto,omitempty"`
	TablaSubRecargo          *TablaSubRecargo        `xml:"TablaSubRecargo,omitempty" json:"TablaSubRecargo,omitempty"`
	TablaImpuestoAdicional   *TablaImpuestoAdicional `xml:"TablaImpuestoAdicional,omitempty" json:"TablaImpuestoAdicional,omitempty"`
	OtraMonedaDetalle        *OtraMonedaDetalle      `xml:"OtraMonedaDetalle,omitempty" json:"OtraMonedaDetalle,omitempty"`
	MontoItem                decimal.Decimal         `xml:"MontoItem" json:"MontoItem"`
}

// TablaCodigosItem holds up to 5 item codes.
type TablaCodigosItem struct {
	CodigosItem []CodigosItem `xml:"CodigosItem" json:"CodigosItem"`
}

// CodigosItem is a code type / code pair (EAN, PLU, DUN, Interna...).
type CodigosItem struct {
	TipoCodigo string `xml:"TipoCodigo" json:"TipoCodigo"`
	CodigoItem string `xml:"CodigoItem" json:"CodigoItem"`
}

// Retencion holds withholding information for the item.
type Retencion struct {
	IndicadorAgenteRetencionoPercepcion int `xml:"IndicadorAgenteRetencionoPercepcion" json:"IndicadorAgenteRetencionoPercepcion"`
	MontoITBISRetenido                  Dec `xml:"MontoITBISRetenido,omitempty" json:"MontoITBISRetenido,omitempty"`
	MontoISRRetenido                    Dec `xml:"MontoISRRetenido,omitempty" json:"MontoISRRetenido,omitempty"`
}

// TablaSubcantidad holds up to 5 sub-quantities.
type TablaSubcantidad struct {
	SubcantidadItem []SubcantidadItem `xml:"SubcantidadItem" json:"SubcantidadItem"`
}

// SubcantidadItem is a sub-quantity / unit code pair.
type SubcantidadItem struct {
	Subcantidad       decimal.Decimal `xml:"Subcantidad" json:"Subcantidad"`
	CodigoSubcantidad int             `xml:"CodigoSubcantidad" json:"CodigoSubcantidad"`
}

// Mineria carries mining-specific data.
type Mineria struct {
	PesoNetoKilogramo Dec `xml:"PesoNetoKilogramo,omitempty" json:"PesoNetoKilogramo,omitempty"`
	PesoNetoMineria   Dec `xml:"PesoNetoMineria,omitempty" json:"PesoNetoMineria,omitempty"`
	TipoAfiliacion    int `xml:"TipoAfiliacion,omitempty" json:"TipoAfiliacion,omitempty"`
	Liquidacion       int `xml:"Liquidacion,omitempty" json:"Liquidacion,omitempty"`
}

// TablaSubDescuento holds up to 12 item sub-discounts.
type TablaSubDescuento struct {
	SubDescuento []SubAjuste `xml:"SubDescuento" json:"SubDescuento"`
}

// TablaSubRecargo holds up to 12 item sub-surcharges.
type TablaSubRecargo struct {
	SubRecargo []SubRecargo `xml:"SubRecargo" json:"SubRecargo"`
}

// SubAjuste is a sub-discount: "$" (amount) or "%" (percentage).
type SubAjuste struct {
	TipoSubDescuento       string          `xml:"TipoSubDescuento" json:"TipoSubDescuento"`
	SubDescuentoPorcentaje Dec             `xml:"SubDescuentoPorcentaje,omitempty" json:"SubDescuentoPorcentaje,omitempty"`
	MontoSubDescuento      decimal.Decimal `xml:"MontoSubDescuento" json:"MontoSubDescuento"`
}

// SubRecargo is a sub-surcharge: "$" (amount) or "%" (percentage).
type SubRecargo struct {
	TipoSubRecargo       string          `xml:"TipoSubRecargo" json:"TipoSubRecargo"`
	SubRecargoPorcentaje Dec             `xml:"SubRecargoPorcentaje,omitempty" json:"SubRecargoPorcentaje,omitempty"`
	MontoSubRecargo      decimal.Decimal `xml:"MontoSubRecargo" json:"MontoSubRecargo"`
}

// TablaImpuestoAdicional holds up to 2 additional tax codes for the item.
type TablaImpuestoAdicional struct {
	ImpuestoAdicional []ImpuestoAdicionalItem `xml:"ImpuestoAdicional" json:"ImpuestoAdicional"`
}

// ImpuestoAdicionalItem references a code in Tabla I.
type ImpuestoAdicionalItem struct {
	TipoImpuesto string `xml:"TipoImpuesto" json:"TipoImpuesto"`
}

// OtraMonedaDetalle carries item amounts in foreign currency.
type OtraMonedaDetalle struct {
	PrecioOtraMoneda    decimal.Decimal `xml:"PrecioOtraMoneda" json:"PrecioOtraMoneda"`
	DescuentoOtraMoneda Dec             `xml:"DescuentoOtraMoneda,omitempty" json:"DescuentoOtraMoneda,omitempty"`
	RecargoOtraMoneda   Dec             `xml:"RecargoOtraMoneda,omitempty" json:"RecargoOtraMoneda,omitempty"`
	MontoItemOtraMoneda decimal.Decimal `xml:"MontoItemOtraMoneda" json:"MontoItemOtraMoneda"`
}

// Subtotales is section C (informative subtotals).
type Subtotales struct {
	Subtotal []Subtotal `xml:"Subtotal" json:"Subtotal"`
}

// Subtotal is an informative subtotal.
type Subtotal struct {
	NumeroSubTotal            int    `xml:"NumeroSubTotal" json:"NumeroSubTotal"`
	DescripcionSubtotal       string `xml:"DescripcionSubtotal,omitempty" json:"DescripcionSubtotal,omitempty"`
	Orden                     int    `xml:"Orden,omitempty" json:"Orden,omitempty"`
	SubTotalMontoGravadoTotal Dec    `xml:"SubTotalMontoGravadoTotal,omitempty" json:"SubTotalMontoGravadoTotal,omitempty"`
	SubTotalMontoGravadoI1    Dec    `xml:"SubTotalMontoGravadoI1,omitempty" json:"SubTotalMontoGravadoI1,omitempty"`
	SubTotalMontoGravadoI2    Dec    `xml:"SubTotalMontoGravadoI2,omitempty" json:"SubTotalMontoGravadoI2,omitempty"`
	SubTotalMontoGravadoI3    Dec    `xml:"SubTotalMontoGravadoI3,omitempty" json:"SubTotalMontoGravadoI3,omitempty"`
	SubTotaITBIS              Dec    `xml:"SubTotaITBIS,omitempty" json:"SubTotaITBIS,omitempty"`
	SubTotaITBIS1             Dec    `xml:"SubTotaITBIS1,omitempty" json:"SubTotaITBIS1,omitempty"`
	SubTotaITBIS2             Dec    `xml:"SubTotaITBIS2,omitempty" json:"SubTotaITBIS2,omitempty"`
	SubTotaITBIS3             Dec    `xml:"SubTotaITBIS3,omitempty" json:"SubTotaITBIS3,omitempty"`
	SubTotalImpuestoAdicional Dec    `xml:"SubTotalImpuestoAdicional,omitempty" json:"SubTotalImpuestoAdicional,omitempty"`
	SubTotalExento            Dec    `xml:"SubTotalExento,omitempty" json:"SubTotalExento,omitempty"`
	MontoSubTotal             Dec    `xml:"MontoSubTotal,omitempty" json:"MontoSubTotal,omitempty"`
	Lineas                    int    `xml:"Lineas,omitempty" json:"Lineas,omitempty"`
}

// DescuentosORecargos is section D (global discounts / surcharges).
type DescuentosORecargos struct {
	DescuentoORecargo []DescuentoORecargo `xml:"DescuentoORecargo" json:"DescuentoORecargo"`
}

// DescuentoORecargo is a global discount ("D") or surcharge ("R").
type DescuentoORecargo struct {
	NumeroLinea                           int             `xml:"NumeroLinea" json:"NumeroLinea"`
	TipoAjuste                            string          `xml:"TipoAjuste" json:"TipoAjuste"`
	IndicadorNorma1007                    *int            `xml:"IndicadorNorma1007,omitempty" json:"IndicadorNorma1007,omitempty"`
	DescripcionDescuentooRecargo          string          `xml:"DescripcionDescuentooRecargo,omitempty" json:"DescripcionDescuentooRecargo,omitempty"`
	TipoValor                             string          `xml:"TipoValor,omitempty" json:"TipoValor,omitempty"`
	ValorDescuentooRecargo                Dec             `xml:"ValorDescuentooRecargo,omitempty" json:"ValorDescuentooRecargo,omitempty"`
	MontoDescuentooRecargo                decimal.Decimal `xml:"MontoDescuentooRecargo" json:"MontoDescuentooRecargo"`
	MontoDescuentooRecargoOtraMoneda      Dec             `xml:"MontoDescuentooRecargoOtraMoneda,omitempty" json:"MontoDescuentooRecargoOtraMoneda,omitempty"`
	IndicadorFacturacionDescuentooRecargo *int            `xml:"IndicadorFacturacionDescuentooRecargo,omitempty" json:"IndicadorFacturacionDescuentooRecargo,omitempty"`
}

// Paginacion is section E (printed representation pagination).
type Paginacion struct {
	Pagina []Pagina `xml:"Pagina" json:"Pagina"`
}

// Pagina describes a printed page.
type Pagina struct {
	PaginaNo                        int                        `xml:"PaginaNo" json:"PaginaNo"`
	NoLineaDesde                    int                        `xml:"NoLineaDesde" json:"NoLineaDesde"`
	NoLineaHasta                    int                        `xml:"NoLineaHasta" json:"NoLineaHasta"`
	SubtotalMontoGravadoPagina      Dec                        `xml:"SubtotalMontoGravadoPagina,omitempty" json:"SubtotalMontoGravadoPagina,omitempty"`
	SubtotalMontoGravado1Pagina     Dec                        `xml:"SubtotalMontoGravado1Pagina,omitempty" json:"SubtotalMontoGravado1Pagina,omitempty"`
	SubtotalMontoGravado2Pagina     Dec                        `xml:"SubtotalMontoGravado2Pagina,omitempty" json:"SubtotalMontoGravado2Pagina,omitempty"`
	SubtotalMontoGravado3Pagina     Dec                        `xml:"SubtotalMontoGravado3Pagina,omitempty" json:"SubtotalMontoGravado3Pagina,omitempty"`
	SubtotalExentoPagina            Dec                        `xml:"SubtotalExentoPagina,omitempty" json:"SubtotalExentoPagina,omitempty"`
	SubtotalItbisPagina             Dec                        `xml:"SubtotalItbisPagina,omitempty" json:"SubtotalItbisPagina,omitempty"`
	SubtotalItbis1Pagina            Dec                        `xml:"SubtotalItbis1Pagina,omitempty" json:"SubtotalItbis1Pagina,omitempty"`
	SubtotalItbis2Pagina            Dec                        `xml:"SubtotalItbis2Pagina,omitempty" json:"SubtotalItbis2Pagina,omitempty"`
	SubtotalItbis3Pagina            Dec                        `xml:"SubtotalItbis3Pagina,omitempty" json:"SubtotalItbis3Pagina,omitempty"`
	SubtotalImpuestoAdicionalPagina Dec                        `xml:"SubtotalImpuestoAdicionalPagina,omitempty" json:"SubtotalImpuestoAdicionalPagina,omitempty"`
	SubtotalImpuestoAdicional       *SubtotalImpuestoAdicional `xml:"SubtotalImpuestoAdicional,omitempty" json:"SubtotalImpuestoAdicional,omitempty"`
	MontoSubtotalPagina             Dec                        `xml:"MontoSubtotalPagina,omitempty" json:"MontoSubtotalPagina,omitempty"`
	SubtotalMontoNoFacturablePagina Dec                        `xml:"SubtotalMontoNoFacturablePagina,omitempty" json:"SubtotalMontoNoFacturablePagina,omitempty"`
}

// SubtotalImpuestoAdicional breaks down additional taxes for a page.
type SubtotalImpuestoAdicional struct {
	SubtotalImpuestoSelectivoConsumoEspecificoPagina Dec `xml:"SubtotalImpuestoSelectivoConsumoEspecificoPagina,omitempty" json:"SubtotalImpuestoSelectivoConsumoEspecificoPagina,omitempty"`
	SubtotalOtrosImpuesto                            Dec `xml:"SubtotalOtrosImpuesto,omitempty" json:"SubtotalOtrosImpuesto,omitempty"`
}

// InformacionReferencia is section F: the document being modified by a
// credit/debit note, or the contingency document being replaced.
type InformacionReferencia struct {
	NCFModificado        string `xml:"NCFModificado" json:"NCFModificado"`
	RNCOtroContribuyente string `xml:"RNCOtroContribuyente,omitempty" json:"RNCOtroContribuyente,omitempty"`
	FechaNCFModificado   string `xml:"FechaNCFModificado" json:"FechaNCFModificado"`
	CodigoModificacion   int    `xml:"CodigoModificacion" json:"CodigoModificacion"`
	RazonModificacion    string `xml:"RazonModificacion,omitempty" json:"RazonModificacion,omitempty"`
}
