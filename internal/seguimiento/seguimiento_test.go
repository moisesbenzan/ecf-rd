package seguimiento

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/receptor"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); c.t = c.t.Add(d); c.mu.Unlock() }
func newClock() *clock               { return &clock{t: time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)} }

type fakeDGII struct {
	down      atomic.Int32 // number of upcoming 503 responses
	enProceso atomic.Int32 // number of "en proceso" answers before accepting
	receptor  string
}

func (f *fakeDGII) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /testecf/autenticacion/api/autenticacion/semilla", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<SemillaModel><valor>abc</valor><fecha>2026-10-01T10:00:00</fecha></SemillaModel>`)
	})
	mux.HandleFunc("POST /testecf/autenticacion/api/autenticacion/validarsemilla", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"token":"tok","expira":"2099-01-01T00:00:00Z","expedido":"2026-10-01T10:00:00Z"}`)
	})
	mux.HandleFunc("POST /testecf/recepcion/api/facturaselectronicas", func(w http.ResponseWriter, r *http.Request) {
		if f.down.Load() > 0 {
			f.down.Add(-1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		io.WriteString(w, `{"trackId":"T-1"}`)
	})
	mux.HandleFunc("GET /testecf/consultaresultado/api/consultas/estado", func(w http.ResponseWriter, r *http.Request) {
		if f.enProceso.Load() > 0 {
			f.enProceso.Add(-1)
			io.WriteString(w, `{"trackId":"T-1","codigo":3,"estado":"En Proceso"}`)
			return
		}
		io.WriteString(w, `{"trackId":"T-1","codigo":1,"estado":"Aceptado","secuenciaUtilizada":true,"mensajes":[]}`)
	})
	mux.HandleFunc("GET /testecf/consultadirectorio/api/consultas/obtenerdirectorioporrnc", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("RNC") != "101672919" {
			io.WriteString(w, `[]`)
			return
		}
		json.NewEncoder(w).Encode([]dgii.Contribuyente{{Nombre: "Cliente", RNC: "101672919", URLRecepcion: f.receptor, URLAceptacion: f.receptor}})
	})
	return mux
}

func signedInvoice(t *testing.T, s *xmldsig.Signer) []byte {
	t.Helper()
	raw, _ := os.ReadFile("../../examples/factura-credito-fiscal-31.json")
	var e ecf.ECF
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if err := ecf.Complete(&e); err != nil {
		t.Fatal(err)
	}
	e.FechaHoraFirma = "01-10-2026 10:00:00"
	b, _ := ecf.Marshal(&e)
	signed, err := xmldsig.Sign(b, s)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestLifecycleWithContingencyAndDelivery(t *testing.T) {
	ctx := context.Background()
	// Buyer's receiver.
	buyerSigner, _ := xmldsig.NewTestSigner("101672919", "buyer")
	buyerStore := store.NewMemory()
	bmux := http.NewServeMux()
	receptor.New(receptor.Config{RNC: "101672919", Signer: buyerSigner, Store: buyerStore}).Register(bmux)
	buyer := httptest.NewServer(httpx.CaseInsensitive(bmux))
	defer buyer.Close()

	fake := &fakeDGII{receptor: buyer.URL}
	fake.down.Store(2)
	fake.enProceso.Store(1)
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	signer, _ := xmldsig.NewTestSigner("131880738", "issuer")
	ep := dgii.EndpointsFor(dgii.PreCertificacion)
	for _, p := range []*string{&ep.Autenticacion, &ep.Recepcion, &ep.ConsultaResultado, &ep.ConsultaDirectorio} {
		*p = strings.Replace(*p, dgii.HostECF, srv.URL, 1)
	}
	client := dgii.New(dgii.PreCertificacion, signer, dgii.WithEndpoints(ep))
	st := store.NewMemory()
	clk := newClock()
	tr := New(Config{Store: st, DGII: client, Receptor: dgii.NewReceptorClient(nil, signer), Now: clk.Now})

	archivo := "131880738E310000000001.xml"
	_ = st.Put(ctx, store.EmitidoECF, archivo, signedInvoice(t, signer))
	if err := tr.Submit(ctx, &Registro{Archivo: archivo, ENCF: "E310000000001", TipoeCF: 31, RNCEmisor: "131880738", RNCComprador: "101672919"}); err != nil {
		t.Fatal(err)
	}

	tr.Step(ctx) // 503 → contingency
	r, _ := tr.Get(archivo)
	if r.Estado != Pendiente || !r.Contingencia {
		t.Fatalf("expected contingency, got %+v", r)
	}
	tr.Step(ctx) // not due yet
	clk.Add(15 * time.Second)
	tr.Step(ctx) // second 503
	clk.Add(time.Minute)
	tr.Step(ctx) // accepted reception → TrackId
	r, _ = tr.Get(archivo)
	if r.Estado != EnProceso || r.TrackID != "T-1" || r.Contingencia {
		t.Fatalf("expected en_proceso, got %+v", r)
	}
	clk.Add(2 * time.Second)
	tr.Step(ctx) // "en proceso"
	clk.Add(5 * time.Second)
	tr.Step(ctx) // aceptado
	r, _ = tr.Get(archivo)
	if r.Estado != Aceptado {
		t.Fatalf("expected aceptado, got %+v", r)
	}
	tr.Step(ctx) // delivery
	r, _ = tr.Get(archivo)
	if r.Entrega == nil || r.Entrega.Estado != EntregaEntregado {
		t.Fatalf("expected delivery, got %+v", r.Entrega)
	}
	if ok, _ := buyerStore.Exists(ctx, store.RecibidoECF, archivo); !ok {
		t.Fatal("buyer did not store the e-CF")
	}
	if ok, _ := st.Exists(ctx, store.AcuseRecibido, archivo); !ok {
		t.Fatal("ARECF not stored")
	}

	// Commercial approval from the buyer.
	err := tr.RegistrarAprobacion(ctx, &ecf.ACECF{DetalleAprobacionComercial: ecf.DetalleAprobacionComercial{
		RNCEmisor: "131880738", ENCF: "E310000000001", Estado: ecf.AprobacionAceptado, FechaHoraAprobacionComercial: "02-10-2026 09:00:00"}})
	if err != nil {
		t.Fatal(err)
	}

	// Restart: state is reloaded from the store.
	tr2 := New(Config{Store: st, DGII: client, Now: clk.Now})
	if err := tr2.Load(ctx); err != nil {
		t.Fatal(err)
	}
	r, ok := tr2.Get(archivo)
	if !ok || r.Estado != Aceptado || r.Aprobacion == nil || r.Aprobacion.Estado != 1 {
		t.Fatalf("reloaded record mismatch: %+v", r)
	}
}

func TestNonElectronicBuyer(t *testing.T) {
	ctx := context.Background()
	fake := &fakeDGII{}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()
	signer, _ := xmldsig.NewTestSigner("131880738", "issuer")
	ep := dgii.EndpointsFor(dgii.PreCertificacion)
	for _, p := range []*string{&ep.Autenticacion, &ep.Recepcion, &ep.ConsultaResultado, &ep.ConsultaDirectorio} {
		*p = strings.Replace(*p, dgii.HostECF, srv.URL, 1)
	}
	st := store.NewMemory()
	clk := newClock()
	tr := New(Config{Store: st, DGII: dgii.New(dgii.PreCertificacion, signer, dgii.WithEndpoints(ep)), Receptor: dgii.NewReceptorClient(nil, signer), Now: clk.Now})
	archivo := "131880738E310000000002.xml"
	_ = st.Put(ctx, store.EmitidoECF, archivo, []byte("<ECF/>"))
	_ = tr.Submit(ctx, &Registro{Archivo: archivo, RNCComprador: "999999999"})
	for i := 0; i < 4; i++ {
		tr.Step(ctx)
		clk.Add(10 * time.Second)
	}
	r, _ := tr.Get(archivo)
	if r.Estado != Aceptado || r.Entrega.Estado != EntregaNoElectronico {
		t.Fatalf("got %+v / %+v", r, r.Entrega)
	}
}
