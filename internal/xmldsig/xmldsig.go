// Package xmldsig signs and verifies DGII documents with enveloped XML
// Digital Signatures.
//
// The produced structure follows DGII's "Firmado de e-CF" guide exactly:
//
//	<Signature xmlns="http://www.w3.org/2000/09/xmldsig#">
//	  <SignedInfo>
//	    <CanonicalizationMethod Algorithm="http://www.w3.org/TR/2001/REC-xml-c14n-20010315"/>
//	    <SignatureMethod Algorithm="http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"/>
//	    <Reference URI="">
//	      <Transforms>
//	        <Transform Algorithm="http://www.w3.org/2000/09/xmldsig#enveloped-signature"/>
//	      </Transforms>
//	      <DigestMethod Algorithm="http://www.w3.org/2001/04/xmlenc#sha256"/>
//	      <DigestValue>...</DigestValue>
//	    </Reference>
//	  </SignedInfo>
//	  <SignatureValue>...</SignatureValue>
//	  <KeyInfo><X509Data><X509Certificate>...</X509Certificate></X509Data></KeyInfo>
//	</Signature>
//
// The signature is appended as the last child of the document element, with
// no namespace prefix, and covers the whole document (Reference URI="").
package xmldsig

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"
)

// Algorithm identifiers.
const (
	NamespaceDSig       = "http://www.w3.org/2000/09/xmldsig#"
	AlgC14N10           = "http://www.w3.org/TR/2001/REC-xml-c14n-20010315"
	AlgC14N10Comments   = "http://www.w3.org/TR/2001/REC-xml-c14n-20010315#WithComments"
	AlgExcC14N          = "http://www.w3.org/2001/10/xml-exc-c14n#"
	AlgRSASHA256        = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"
	AlgSHA256           = "http://www.w3.org/2001/04/xmlenc#sha256"
	AlgEnvelopedSig     = "http://www.w3.org/2000/09/xmldsig#enveloped-signature"
	xmlDeclaration      = `<?xml version="1.0" encoding="utf-8"?>`
	securityCodeLength  = 6
	signatureElementTag = "Signature"
)

var c14n = dsig.MakeC14N10RecCanonicalizer()

// ErrAlreadySigned is returned when signing a document that already carries
// an enveloped signature.
var ErrAlreadySigned = errors.New("xmldsig: el documento ya está firmado")

// ErrNoSignature is returned when verifying an unsigned document.
var ErrNoSignature = errors.New("xmldsig: el documento no contiene una firma")

// Sign appends an enveloped signature to the XML document in data and returns
// the signed document (with XML declaration, no indentation).
func Sign(data []byte, signer *Signer) ([]byte, error) {
	doc, err := parse(data)
	if err != nil {
		return nil, err
	}
	root := doc.Root()
	if findSignature(root) != nil {
		return nil, ErrAlreadySigned
	}

	digest, err := digestElement(root)
	if err != nil {
		return nil, err
	}

	sig := etree.NewElement("Signature")
	sig.CreateAttr("xmlns", NamespaceDSig)
	si := sig.CreateElement("SignedInfo")
	si.CreateElement("CanonicalizationMethod").CreateAttr("Algorithm", AlgC14N10)
	si.CreateElement("SignatureMethod").CreateAttr("Algorithm", AlgRSASHA256)
	ref := si.CreateElement("Reference")
	ref.CreateAttr("URI", "")
	ref.CreateElement("Transforms").CreateElement("Transform").CreateAttr("Algorithm", AlgEnvelopedSig)
	ref.CreateElement("DigestMethod").CreateAttr("Algorithm", AlgSHA256)
	ref.CreateElement("DigestValue").SetText(base64.StdEncoding.EncodeToString(digest))

	// The SignedInfo is canonicalized as it will appear in the final
	// document: inheriting the dsig default namespace from <Signature> and
	// any namespace declarations of the document element.
	canonSI, err := canonicalizeInContext(si, root, sig)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(canonSI)
	sigValue, err := signer.Key.Sign(nil, h[:], crypto.SHA256)
	if err != nil {
		return nil, fmt.Errorf("xmldsig: firmando: %w", err)
	}
	sig.CreateElement("SignatureValue").SetText(base64.StdEncoding.EncodeToString(sigValue))
	x509Data := sig.CreateElement("KeyInfo").CreateElement("X509Data")
	x509Data.CreateElement("X509Certificate").SetText(base64.StdEncoding.EncodeToString(signer.Certificate.Raw))

	root.AddChild(sig)
	return serialize(doc)
}

// SecurityCode returns the "código de seguridad" of a signed document: the
// first six characters of its SignatureValue. It is printed in the QR code
// of the printed representation and sent in RFCE summaries.
func SecurityCode(signed []byte) (string, error) {
	sv, err := SignatureValue(signed)
	if err != nil {
		return "", err
	}
	if len(sv) < securityCodeLength {
		return "", errors.New("xmldsig: SignatureValue demasiado corto")
	}
	return sv[:securityCodeLength], nil
}

// SignatureValue extracts the base64 SignatureValue text of a signed document.
// It uses a byte scan instead of a full parse, since it is called on hot paths
// right after signing.
func SignatureValue(signed []byte) (string, error) {
	i := bytes.Index(signed, []byte("SignatureValue"))
	if i < 0 {
		return "", ErrNoSignature
	}
	rest := signed[i:]
	start := bytes.IndexByte(rest, '>')
	if start < 0 {
		return "", ErrNoSignature
	}
	rest = rest[start+1:]
	end := bytes.IndexByte(rest, '<')
	if end < 0 {
		return "", ErrNoSignature
	}
	return strings.Join(strings.Fields(string(rest[:end])), ""), nil
}

// Verify checks the enveloped signature of data and returns the signing
// certificate. Certificate trust (chain, revocation, ownership) is not
// evaluated; callers must apply their own policy to the returned certificate.
func Verify(data []byte) (*x509.Certificate, error) {
	doc, err := parse(data)
	if err != nil {
		return nil, err
	}
	root := doc.Root()
	sig := findSignature(root)
	if sig == nil {
		return nil, ErrNoSignature
	}
	si := child(sig, "SignedInfo")
	if si == nil {
		return nil, errors.New("xmldsig: falta SignedInfo")
	}
	if alg := attr(child(si, "CanonicalizationMethod"), "Algorithm"); alg != AlgC14N10 {
		return nil, fmt.Errorf("xmldsig: algoritmo de canonicalización no soportado: %q", alg)
	}
	if alg := attr(child(si, "SignatureMethod"), "Algorithm"); alg != AlgRSASHA256 {
		return nil, fmt.Errorf("xmldsig: algoritmo de firma no soportado: %q", alg)
	}
	ref := child(si, "Reference")
	if ref == nil {
		return nil, errors.New("xmldsig: falta Reference")
	}
	if uri := attr(ref, "URI"); uri != "" {
		return nil, fmt.Errorf("xmldsig: Reference URI debe estar vacío, se encontró %q", uri)
	}
	if alg := attr(child(ref, "DigestMethod"), "Algorithm"); alg != AlgSHA256 {
		return nil, fmt.Errorf("xmldsig: algoritmo de digest no soportado: %q", alg)
	}
	if tr := child(ref, "Transforms"); tr != nil {
		for _, t := range tr.ChildElements() {
			switch a := attr(t, "Algorithm"); a {
			case AlgEnvelopedSig, AlgC14N10:
			default:
				return nil, fmt.Errorf("xmldsig: transformación no soportada: %q", a)
			}
		}
	}
	wantDigest, err := base64.StdEncoding.DecodeString(strings.TrimSpace(text(child(ref, "DigestValue"))))
	if err != nil {
		return nil, fmt.Errorf("xmldsig: DigestValue inválido: %w", err)
	}
	sigValue, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(text(child(sig, "SignatureValue"))), ""))
	if err != nil {
		return nil, fmt.Errorf("xmldsig: SignatureValue inválido: %w", err)
	}
	certText := ""
	if ki := child(sig, "KeyInfo"); ki != nil {
		if xd := child(ki, "X509Data"); xd != nil {
			certText = text(child(xd, "X509Certificate"))
		}
	}
	if certText == "" {
		return nil, errors.New("xmldsig: falta X509Certificate en KeyInfo")
	}
	certDER, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(certText), ""))
	if err != nil {
		return nil, fmt.Errorf("xmldsig: X509Certificate inválido: %w", err)
	}
	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, fmt.Errorf("xmldsig: X509Certificate inválido: %w", err)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("xmldsig: el certificado no contiene una llave RSA")
	}

	// 1. SignedInfo signature.
	canonSI, err := canonicalizeInContext(si, root, sig)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(canonSI)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, h[:], sigValue); err != nil {
		return nil, errors.New("xmldsig: la firma del documento no es válida")
	}

	// 2. Reference digest over the document without the signature.
	unsigned := root.Copy()
	if s := findSignature(unsigned); s != nil {
		unsigned.RemoveChild(s)
	}
	got, err := digestElement(unsigned)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(got, wantDigest) {
		// Signers using preserveWhitespace=false digest the document without
		// insignificant whitespace; retry in that form.
		stripWhitespace(unsigned)
		got, err = digestElement(unsigned)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(got, wantDigest) {
			return nil, errors.New("xmldsig: el digest no coincide; el documento fue alterado")
		}
	}
	return cert, nil
}

func parse(data []byte) (*etree.Document, error) {
	doc := etree.NewDocument()
	doc.ReadSettings.PreserveCData = true
	if err := doc.ReadFromBytes(data); err != nil {
		return nil, fmt.Errorf("xmldsig: XML inválido: %w", err)
	}
	if doc.Root() == nil {
		return nil, errors.New("xmldsig: XML sin elemento raíz")
	}
	return doc, nil
}

func serialize(doc *etree.Document) ([]byte, error) {
	// Drop any existing declaration and write a canonical one.
	for _, t := range doc.Child {
		if p, ok := t.(*etree.ProcInst); ok && p.Target == "xml" {
			doc.RemoveChild(p)
		}
	}
	doc.WriteSettings.CanonicalEndTags = false
	doc.WriteSettings.CanonicalText = true
	doc.WriteSettings.CanonicalAttrVal = true
	var buf bytes.Buffer
	buf.WriteString(xmlDeclaration)
	if _, err := doc.WriteTo(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func digestElement(el *etree.Element) ([]byte, error) {
	canon, err := c14n.Canonicalize(el)
	if err != nil {
		return nil, fmt.Errorf("xmldsig: canonicalizando: %w", err)
	}
	h := sha256.Sum256(canon)
	return h[:], nil
}

// canonicalizeInContext canonicalizes el (a descendant of sig, itself a
// child of root) as a document subset under inclusive C14N 1.0, which
// requires every namespace declaration in scope to be rendered on el.
func canonicalizeInContext(el, root, sig *etree.Element) ([]byte, error) {
	detached := el.Copy()
	declared := map[string]bool{}
	for _, a := range detached.Attr {
		if a.Space == "xmlns" || (a.Space == "" && a.Key == "xmlns") {
			declared[a.FullKey()] = true
		}
	}
	// Closest scope wins: Signature first, then the document element.
	for _, scope := range []*etree.Element{sig, root} {
		for _, a := range scope.Attr {
			isNS := a.Space == "xmlns" || (a.Space == "" && a.Key == "xmlns")
			if !isNS || declared[a.FullKey()] {
				continue
			}
			declared[a.FullKey()] = true
			if a.Space == "" {
				detached.CreateAttr("xmlns", a.Value)
			} else {
				detached.CreateAttr("xmlns:"+a.Key, a.Value)
			}
		}
	}
	out, err := c14n.Canonicalize(detached)
	if err != nil {
		return nil, fmt.Errorf("xmldsig: canonicalizando SignedInfo: %w", err)
	}
	return out, nil
}

func findSignature(root *etree.Element) *etree.Element {
	for _, c := range root.ChildElements() {
		if c.Tag == signatureElementTag && (c.Space == "" || c.NamespaceURI() == NamespaceDSig) {
			return c
		}
	}
	return nil
}

func child(el *etree.Element, tag string) *etree.Element {
	if el == nil {
		return nil
	}
	for _, c := range el.ChildElements() {
		if c.Tag == tag {
			return c
		}
	}
	return nil
}

func attr(el *etree.Element, key string) string {
	if el == nil {
		return ""
	}
	return el.SelectAttrValue(key, "")
}

func text(el *etree.Element) string {
	if el == nil {
		return ""
	}
	return el.Text()
}

// stripWhitespace removes whitespace-only text nodes recursively.
func stripWhitespace(el *etree.Element) {
	hasElements := len(el.ChildElements()) > 0
	kept := make([]etree.Token, 0, len(el.Child))
	for _, t := range el.Child {
		switch n := t.(type) {
		case *etree.CharData:
			if hasElements && strings.TrimSpace(n.Data) == "" {
				continue
			}
		case *etree.Element:
			stripWhitespace(n)
		}
		kept = append(kept, t)
	}
	el.Child = kept
}
