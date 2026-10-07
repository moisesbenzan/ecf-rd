package xmldsig

import (
	"bytes"
	"crypto/x509"
	"strings"
	"testing"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

const sample = `<?xml version="1.0" encoding="utf-8"?><ECF><Encabezado><Version>1.0</Version><IdDoc><TipoeCF>31</TipoeCF><eNCF>E310000000001</eNCF></IdDoc><Emisor><RazonSocialEmisor>Doña &amp; Hijos, S.R.L.</RazonSocialEmisor></Emisor></Encabezado><FechaHoraFirma>01-04-2025 10:00:00</FechaHoraFirma></ECF>`

func testSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := NewTestSigner("130000001", "Test Emisor")
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSignVerifyRoundTrip(t *testing.T) {
	s := testSigner(t)
	signed, err := Sign([]byte(sample), s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(signed, []byte(`<?xml version="1.0" encoding="utf-8"?><ECF>`)) {
		t.Fatalf("unexpected prefix: %s", signed[:60])
	}
	if !bytes.Contains(signed, []byte(`<Signature xmlns="http://www.w3.org/2000/09/xmldsig#"><SignedInfo>`)) {
		t.Fatalf("signature structure not as expected:\n%s", signed)
	}
	if !bytes.HasSuffix(signed, []byte(`</Signature></ECF>`)) {
		t.Fatalf("signature must be last child of root")
	}
	cert, err := Verify(signed)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if SubjectID(cert) != "130000001" {
		t.Fatalf("subject id = %q", SubjectID(cert))
	}
	code, err := SecurityCode(signed)
	if err != nil || len(code) != 6 {
		t.Fatalf("security code %q %v", code, err)
	}
	if _, err := Sign(signed, s); err != ErrAlreadySigned {
		t.Fatalf("expected ErrAlreadySigned, got %v", err)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	signed, err := Sign([]byte(sample), testSigner(t))
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(signed, []byte("E310000000001"), []byte("E310000000002"), 1)
	if _, err := Verify(tampered); err == nil {
		t.Fatal("expected verification failure on tampered document")
	}
	if _, err := Verify([]byte(sample)); err != ErrNoSignature {
		t.Fatalf("expected ErrNoSignature, got %v", err)
	}
}

func TestVerifyToleratesIndentation(t *testing.T) {
	// A document signed without whitespace and later pretty-printed outside
	// the signature should still verify (preserveWhitespace=false semantics).
	signed, err := Sign([]byte(sample), testSigner(t))
	if err != nil {
		t.Fatal(err)
	}
	pretty := strings.Replace(string(signed), "<Encabezado>", "<Encabezado>\n  ", 1)
	if _, err := Verify([]byte(pretty)); err != nil {
		t.Fatalf("verify indented: %v", err)
	}
}

// TestIndependentVerifier validates our signatures with goxmldsig's own
// verifier, an implementation independent from ours.
func TestIndependentVerifier(t *testing.T) {
	s := testSigner(t)
	signed, err := Sign([]byte(sample), s)
	if err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(signed); err != nil {
		t.Fatal(err)
	}
	ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{s.Certificate}})
	if _, err := ctx.Validate(doc.Root()); err != nil {
		t.Fatalf("goxmldsig rejected our signature: %v", err)
	}
}

func TestVerifyWithRootNamespaces(t *testing.T) {
	// ARECF documents commonly carry xsi/xsd declarations on the root; they
	// are in scope for the inclusive canonicalization of SignedInfo.
	in := `<?xml version="1.0" encoding="utf-8"?><ARECF xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema"><DetalleAcusedeRecibo><Version>1.0</Version></DetalleAcusedeRecibo></ARECF>`
	s := testSigner(t)
	signed, err := Sign([]byte(in), s)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(signed); err != nil {
		t.Fatal(err)
	}
	doc := etree.NewDocument()
	_ = doc.ReadFromBytes(signed)
	ctx := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{s.Certificate}})
	if _, err := ctx.Validate(doc.Root()); err != nil {
		t.Fatalf("goxmldsig rejected our signature: %v", err)
	}
}

func BenchmarkSign(b *testing.B) {
	s, _ := NewTestSigner("130000001", "Bench")
	data := []byte(sample)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := Sign(data, s); err != nil {
			b.Fatal(err)
		}
	}
}
