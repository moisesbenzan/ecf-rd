package xmldsig

import (
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// Signer holds the private key and certificate used to sign documents.
type Signer struct {
	Key         crypto.Signer
	Certificate *x509.Certificate
}

// LoadPKCS12File loads a .p12/.pfx digital certificate issued by a DGII
// authorized certification authority.
func LoadPKCS12File(path, password string) (*Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("xmldsig: leyendo certificado: %w", err)
	}
	return LoadPKCS12(data, password)
}

// LoadPKCS12 decodes a PKCS#12 bundle.
func LoadPKCS12(data []byte, password string) (*Signer, error) {
	key, cert, _, err := pkcs12.DecodeChain(data, password)
	if err != nil {
		return nil, fmt.Errorf("xmldsig: decodificando PKCS#12: %w", err)
	}
	return NewSigner(key, cert)
}

// NewSigner builds a Signer from an RSA private key and its certificate.
func NewSigner(key any, cert *x509.Certificate) (*Signer, error) {
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("xmldsig: DGII requiere una llave RSA")
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok || pub.N.Cmp(rk.N) != 0 {
		return nil, errors.New("xmldsig: la llave privada no corresponde al certificado")
	}
	return &Signer{Key: rk, Certificate: cert}, nil
}

// SubjectID returns the identity in the certificate subject "SN"
// (serialNumber) attribute, which DGII requires to be the owner's RNC,
// cédula or passport. Non-alphanumeric prefixes such as "IDCDO-" are removed.
func SubjectID(cert *x509.Certificate) string {
	sn := cert.Subject.SerialNumber
	if i := strings.LastIndexAny(sn, "-: "); i >= 0 {
		sn = sn[i+1:]
	}
	return sn
}

// CheckValidity reports an error when the certificate is not valid at t.
func CheckValidity(cert *x509.Certificate, t time.Time) error {
	if t.Before(cert.NotBefore) {
		return fmt.Errorf("xmldsig: el certificado aún no es válido (desde %s)", cert.NotBefore.Format(time.RFC3339))
	}
	if t.After(cert.NotAfter) {
		return fmt.Errorf("xmldsig: el certificado expiró el %s", cert.NotAfter.Format(time.RFC3339))
	}
	return nil
}
