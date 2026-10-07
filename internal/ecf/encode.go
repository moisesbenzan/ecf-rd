package ecf

import (
	"bytes"
	"encoding/xml"
	"net/url"
	"strings"
)

// xmlHeader is the declaration used for every generated document.
const xmlHeader = `<?xml version="1.0" encoding="utf-8"?>`

// symbolEscaper replaces the symbols DGII asks to send as character
// references (see "Restricciones de Contenido y/o Caracteres en los XML").
// encoding/xml already escapes " ' < > &.
var symbolEscaper = strings.NewReplacer("©", "&#169;", "€", "&#8364;", "®", "&#174;")

// Marshal serializes a DGII document (ECF, RFCE, ARECF, ACECF, ANECF...) to
// compact UTF-8 XML with declaration. No indentation is emitted: DGII signs
// with preserveWhitespace=false, and compact output keeps the signed bytes
// identical to what receivers canonicalize.
//
// Empty optional elements are omitted, as required by DGII ("no deberá
// incluirse tags vacíos en los XML").
func Marshal(doc any) ([]byte, error) {
	var buf bytes.Buffer
	buf.Grow(4096)
	buf.WriteString(xmlHeader)
	enc := xml.NewEncoder(&buf)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if bytes.ContainsAny(out, "©€®") {
		out = []byte(symbolEscaper.Replace(string(out)))
	}
	return out, nil
}

// Unmarshal parses an e-CF XML document. The trailing Signature element is
// ignored by the model and must be verified separately.
func Unmarshal(data []byte) (*ECF, error) {
	var e ECF
	if err := xml.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

// TimbreURL returns the URL to encode in the QR code ("timbre") of the
// printed representation of an e-CF. host is the DGII consultatimbre base,
// e.g. https://ecf.dgii.gov.do/ecf/consultatimbre.
func TimbreURL(host string, e *ECF, codigoSeguridad string) string {
	h := &e.Encabezado
	// Built by hand to keep the parameter order shown in the specification.
	var b strings.Builder
	b.WriteString(host)
	b.WriteString("?rncemisor=")
	b.WriteString(url.QueryEscape(h.Emisor.RNCEmisor))
	if h.Comprador != nil && h.Comprador.RNCComprador != "" {
		b.WriteString("&rnccomprador=")
		b.WriteString(url.QueryEscape(h.Comprador.RNCComprador))
	}
	b.WriteString("&encf=")
	b.WriteString(url.QueryEscape(h.IdDoc.ENCF))
	b.WriteString("&fechaemision=")
	b.WriteString(url.QueryEscape(h.Emisor.FechaEmision))
	b.WriteString("&montototal=")
	b.WriteString(h.Totales.MontoTotal.String())
	b.WriteString("&fechafirma=")
	// Same encoding as DGII's example: "fechafirma=10-10-2020%2009:00:00".
	b.WriteString(strings.ReplaceAll(e.FechaHoraFirma, " ", "%20"))
	b.WriteString("&codigoseguridad=")
	b.WriteString(escapeSecurityCode(codigoSeguridad))
	return b.String()
}

// TimbreFCURL returns the QR URL for a consumer invoice reported through
// the RFCE service. host is e.g. https://fc.dgii.gov.do/ecf/consultatimbrefc.
func TimbreFCURL(host string, e *ECF, codigoSeguridad string) string {
	h := &e.Encabezado
	return host + "?rncemisor=" + url.QueryEscape(h.Emisor.RNCEmisor) +
		"&encf=" + url.QueryEscape(h.IdDoc.ENCF) +
		"&montototal=" + h.Totales.MontoTotal.String() +
		"&codigoseguridad=" + escapeSecurityCode(codigoSeguridad)
}

// escapeSecurityCode percent-encodes every character DGII lists as reserved
// for the QR security code, including "-", ".", "_" which url.QueryEscape
// leaves untouched.
func escapeSecurityCode(s string) string {
	var b strings.Builder
	const hex = "0123456789ABCDEF"
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}
