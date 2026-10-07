package dgii

import (
	"context"
	"net/http"
	"strings"

	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// Standard resource paths every electronic receiver exposes (see
// "Descripción Técnica Servicios Emisores Electrónicos").
const (
	PathSemilla               = "/fe/autenticacion/api/semilla"
	PathValidacionCertificado = "/fe/autenticacion/api/validacioncertificado"
	PathRecepcion             = "/fe/recepcion/api/ecf"
	PathAprobacionComercial   = "/fe/aprobacioncomercial/api/ecf"
)

// ReceptorClient delivers e-CF and commercial approvals to another
// electronic taxpayer, using the URLs published in the DGII directory.
type ReceptorClient struct {
	http   *http.Client
	signer *xmldsig.Signer
}

// NewReceptorClient creates a client that authenticates with signer when the
// receiver publishes an authentication URL.
func NewReceptorClient(h *http.Client, signer *xmldsig.Signer) *ReceptorClient {
	if h == nil {
		h = http.DefaultClient
	}
	return &ReceptorClient{http: h, signer: signer}
}

// Destino describes a receiver, typically built from a Contribuyente
// directory entry.
type Destino struct {
	URLRecepcion  string
	URLAceptacion string
	URLOpcional   string // authentication host; empty when not used
}

// DestinoFrom converts a directory entry.
func DestinoFrom(c Contribuyente) Destino {
	return Destino{URLRecepcion: c.URLRecepcion, URLAceptacion: c.URLAceptacion, URLOpcional: c.URLOpcional}
}

// EnviarECF delivers a signed e-CF and returns the receiver's signed ARECF
// (acuse de recibo).
func (r *ReceptorClient) EnviarECF(ctx context.Context, d Destino, fileName string, signedXML []byte) ([]byte, error) {
	return r.post(ctx, d, join(d.URLRecepcion, PathRecepcion), fileName, signedXML)
}

// EnviarAprobacionComercial delivers a signed ACECF to the issuer.
func (r *ReceptorClient) EnviarAprobacionComercial(ctx context.Context, d Destino, fileName string, signedXML []byte) error {
	_, err := r.post(ctx, d, join(d.URLAceptacion, PathAprobacionComercial), fileName, signedXML)
	return err
}

func (r *ReceptorClient) post(ctx context.Context, d Destino, u, fileName string, data []byte) ([]byte, error) {
	var token string
	if d.URLOpcional != "" {
		tok, err := authenticate(ctx, r.http, r.signer, join(d.URLOpcional, PathSemilla), join(d.URLOpcional, PathValidacionCertificado))
		if err != nil {
			return nil, err
		}
		token = tok.Token
	}
	req, err := newMultipart(ctx, u, fileName, data)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "*/*")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return do(r.http, req)
}

// join appends a standard path to a published host URL, tolerating hosts
// that already include the path.
func join(base, path string) string {
	base = strings.TrimRight(base, "/")
	if strings.HasSuffix(strings.ToLower(base), strings.ToLower(path)) {
		return base
	}
	return base + path
}
