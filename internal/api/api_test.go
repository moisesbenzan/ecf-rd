package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/emision"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	signer, err := xmldsig.NewTestSigner("131880738", "api test")
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	svc := &emision.Service{RNC: "131880738", Signer: signer, Store: st, Endpoints: dgii.EndpointsFor(dgii.PreCertificacion)}
	mux := http.NewServeMux()
	(&API{Emision: svc, Store: st, APIKey: "secret"}).Register(mux)
	srv := httptest.NewServer(httpx.CaseInsensitive(mux))
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url string, body []byte) (*http.Response, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "secret")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp, out
}

func TestEmitirConsumoGeneraResumen(t *testing.T) {
	srv := newTestServer(t)
	body, _ := os.ReadFile("../../examples/factura-consumo-32.json")
	resp, out := post(t, srv.URL+"/api/v1/ecf/emitir", body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status %d: %v", resp.StatusCode, out)
	}
	if out["esResumen"] != true || !strings.HasPrefix(out["xmlResumen"].(string), `<?xml version="1.0" encoding="utf-8"?><RFCE>`) {
		t.Fatalf("expected RFCE summary: %v", out)
	}
	code := out["codigoSeguridad"].(string)
	if len(code) != 6 || !strings.Contains(out["xmlResumen"].(string), "<CodigoSeguridadeCF>"+code+"</CodigoSeguridadeCF>") {
		t.Fatalf("security code not propagated to RFCE")
	}
	if _, err := xmldsig.Verify([]byte(out["xml"].(string))); err != nil {
		t.Fatal(err)
	}

	// The stored document is retrievable (case-insensitive).
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/API/V1/ECF/131880738e320000000001", nil)
	req.Header.Set("X-API-Key", "secret")
	r2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("get stored: %d", r2.StatusCode)
	}
}

func TestEmitirRechazaInvalido(t *testing.T) {
	srv := newTestServer(t)
	body, _ := os.ReadFile("../../examples/factura-credito-fiscal-31.json")
	body = bytes.Replace(body, []byte(`"RNCComprador": "101672919",`), nil, 1)
	resp, out := post(t, srv.URL+"/api/v1/ecf/emitir", body)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status %d: %v", resp.StatusCode, out)
	}
	if !strings.Contains(mustJSON(out["detalle"]), "Comprador.RNCComprador") {
		t.Fatalf("expected RNCComprador finding: %v", out)
	}
}

func TestUnknownFieldsRejected(t *testing.T) {
	srv := newTestServer(t)
	resp, out := post(t, srv.URL+"/api/v1/ecf/validar", []byte(`{"Encabezado":{"IdDocs":{}}}`))
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("misspelled tag must be rejected, got %d %v", resp.StatusCode, out)
	}
}

func TestAPIKeyRequired(t *testing.T) {
	srv := newTestServer(t)
	resp, err := http.Post(srv.URL+"/api/v1/ecf/validar", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
