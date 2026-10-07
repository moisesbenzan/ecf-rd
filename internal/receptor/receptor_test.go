package receptor

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

const (
	rncEmisor   = "131880738"
	rncReceptor = "101672919"
)

func signedECF(t *testing.T, signer *xmldsig.Signer) []byte {
	t.Helper()
	raw, err := os.ReadFile("../ecf/testdata/factura-credito-fiscal-31.json")
	if err != nil {
		t.Fatal(err)
	}
	var e ecf.ECF
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	if err := ecf.Complete(&e); err != nil {
		t.Fatal(err)
	}
	e.FechaHoraFirma = "31-10-2026 10:00:00"
	b, err := ecf.Marshal(&e)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := xmldsig.Sign(b, signer)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func newServer(t *testing.T, requireAuth bool) (*httptest.Server, store.Store) {
	t.Helper()
	receptorSigner, err := xmldsig.NewTestSigner(rncReceptor, "receptor")
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory()
	mux := http.NewServeMux()
	New(Config{RNC: rncReceptor, Signer: receptorSigner, Store: st, RequireAuth: requireAuth}).Register(mux)
	return httptest.NewServer(httpx.CaseInsensitive(mux)), st
}

func TestIssuerToReceiverRoundTrip(t *testing.T) {
	srv, st := newServer(t, true)
	defer srv.Close()

	emisorSigner, err := xmldsig.NewTestSigner(rncEmisor, "emisor")
	if err != nil {
		t.Fatal(err)
	}
	doc := signedECF(t, emisorSigner)
	client := dgii.NewReceptorClient(srv.Client(), emisorSigner)
	// Mixed-case host path: receivers must be case-insensitive.
	dest := dgii.Destino{URLRecepcion: srv.URL, URLAceptacion: srv.URL, URLOpcional: srv.URL}
	ctx := context.Background()

	ack, err := client.EnviarECF(ctx, dest, ecf.FileName(rncEmisor, "E310000000001"), doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := xmldsig.Verify(ack); err != nil {
		t.Fatalf("ARECF must be signed: %v", err)
	}
	var arecf ecf.ARECF
	if err := xml.Unmarshal(ack, &arecf); err != nil {
		t.Fatal(err)
	}
	d := arecf.DetalleAcusedeRecibo
	if d.Estado != ecf.AcuseRecibido || d.ENCF != "E310000000001" || d.RNCEmisor != rncEmisor || d.RNCComprador != rncReceptor {
		t.Fatalf("unexpected acuse %+v", d)
	}
	if ok, _ := st.Exists(ctx, store.RecibidoECF, "131880738E310000000001.xml"); !ok {
		t.Fatal("received e-CF not stored")
	}

	// Duplicate submission → no recibido, motivo 3.
	ack, err = client.EnviarECF(ctx, dest, "dup.xml", doc)
	if err != nil {
		t.Fatal(err)
	}
	_ = xml.Unmarshal(ack, &arecf)
	if arecf.DetalleAcusedeRecibo.Estado != ecf.AcuseNoRecibido || arecf.DetalleAcusedeRecibo.CodigoMotivoNoRecibido != ecf.MotivoEnvioDuplicado {
		t.Fatalf("expected duplicate rejection, got %+v", arecf.DetalleAcusedeRecibo)
	}

	// Tampered document → motivo 2 (firma).
	tampered := []byte(strings.Replace(string(doc), "E310000000001", "E310000000002", 1))
	ack, _ = client.EnviarECF(ctx, dest, "t.xml", tampered)
	_ = xml.Unmarshal(ack, &arecf)
	if arecf.DetalleAcusedeRecibo.CodigoMotivoNoRecibido != ecf.MotivoErrorFirma {
		t.Fatalf("expected signature rejection, got %+v", arecf.DetalleAcusedeRecibo)
	}
}

func TestRecepcionRequiresToken(t *testing.T) {
	srv, _ := newServer(t, true)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/FE/Recepcion/API/ECF", "application/xml", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}
