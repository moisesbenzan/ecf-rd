package dgii

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// fakeDGII emulates the authentication and reception services.
type fakeDGII struct {
	t        *testing.T
	auths    atomic.Int32
	reject   atomic.Bool // reject the next authorized call with 401
	lastFile atomic.Value
}

func (f *fakeDGII) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /testecf/autenticacion/api/autenticacion/semilla", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><SemillaModel xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xmlns:xsd="http://www.w3.org/2001/XMLSchema"><valor>abc123</valor><fecha>2026-10-31T10:00:00.0000000-04:00</fecha></SemillaModel>`)
	})
	mux.HandleFunc("POST /testecf/autenticacion/api/autenticacion/validarsemilla", func(w http.ResponseWriter, r *http.Request) {
		f2, _, err := r.FormFile("xml")
		if err != nil {
			http.Error(w, "no xml", 400)
			return
		}
		b, _ := io.ReadAll(f2)
		if _, err := xmldsig.Verify(b); err != nil {
			http.Error(w, "firma invalida: "+err.Error(), 400)
			return
		}
		if !strings.Contains(string(b), "<valor>abc123</valor>") {
			http.Error(w, "semilla alterada", 400)
			return
		}
		n := f.auths.Add(1)
		json.NewEncoder(w).Encode(map[string]string{
			"token":    "tok" + string(rune('0'+n)),
			"expira":   time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05Z"),
			"expedido": time.Now().UTC().Format("2006-01-02T15:04:05Z"),
		})
	})
	mux.HandleFunc("POST /testecf/recepcion/api/facturaselectronicas", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok") || f.reject.Swap(false) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, hdr, err := r.FormFile("xml")
		if err != nil {
			http.Error(w, "no xml", 400)
			return
		}
		f.lastFile.Store(hdr.Filename)
		json.NewEncoder(w).Encode(map[string]string{"trackId": "track-1"})
	})
	mux.HandleFunc("GET /testecf/consultaresultado/api/consultas/estado", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"trackId":"`+r.URL.Query().Get("trackid")+`","codigo":1,"estado":"Aceptado","rnc":"131880738","eNCF":"E310000000001","secuenciaUtilizada":true,"fechaRecepcion":"31/10/2026","mensajes":[{"valor":"","codigo":0}]}`)
	})
	return mux
}

func TestClientAuthAndSubmit(t *testing.T) {
	fake := &fakeDGII{t: t}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	signer, err := xmldsig.NewTestSigner("131880738", "test")
	if err != nil {
		t.Fatal(err)
	}
	ep := EndpointsFor(PreCertificacion)
	for _, p := range []*string{&ep.Autenticacion, &ep.Recepcion, &ep.ConsultaResultado} {
		*p = strings.Replace(*p, HostECF, srv.URL, 1)
	}
	c := New(PreCertificacion, signer, WithEndpoints(ep), WithHTTPClient(srv.Client()))
	ctx := context.Background()

	r, err := c.EnviarECF(ctx, "131880738E310000000001.xml", []byte("<ECF/>"))
	if err != nil {
		t.Fatal(err)
	}
	if r.TrackID != "track-1" || fake.lastFile.Load() != "131880738E310000000001.xml" {
		t.Fatalf("unexpected response %+v", r)
	}
	// Token is cached.
	if _, err := c.EnviarECF(ctx, "x.xml", []byte("<ECF/>")); err != nil {
		t.Fatal(err)
	}
	if n := fake.auths.Load(); n != 1 {
		t.Fatalf("expected 1 authentication, got %d", n)
	}
	// A 401 triggers one re-authentication and retry.
	fake.reject.Store(true)
	if _, err := c.EnviarECF(ctx, "x.xml", []byte("<ECF/>")); err != nil {
		t.Fatal(err)
	}
	if n := fake.auths.Load(); n != 2 {
		t.Fatalf("expected re-authentication, got %d", n)
	}

	res, err := c.ConsultarResultado(ctx, "track-1")
	if err != nil {
		t.Fatal(err)
	}
	if res.Codigo != EstadoAceptado || !res.Final() || res.ENCF != "E310000000001" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestEndpoints(t *testing.T) {
	ep := EndpointsFor(Produccion)
	if ep.Recepcion != "https://ecf.dgii.gov.do/ecf/recepcion" || ep.RecepcionFC != "https://fc.dgii.gov.do/ecf/recepcionfc" {
		t.Fatalf("unexpected endpoints %+v", ep)
	}
	if e, err := ParseEnvironment("CerteCF"); err != nil || e != Certificacion {
		t.Fatalf("parse env: %v %v", e, err)
	}
}
