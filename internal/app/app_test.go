package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

const (
	rncEmisor   = "131880738"
	rncReceptor = "101672919"
)

// fakeDGII emulates the DGII services used along the e-CF life cycle.
type fakeDGII struct {
	mu           sync.Mutex
	urls         map[string]string // RNC → receiver base URL
	aprobaciones int
}

func (f *fakeDGII) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /testecf/autenticacion/api/autenticacion/semilla", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<SemillaModel><valor>seed</valor><fecha>2026-10-01T10:00:00</fecha></SemillaModel>`)
	})
	mux.HandleFunc("POST /testecf/autenticacion/api/autenticacion/validarsemilla", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"token":"tok","expira":"2099-01-01T00:00:00Z","expedido":"2026-10-01T10:00:00Z"}`)
	})
	verify := func(r *http.Request) bool {
		f, _, err := r.FormFile("xml")
		if err != nil {
			return false
		}
		b, _ := io.ReadAll(f)
		_, err = xmldsig.Verify(b)
		return err == nil
	}
	mux.HandleFunc("POST /testecf/recepcion/api/facturaselectronicas", func(w http.ResponseWriter, r *http.Request) {
		if !verify(r) {
			w.WriteHeader(400)
			io.WriteString(w, `{"error":"firma","mensaje":"La firma del XML no es válida"}`)
			return
		}
		io.WriteString(w, `{"trackId":"T-`+strings.TrimSuffix(r.MultipartForm.File["xml"][0].Filename, ".xml")+`"}`)
	})
	mux.HandleFunc("POST /testecf/recepcionfc/api/recepcion/ecf", func(w http.ResponseWriter, r *http.Request) {
		if !verify(r) {
			w.WriteHeader(400)
			return
		}
		io.WriteString(w, `{"codigo":1,"estado":"Aceptado","mensajes":[],"encf":"E320000000001","secuenciaUtilizada":true}`)
	})
	mux.HandleFunc("GET /testecf/consultaresultado/api/consultas/estado", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"trackId":"`+r.URL.Query().Get("trackid")+`","codigo":1,"estado":"Aceptado","secuenciaUtilizada":true,"mensajes":[]}`)
	})
	mux.HandleFunc("GET /testecf/consultadirectorio/api/consultas/obtenerdirectorioporrnc", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		u, ok := f.urls[r.URL.Query().Get("RNC")]
		f.mu.Unlock()
		if !ok {
			io.WriteString(w, `[]`)
			return
		}
		json.NewEncoder(w).Encode([]dgii.Contribuyente{{RNC: r.URL.Query().Get("RNC"), URLRecepcion: u, URLAceptacion: u, URLOpcional: u}})
	})
	mux.HandleFunc("POST /testecf/aprobacioncomercial/api/aprobacioncomercial", func(w http.ResponseWriter, r *http.Request) {
		if !verify(r) {
			w.WriteHeader(400)
			return
		}
		f.mu.Lock()
		f.aprobaciones++
		f.mu.Unlock()
		io.WriteString(w, `{"mensaje":[],"estado":"Aprobación comercial aprobada","codigo":"1"}`)
	})
	return mux
}

func newInstance(t *testing.T, ctx context.Context, rnc string, ep dgii.Endpoints) (*httptest.Server, *App) {
	t.Helper()
	signer, err := xmldsig.NewTestSigner(rnc, "e2e "+rnc)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(ctx, Config{Environment: dgii.PreCertificacion, Endpoints: &ep, RNC: rnc, Signer: signer,
		Store: store.NewMemory(), APIKey: "k", ReceptorAuth: true, TrackerInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler)
	t.Cleanup(srv.Close)
	return srv, a
}

func call(t *testing.T, method, url string, body []byte, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("X-API-Key", "k")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: %d %s", method, url, resp.StatusCode, b)
		}
	}
	return resp.StatusCode
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEndToEndOperatingModel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fake := &fakeDGII{urls: map[string]string{}}
	dgiiSrv := httptest.NewServer(fake.handler(t))
	defer dgiiSrv.Close()
	ep := dgii.EndpointsFor(dgii.PreCertificacion)
	for _, p := range []*string{&ep.Autenticacion, &ep.Recepcion, &ep.RecepcionFC, &ep.ConsultaResultado, &ep.ConsultaDirectorio, &ep.AprobacionComercial} {
		*p = strings.NewReplacer(dgii.HostECF, dgiiSrv.URL, dgii.HostFC, dgiiSrv.URL).Replace(*p)
	}

	issuer, _ := newInstance(t, ctx, rncEmisor, ep)
	buyer, _ := newInstance(t, ctx, rncReceptor, ep)
	fake.urls[rncEmisor], fake.urls[rncReceptor] = issuer.URL, buyer.URL

	// 1-2. Issue and submit to DGII; wait for the TrackId result.
	body, _ := os.ReadFile("../../examples/factura-credito-fiscal-31.json")
	var res struct {
		Archivo     string `json:"archivo"`
		Seguimiento struct {
			Estado  string `json:"estado"`
			TrackID string `json:"trackId"`
		} `json:"seguimiento"`
	}
	if code := call(t, "POST", issuer.URL+"/api/v1/ecf/emitir?enviar=true&esperar=5", body, &res); code != http.StatusCreated {
		t.Fatalf("emitir: %d %+v", code, res)
	}
	if res.Seguimiento.Estado != "aceptado" || res.Seguimiento.TrackID == "" {
		t.Fatalf("expected aceptado, got %+v", res.Seguimiento)
	}

	// 3-4. Delivered to the electronic buyer, which answers with an ARECF.
	type estado struct {
		Entrega *struct {
			Estado string `json:"estado"`
		} `json:"entrega"`
		Aprobacion *struct {
			Estado int `json:"estado"`
		} `json:"aprobacionComercial"`
	}
	eventually(t, "delivery to buyer", func() bool {
		var st estado
		call(t, "GET", issuer.URL+"/api/v1/ecf/"+res.Archivo+"/estado", nil, &st)
		return st.Entrega != nil && st.Entrega.Estado == "entregado"
	})
	var recibidos []struct {
		Archivo string `json:"archivo"`
	}
	call(t, "GET", buyer.URL+"/api/v1/recibidos", nil, &recibidos)
	if len(recibidos) != 1 || recibidos[0].Archivo != res.Archivo {
		t.Fatalf("buyer did not receive the e-CF: %+v", recibidos)
	}

	// 5-6. Commercial approval to the issuer and DGII.
	var apr struct {
		Emisor struct {
			Estado, Error string
		} `json:"emisor"`
		DGII struct {
			Estado, Error string
		} `json:"dgii"`
	}
	if code := call(t, "POST", buyer.URL+"/api/v1/recibidos/"+res.Archivo+"/aprobacion?enviar=true", []byte(`{"estado":1}`), &apr); code != http.StatusCreated {
		t.Fatalf("aprobacion: %d %+v", code, apr)
	}
	if apr.Emisor.Estado != "entregada" || apr.DGII.Error != "" {
		t.Fatalf("approval delivery: %+v", apr)
	}
	eventually(t, "approval registered by issuer", func() bool {
		var st estado
		call(t, "GET", issuer.URL+"/api/v1/ecf/"+res.Archivo+"/estado", nil, &st)
		return st.Aprobacion != nil && st.Aprobacion.Estado == 1
	})

	// Printed representation.
	req, _ := http.NewRequest("GET", issuer.URL+"/api/v1/ecf/"+res.Archivo+"/pdf", nil)
	req.Header.Set("X-API-Key", "k")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	pdf, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf: %d", resp.StatusCode)
	}

	// Duplicate e-NCF is refused.
	if code := call(t, "POST", issuer.URL+"/api/v1/ecf/emitir", body, nil); code != http.StatusConflict {
		t.Fatalf("duplicate: %d", code)
	}

	// Consumer invoice below RD$250,000 goes to DGII as RFCE.
	body32, _ := os.ReadFile("../../examples/factura-consumo-32.json")
	var res32 struct {
		EsResumen   bool `json:"esResumen"`
		Seguimiento struct {
			Estado string `json:"estado"`
		} `json:"seguimiento"`
	}
	if code := call(t, "POST", issuer.URL+"/api/v1/ecf/emitir?enviar=true&esperar=5", body32, &res32); code != http.StatusCreated {
		t.Fatalf("emitir 32: %d", code)
	}
	if !res32.EsResumen || res32.Seguimiento.Estado != "aceptado" {
		t.Fatalf("RFCE flow: %+v", res32)
	}
}
