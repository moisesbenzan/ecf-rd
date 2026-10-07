package dgii

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sync"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// tokenMargin is how long before expiry a cached token is renewed.
const tokenMargin = 2 * time.Minute

// maxResponseBytes bounds response bodies read from remote services.
const maxResponseBytes = 8 << 20

// APIError is returned for non-2xx responses.
type APIError struct {
	StatusCode int
	Body       string
	URL        string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("dgii: %s respondió HTTP %d: %s", e.URL, e.StatusCode, e.Body)
}

// Client calls the DGII web services, handling authentication (semilla →
// firma → token) and token renewal transparently. It is safe for concurrent
// use.
type Client struct {
	endpoints Endpoints
	http      *http.Client
	signer    *xmldsig.Signer
	now       func() time.Time

	mu    sync.Mutex
	token *Token
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient sets the HTTP client used for requests.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// WithEndpoints overrides the service base URLs.
func WithEndpoints(e Endpoints) Option { return func(c *Client) { c.endpoints = e } }

// New creates a client for env that authenticates with signer.
func New(env Environment, signer *xmldsig.Signer, opts ...Option) *Client {
	c := &Client{
		endpoints: EndpointsFor(env),
		http: &http.Client{
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				MaxIdleConnsPerHost: 32,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
		},
		signer: signer,
		now:    time.Now,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Endpoints returns the configured base URLs.
func (c *Client) Endpoints() Endpoints { return c.endpoints }

// Authenticate obtains a new session token from DGII: it downloads the seed
// XML, signs it with the taxpayer certificate and exchanges it for a token.
func (c *Client) Authenticate(ctx context.Context) (*Token, error) {
	tok, err := authenticate(ctx, c.http, c.signer,
		c.endpoints.Autenticacion+"/api/autenticacion/semilla",
		c.endpoints.Autenticacion+"/api/autenticacion/validarsemilla")
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.token = tok
	c.mu.Unlock()
	return tok, nil
}

// Token returns a valid cached token, authenticating when needed.
func (c *Client) Token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token.Valid(c.now(), tokenMargin) {
		return c.token.Token, nil
	}
	tok, err := authenticate(ctx, c.http, c.signer,
		c.endpoints.Autenticacion+"/api/autenticacion/semilla",
		c.endpoints.Autenticacion+"/api/autenticacion/validarsemilla")
	if err != nil {
		return "", err
	}
	if tok.Expira.IsZero() {
		// Fall back to the documented one-hour lifetime.
		tok.Expira = c.now().Add(time.Hour)
	}
	c.token = tok
	return tok.Token, nil
}

func (c *Client) invalidate(token string) {
	c.mu.Lock()
	if c.token != nil && c.token.Token == token {
		c.token = nil
	}
	c.mu.Unlock()
}

// authenticate implements the seed/signature/token flow shared by DGII and
// by taxpayer receivers (validarsemilla vs validacioncertificado).
func authenticate(ctx context.Context, h *http.Client, signer *xmldsig.Signer, semillaURL, validarURL string) (*Token, error) {
	if signer == nil {
		return nil, errors.New("dgii: no hay certificado configurado para autenticarse")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, semillaURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/xml")
	seed, err := do(h, req)
	if err != nil {
		return nil, fmt.Errorf("dgii: obteniendo semilla: %w", err)
	}
	signed, err := xmldsig.Sign(seed, signer)
	if err != nil {
		return nil, fmt.Errorf("dgii: firmando semilla: %w", err)
	}
	req, err = newMultipart(ctx, validarURL, "semilla.xml", signed)
	if err != nil {
		return nil, err
	}
	body, err := do(h, req)
	if err != nil {
		return nil, fmt.Errorf("dgii: validando semilla: %w", err)
	}
	var tok Token
	if err := decode(body, &tok); err != nil {
		return nil, fmt.Errorf("dgii: respuesta de autenticación inválida: %w", err)
	}
	if tok.Token == "" {
		return nil, errors.New("dgii: la autenticación no retornó un token")
	}
	return &tok, nil
}

// EnviarECF submits a signed e-CF (all types, and type 32 ≥ RD$250,000) and
// returns the TrackId to poll with ConsultarResultado.
func (c *Client) EnviarECF(ctx context.Context, fileName string, signedXML []byte) (*RespuestaRecepcion, error) {
	var r RespuestaRecepcion
	err := c.postXML(ctx, c.endpoints.Recepcion+"/api/facturaselectronicas", fileName, signedXML, &r)
	return &r, err
}

// EnviarRFCE submits a signed consumer-invoice summary (type 32 below
// RD$250,000). The response carries the final validation state directly.
func (c *Client) EnviarRFCE(ctx context.Context, fileName string, signedXML []byte) (*RespuestaRFCE, error) {
	var r RespuestaRFCE
	err := c.postXML(ctx, c.endpoints.RecepcionFC+"/api/recepcion/ecf", fileName, signedXML, &r)
	return &r, err
}

// EnviarAprobacionComercial submits a signed ACECF to DGII.
func (c *Client) EnviarAprobacionComercial(ctx context.Context, fileName string, signedXML []byte) (*RespuestaAprobacionComercial, error) {
	var r RespuestaAprobacionComercial
	err := c.postXML(ctx, c.endpoints.AprobacionComercial+"/api/aprobacioncomercial", fileName, signedXML, &r)
	return &r, err
}

// AnularRangos submits a signed ANECF voiding unused e-NCF ranges.
func (c *Client) AnularRangos(ctx context.Context, fileName string, signedXML []byte) (*RespuestaAnulacionRango, error) {
	var r RespuestaAnulacionRango
	err := c.postXML(ctx, c.endpoints.AnulacionRangos+"/api/operaciones/anularrango", fileName, signedXML, &r)
	return &r, err
}

// ConsultarResultado returns the validation result of a submission.
func (c *Client) ConsultarResultado(ctx context.Context, trackID string) (*RespuestaConsultaResultado, error) {
	var r RespuestaConsultaResultado
	q := url.Values{"trackid": {trackID}}
	err := c.get(ctx, c.endpoints.ConsultaResultado+"/api/consultas/estado?"+q.Encode(), &r)
	return &r, err
}

// ConsultarEstado returns the status of an e-CF as seen by DGII.
// rncComprador and codigoSeguridad are conditional on the receipt type.
func (c *Client) ConsultarEstado(ctx context.Context, rncEmisor, encf, rncComprador, codigoSeguridad string) (*RespuestaConsultaEstado, error) {
	q := url.Values{"rncemisor": {rncEmisor}, "ncfelectronico": {encf}}
	if rncComprador != "" {
		q.Set("rnccomprador", rncComprador)
	}
	if codigoSeguridad != "" {
		q.Set("codigoseguridad", codigoSeguridad)
	}
	var r RespuestaConsultaEstado
	err := c.get(ctx, c.endpoints.ConsultaEstado+"/api/consultas/estado?"+q.Encode(), &r)
	return &r, err
}

// ConsultarTrackIDs lists every submission received for an e-NCF.
func (c *Client) ConsultarTrackIDs(ctx context.Context, rncEmisor, encf string) ([]TrackingDetalle, error) {
	q := url.Values{"rncemisor": {rncEmisor}, "encf": {encf}}
	var r []TrackingDetalle
	err := c.get(ctx, c.endpoints.ConsultaTrackIds+"/api/trackids/consulta?"+q.Encode(), &r)
	return r, err
}

// ConsultarRFCE returns the state of a consumer-invoice summary.
func (c *Client) ConsultarRFCE(ctx context.Context, rncEmisor, encf, codigoSeguridad string) (*RespuestaRFCE, error) {
	q := url.Values{"RNC_Emisor": {rncEmisor}, "ENCF": {encf}, "Cod_Seguridad_eCF": {codigoSeguridad}}
	var r RespuestaRFCE
	err := c.get(ctx, c.endpoints.ConsultaRFCE+"/api/Consultas/Consulta?"+q.Encode(), &r)
	return &r, err
}

// Directorio lists electronic taxpayers and their reception URLs.
func (c *Client) Directorio(ctx context.Context) ([]Contribuyente, error) {
	var r []Contribuyente
	err := c.get(ctx, c.endpoints.ConsultaDirectorio+"/api/consultas/listado", &r)
	return r, err
}

// DirectorioPorRNC returns the reception URLs of a single taxpayer.
func (c *Client) DirectorioPorRNC(ctx context.Context, rnc string) ([]Contribuyente, error) {
	var r []Contribuyente
	q := url.Values{"RNC": {rnc}}
	err := c.get(ctx, c.endpoints.ConsultaDirectorio+"/api/consultas/obtenerdirectorioporrnc?"+q.Encode(), &r)
	return r, err
}

func (c *Client) get(ctx context.Context, u string, out any) error {
	return c.withToken(ctx, func(token string) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	}, out)
}

func (c *Client) postXML(ctx context.Context, u, fileName string, data []byte, out any) error {
	return c.withToken(ctx, func(token string) (*http.Request, error) {
		req, err := newMultipart(ctx, u, fileName, data)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return req, nil
	}, out)
}

// withToken performs an authenticated request, re-authenticating once if
// DGII rejects the cached token.
func (c *Client) withToken(ctx context.Context, build func(token string) (*http.Request, error), out any) error {
	for attempt := 0; ; attempt++ {
		token, err := c.Token(ctx)
		if err != nil {
			return err
		}
		req, err := build(token)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		body, err := do(c.http, req)
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized && attempt == 0 {
			c.invalidate(token)
			continue
		}
		if err != nil {
			// DGII returns validation failures with 4xx and a JSON body
			// matching the success schema; decode it so callers see the
			// messages, but still report the error.
			if apiErr != nil && out != nil && len(apiErr.Body) > 0 {
				_ = decode([]byte(apiErr.Body), out)
			}
			return err
		}
		if out == nil {
			return nil
		}
		return decode(body, out)
	}
}

func newMultipart(ctx context.Context, u, fileName string, data []byte) (*http.Request, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="xml"; filename=%q`, fileName))
	h.Set("Content-Type", "text/xml")
	part, err := mw.CreatePart(h)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req, nil
}

func do(h *http.Client, req *http.Request) ([]byte, error) {
	resp, err := h.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return body, &APIError{StatusCode: resp.StatusCode, Body: string(body), URL: req.URL.Redacted()}
	}
	return body, nil
}

// decode accepts JSON or XML bodies (DGII services honour Accept but some
// return XML regardless).
func decode(body []byte, out any) error {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '<' {
		return xml.Unmarshal(trimmed, out)
	}
	return json.Unmarshal(trimmed, out)
}
