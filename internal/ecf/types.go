package ecf

import (
	"fmt"
	"strconv"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/decimal"
)

// Version is the only format version currently published by DGII.
const Version = "1.0"

// TipoECF is the electronic receipt type code.
type TipoECF int

// e-CF types defined by DGII.
const (
	FacturaCreditoFiscal TipoECF = 31
	FacturaConsumo       TipoECF = 32
	NotaDebito           TipoECF = 33
	NotaCredito          TipoECF = 34
	Compras              TipoECF = 41
	GastosMenores        TipoECF = 43
	RegimenesEspeciales  TipoECF = 44
	Gubernamental        TipoECF = 45
	Exportaciones        TipoECF = 46
	PagosExterior        TipoECF = 47
)

var tipoNames = map[TipoECF]string{
	FacturaCreditoFiscal: "Factura de Crédito Fiscal Electrónica",
	FacturaConsumo:       "Factura de Consumo Electrónica",
	NotaDebito:           "Nota de Débito Electrónica",
	NotaCredito:          "Nota de Crédito Electrónica",
	Compras:              "Compras Electrónico",
	GastosMenores:        "Gastos Menores Electrónico",
	RegimenesEspeciales:  "Regímenes Especiales Electrónico",
	Gubernamental:        "Gubernamental Electrónico",
	Exportaciones:        "Comprobante de Exportaciones Electrónico",
	PagosExterior:        "Comprobante para Pagos al Exterior Electrónico",
}

// Valid reports whether t is a known e-CF type.
func (t TipoECF) Valid() bool { _, ok := tipoNames[t]; return ok }

// Name returns the official description of the type (used in printed
// representations).
func (t TipoECF) Name() string { return tipoNames[t] }

// String returns the two-digit code.
func (t TipoECF) String() string { return strconv.Itoa(int(t)) }

// Tipos returns every known type in ascending order.
func Tipos() []TipoECF {
	return []TipoECF{31, 32, 33, 34, 41, 43, 44, 45, 46, 47}
}

// IndicadorFacturacion classifies an item for ITBIS purposes.
type IndicadorFacturacion int

// Item billing indicators.
const (
	NoFacturable IndicadorFacturacion = 0
	ITBISTasa1   IndicadorFacturacion = 1 // 18%
	ITBISTasa2   IndicadorFacturacion = 2 // 16%
	ITBISTasa3   IndicadorFacturacion = 3 // 0%
	Exento       IndicadorFacturacion = 4
)

// ITBIS rates as published in the e-CF format (Tasa 1, 2 and 3).
var (
	TasaITBIS1 = decimal.MustParse("0.18")
	TasaITBIS2 = decimal.MustParse("0.16")
	TasaITBIS3 = decimal.Zero
)

// UmbralConsumo is the amount (RD$250,000.00) below which a Factura de
// Consumo Electrónica is reported to DGII through the RFCE summary service
// instead of the full e-CF reception service.
var UmbralConsumo = decimal.FromInt(250000)

// DateLayout is the dd-MM-yyyy layout used for dates in e-CF documents.
const DateLayout = "02-01-2006"

// DateTimeLayout is the dd-MM-yyyy HH:mm:ss layout used for timestamps.
const DateTimeLayout = "02-01-2006 15:04:05"

// Location is the time zone DGII uses for timestamps (GMT-4, no DST).
var Location = time.FixedZone("AST", -4*60*60)

// Now returns the current time in the DGII time zone.
func Now() time.Time { return time.Now().In(Location) }

// FormatDate formats t as dd-MM-yyyy.
func FormatDate(t time.Time) string { return t.In(Location).Format(DateLayout) }

// FormatDateTime formats t as dd-MM-yyyy HH:mm:ss in GMT-4.
func FormatDateTime(t time.Time) string { return t.In(Location).Format(DateTimeLayout) }

// ParseDate parses a dd-MM-yyyy date.
func ParseDate(s string) (time.Time, error) {
	return time.ParseInLocation(DateLayout, s, Location)
}

// ParseDateTime parses a dd-MM-yyyy HH:mm:ss timestamp.
func ParseDateTime(s string) (time.Time, error) {
	return time.ParseInLocation(DateTimeLayout, s, Location)
}

// ENCF is a parsed electronic fiscal receipt number: a series letter (E-Z
// except P; DGII currently issues E), the two-digit type and a 10-digit
// sequence (13 characters in total), e.g. E310000000001.
type ENCF struct {
	Serie     byte
	Tipo      TipoECF
	Secuencia int64
}

// ParseENCF validates and parses an e-NCF.
func ParseENCF(s string) (ENCF, error) {
	if len(s) != 13 {
		return ENCF{}, fmt.Errorf("e-NCF %q debe tener 13 caracteres", s)
	}
	serie := s[0]
	if serie >= 'a' && serie <= 'z' {
		serie -= 'a' - 'A'
	}
	if serie < 'E' || serie > 'Z' || serie == 'P' {
		return ENCF{}, fmt.Errorf("e-NCF %q: la serie debe ser una letra entre E y Z (excepto P)", s)
	}
	for i := 1; i < 13; i++ {
		if s[i] < '0' || s[i] > '9' {
			return ENCF{}, fmt.Errorf("e-NCF %q contiene caracteres no numéricos", s)
		}
	}
	tipo, _ := strconv.Atoi(s[1:3])
	seq, _ := strconv.ParseInt(s[3:], 10, 64)
	t := TipoECF(tipo)
	if !t.Valid() {
		return ENCF{}, fmt.Errorf("e-NCF %q tiene un tipo de comprobante inválido (%02d)", s, tipo)
	}
	if seq == 0 {
		return ENCF{}, fmt.Errorf("e-NCF %q tiene una secuencia inválida", s)
	}
	return ENCF{Serie: serie, Tipo: t, Secuencia: seq}, nil
}

// String formats the e-NCF.
func (e ENCF) String() string {
	serie := e.Serie
	if serie == 0 {
		serie = 'E'
	}
	return fmt.Sprintf("%c%02d%010d", serie, int(e.Tipo), e.Secuencia)
}

// ValidRNCFormat reports whether s has the structure of an RNC (9 digits) or
// a cédula (11 digits).
func ValidRNCFormat(s string) bool {
	if len(s) != 9 && len(s) != 11 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ValidRNCCheckDigit verifies the check digit of a 9-digit RNC or an
// 11-digit cédula. DGII has issued some historical identifiers that do not
// satisfy the algorithm, so callers should treat a failure as a warning.
func ValidRNCCheckDigit(s string) bool {
	if !ValidRNCFormat(s) {
		return false
	}
	if len(s) == 9 {
		weights := [8]int{7, 9, 8, 6, 5, 4, 3, 2}
		sum := 0
		for i, w := range weights {
			sum += int(s[i]-'0') * w
		}
		r := sum % 11
		var check int
		switch r {
		case 0:
			check = 2
		case 1:
			check = 1
		default:
			check = 11 - r
		}
		return check == int(s[8]-'0')
	}
	// Cédula: Luhn over the first 10 digits with weights 1,2,1,2...
	sum := 0
	for i := 0; i < 10; i++ {
		d := int(s[i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	check := (10 - sum%10) % 10
	return check == int(s[10]-'0')
}

// FileName returns the standard DGII file name for an XML document:
// RNC + e-NCF + ".xml" (e.g. 101672919E310000000001.xml).
func FileName(rnc, encf string) string { return rnc + encf + ".xml" }
