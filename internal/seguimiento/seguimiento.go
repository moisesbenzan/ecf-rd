// Package seguimiento tracks every issued e-CF through its life cycle
// ("Modelo de Operación", Informe Técnico e-CF secciones 8, 9 y 19):
//
//  1. submission to DGII (e-CF reception service, or RFCE for consumer
//     invoices below RD$250,000), retried while DGII or the network is
//     unavailable (contingency: documents must reach DGII within 72 hours);
//  2. polling of the validation result by TrackId until it is final
//     (Aceptado, Aceptado Condicional or Rechazado);
//  3. delivery of accepted e-CF to electronic buyers listed in the DGII
//     directory, storing the signed acuse de recibo (ARECF) they return;
//  4. registration of the commercial approval (ACECF) sent by the buyer.
//
// State is persisted as JSON documents in the store so the process resumes
// after restarts.
package seguimiento

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

// Estado is the DGII state of an issued document.
type Estado string

// States.
const (
	// Pendiente: signed and stored, not yet received by DGII (includes
	// contingency while DGII or the network is unavailable).
	Pendiente Estado = "pendiente"
	// EnProceso: DGII returned a TrackId and is validating.
	EnProceso Estado = "en_proceso"
	// Aceptado: the e-CF has fiscal validity.
	Aceptado Estado = "aceptado"
	// AceptadoCondicional: valid, with observations to correct in the future.
	AceptadoCondicional Estado = "aceptado_condicional"
	// Rechazado: void for tax purposes; issue a new e-CF.
	Rechazado Estado = "rechazado"
	// ErrorEnvio: DGII refused the submission (e.g. malformed request).
	ErrorEnvio Estado = "error_envio"
	// Anulado: the e-NCF was voided through the ANECF service before it
	// reached DGII (Informe Técnico, sección 10).
	Anulado Estado = "anulado"
)

// Final reports whether the DGII state no longer changes.
func (e Estado) Final() bool {
	switch e {
	case Aceptado, AceptadoCondicional, Rechazado, ErrorEnvio, Anulado:
		return true
	}
	return false
}

// Valido reports whether the document has fiscal validity.
func (e Estado) Valido() bool { return e == Aceptado || e == AceptadoCondicional }

// Delivery states.
const (
	EntregaPendiente      = "pendiente"
	EntregaEntregado      = "entregado"
	EntregaRechazada      = "no_recibido"    // the buyer answered ARECF Estado=1
	EntregaNoElectronico  = "no_electronico" // deliver the printed representation instead
	EntregaNoAplica       = "no_aplica"
	EntregaError          = "error"
	maxIntentosEntrega    = 12
	contingenciaPlazo     = 72 * time.Hour
	maxIntentosNoEncontro = 10
)

// Entrega describes the delivery of the e-CF to the buyer.
type Entrega struct {
	Estado         string    `json:"estado"`
	URLRecepcion   string    `json:"urlRecepcion,omitempty"`
	Intentos       int       `json:"intentos"`
	ProximoIntento time.Time `json:"proximoIntento,omitempty"`
	UltimoError    string    `json:"ultimoError,omitempty"`
	AcuseEstado    *int      `json:"acuseEstado,omitempty"`
	AcuseMotivo    int       `json:"acuseMotivo,omitempty"`
	Fecha          time.Time `json:"fecha,omitempty"`
}

// Aprobacion is the commercial approval received from the buyer.
type Aprobacion struct {
	Estado   int       `json:"estado"` // 1 aceptado, 2 rechazado
	Motivo   string    `json:"motivo,omitempty"`
	Fecha    string    `json:"fechaHora"`
	Recibida time.Time `json:"recibida"`
}

// Registro is the tracking record of an issued document.
type Registro struct {
	Archivo         string `json:"archivo"`
	ENCF            string `json:"eNCF"`
	TipoeCF         int    `json:"tipoeCF"`
	RNCEmisor       string `json:"rncEmisor"`
	RNCComprador    string `json:"rncComprador,omitempty"`
	FechaEmision    string `json:"fechaEmision"`
	FechaHoraFirma  string `json:"fechaHoraFirma"`
	MontoTotal      string `json:"montoTotal"`
	CodigoSeguridad string `json:"codigoSeguridad"`
	// Resumen is true when DGII receives the RFCE summary.
	Resumen bool `json:"resumen"`

	Estado             Estado         `json:"estado"`
	TrackID            string         `json:"trackId,omitempty"`
	CodigoDGII         *int           `json:"codigoDGII,omitempty"`
	EstadoDGII         string         `json:"estadoDGII,omitempty"`
	Mensajes           []dgii.Mensaje `json:"mensajes,omitempty"`
	SecuenciaUtilizada *bool          `json:"secuenciaUtilizada,omitempty"`
	FechaRecepcion     string         `json:"fechaRecepcion,omitempty"`

	// Contingencia is set while the document could not be sent to DGII.
	Contingencia      bool      `json:"contingencia"`
	VenceContingencia time.Time `json:"venceContingencia"`

	Intentos       int       `json:"intentos"`
	UltimoError    string    `json:"ultimoError,omitempty"`
	ProximoIntento time.Time `json:"proximoIntento,omitempty"`

	Entrega    *Entrega    `json:"entrega,omitempty"`
	Aprobacion *Aprobacion `json:"aprobacionComercial,omitempty"`

	Creado      time.Time `json:"creado"`
	Actualizado time.Time `json:"actualizado"`
}

func (r *Registro) clone() *Registro {
	c := *r
	c.Mensajes = append([]dgii.Mensaje(nil), r.Mensajes...)
	if r.Entrega != nil {
		e := *r.Entrega
		c.Entrega = &e
	}
	if r.Aprobacion != nil {
		a := *r.Aprobacion
		c.Aprobacion = &a
	}
	return &c
}

// due reports whether the record needs work at now.
func (r *Registro) due(now time.Time) bool {
	if !r.Estado.Final() {
		return !now.Before(r.ProximoIntento)
	}
	if r.Estado.Valido() && r.Entrega != nil && r.Entrega.Estado == EntregaPendiente {
		return !now.Before(r.Entrega.ProximoIntento)
	}
	return false
}

// Config configures a Tracker.
type Config struct {
	Store    store.Store
	DGII     *dgii.Client
	Receptor *dgii.ReceptorClient // nil disables delivery to buyers
	Logger   *slog.Logger
	// Concurrency bounds simultaneous DGII calls (default 8).
	Concurrency int
	// Interval between scans (default 1s).
	Interval time.Duration
	Now      func() time.Time
}

// Tracker drives tracking records. It is safe for concurrent use.
type Tracker struct {
	cfg  Config
	log  *slog.Logger
	now  func() time.Time
	wake chan struct{}

	mu      sync.Mutex
	regs    map[string]*Registro
	busy    map[string]bool
	waiters map[string][]chan struct{}

	dirMu sync.Mutex
	dir   map[string]dirEntry
}

type dirEntry struct {
	c   *dgii.Contribuyente
	exp time.Time
}

// New creates a Tracker; call Load then Run.
func New(cfg Config) *Tracker {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 8
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Tracker{
		cfg: cfg, log: log, now: cfg.Now, wake: make(chan struct{}, 1),
		regs: map[string]*Registro{}, busy: map[string]bool{}, waiters: map[string][]chan struct{}{},
		dir: map[string]dirEntry{},
	}
}

func recordName(archivo string) string { return strings.TrimSuffix(archivo, ".xml") + ".json" }

// Load reads persisted records.
func (t *Tracker) Load(ctx context.Context) error {
	names, err := t.cfg.Store.List(ctx, store.Estado)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, n := range names {
		b, err := t.cfg.Store.Get(ctx, store.Estado, n)
		if err != nil {
			return err
		}
		var r Registro
		if err := json.Unmarshal(b, &r); err != nil {
			t.log.Error("registro de seguimiento inválido", "archivo", n, "error", err)
			continue
		}
		t.regs[r.Archivo] = &r
	}
	return nil
}

func (t *Tracker) persist(ctx context.Context, r *Registro) error {
	r.Actualizado = t.now()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return t.cfg.Store.Put(ctx, store.Estado, recordName(r.Archivo), b)
}

// Submit registers a newly issued document for submission.
func (t *Tracker) Submit(ctx context.Context, r *Registro) error {
	now := t.now()
	r.Estado = Pendiente
	r.Creado, r.ProximoIntento = now, now
	r.VenceContingencia = now.Add(contingenciaPlazo)
	if !r.Resumen && r.RNCComprador != "" && t.cfg.Receptor != nil {
		r.Entrega = &Entrega{Estado: EntregaPendiente}
	} else {
		r.Entrega = &Entrega{Estado: EntregaNoAplica}
	}
	t.mu.Lock()
	t.regs[r.Archivo] = r
	err := t.persist(ctx, r)
	t.mu.Unlock()
	t.signal()
	return err
}

func (t *Tracker) signal() {
	select {
	case t.wake <- struct{}{}:
	default:
	}
}

// Get returns a copy of a record.
func (t *Tracker) Get(archivo string) (*Registro, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.regs[archivo]
	if !ok {
		return nil, false
	}
	return r.clone(), true
}

// List returns copies of the records in a state ("" = all), newest first.
func (t *Tracker) List(estado Estado, limit int) []*Registro {
	t.mu.Lock()
	out := make([]*Registro, 0, len(t.regs))
	for _, r := range t.regs {
		if estado == "" || r.Estado == estado {
			out = append(out, r.clone())
		}
	}
	t.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Creado.After(out[j].Creado) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Wait blocks until the record reaches a final DGII state or ctx ends, and
// returns the latest copy.
func (t *Tracker) Wait(ctx context.Context, archivo string) (*Registro, bool) {
	for {
		t.mu.Lock()
		r, ok := t.regs[archivo]
		if !ok {
			t.mu.Unlock()
			return nil, false
		}
		if r.Estado.Final() {
			c := r.clone()
			t.mu.Unlock()
			return c, true
		}
		ch := make(chan struct{})
		t.waiters[archivo] = append(t.waiters[archivo], ch)
		t.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return t.Get(archivo)
		}
	}
}

// Reintentar schedules a pending or failed submission for immediate retry.
func (t *Tracker) Reintentar(ctx context.Context, archivo string) error {
	t.mu.Lock()
	defer t.signal()
	defer t.mu.Unlock()
	r, ok := t.regs[archivo]
	if !ok {
		return store.ErrNotFound
	}
	switch r.Estado {
	case Pendiente, EnProceso, ErrorEnvio:
		if r.Estado == ErrorEnvio {
			r.Estado = Pendiente
		}
		r.ProximoIntento = t.now()
	default:
		return fmt.Errorf("el documento está en estado %s", r.Estado)
	}
	return t.persist(ctx, r)
}

// ReintentarEntrega schedules the delivery to the buyer again.
func (t *Tracker) ReintentarEntrega(ctx context.Context, archivo string) error {
	t.mu.Lock()
	defer t.signal()
	defer t.mu.Unlock()
	r, ok := t.regs[archivo]
	if !ok {
		return store.ErrNotFound
	}
	if r.Resumen || r.RNCComprador == "" || t.cfg.Receptor == nil {
		return errors.New("la entrega al receptor no aplica para este documento")
	}
	r.Entrega = &Entrega{Estado: EntregaPendiente, ProximoIntento: t.now()}
	return t.persist(ctx, r)
}

// RegistrarAprobacion records a commercial approval received for one of
// our documents.
func (t *Tracker) RegistrarAprobacion(ctx context.Context, a *ecf.ACECF) error {
	d := &a.DetalleAprobacionComercial
	archivo := ecf.FileName(d.RNCEmisor, d.ENCF)
	t.mu.Lock()
	defer t.mu.Unlock()
	r, ok := t.regs[archivo]
	if !ok {
		return store.ErrNotFound
	}
	r.Aprobacion = &Aprobacion{Estado: d.Estado, Motivo: d.DetalleMotivoRechazo, Fecha: d.FechaHoraAprobacionComercial, Recibida: t.now()}
	return t.persist(ctx, r)
}

// MarcarAnulados stops tracking pending documents whose e-NCF falls in a
// voided range and returns the affected files. inRange reports whether an
// e-NCF was voided.
func (t *Tracker) MarcarAnulados(ctx context.Context, rnc string, inRange func(encf string) bool) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for k, r := range t.regs {
		if r.RNCEmisor == rnc && (r.Estado == Pendiente || r.Estado == ErrorEnvio) && !t.busy[k] && inRange(r.ENCF) {
			r.Estado, r.UltimoError = Anulado, ""
			if r.Entrega != nil && r.Entrega.Estado == EntregaPendiente {
				r.Entrega.Estado = EntregaNoAplica
			}
			_ = t.persist(ctx, r)
			out = append(out, k)
		}
	}
	return out
}

// Run processes due records until ctx is cancelled.
func (t *Tracker) Run(ctx context.Context) {
	tick := time.NewTicker(t.cfg.Interval)
	defer tick.Stop()
	for {
		t.Step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-t.wake:
		}
	}
}

// Step processes every due record once and waits for completion.
func (t *Tracker) Step(ctx context.Context) {
	now := t.now()
	t.mu.Lock()
	var due []string
	for k, r := range t.regs {
		if !t.busy[k] && r.due(now) {
			t.busy[k] = true
			due = append(due, k)
		}
	}
	t.mu.Unlock()
	if len(due) == 0 {
		return
	}
	sem := make(chan struct{}, t.cfg.Concurrency)
	var wg sync.WaitGroup
	for _, k := range due {
		wg.Add(1)
		sem <- struct{}{}
		go func(k string) {
			defer wg.Done()
			defer func() { <-sem }()
			t.process(ctx, k)
		}(k)
	}
	wg.Wait()
}

func (t *Tracker) process(ctx context.Context, archivo string) {
	t.mu.Lock()
	r := t.regs[archivo].clone()
	t.mu.Unlock()

	switch {
	case r.Estado == Pendiente:
		t.enviar(ctx, r)
	case r.Estado == EnProceso:
		t.consultar(ctx, r)
	case r.Estado.Valido() && r.Entrega != nil && r.Entrega.Estado == EntregaPendiente:
		t.entregar(ctx, r)
	}

	t.mu.Lock()
	// Keep a commercial approval registered concurrently.
	if cur := t.regs[archivo]; cur != nil && cur.Aprobacion != nil && r.Aprobacion == nil {
		r.Aprobacion = cur.Aprobacion
	}
	t.regs[archivo] = r
	if err := t.persist(ctx, r); err != nil {
		t.log.Error("persistiendo seguimiento", "archivo", archivo, "error", err)
	}
	delete(t.busy, archivo)
	var ws []chan struct{}
	if r.Estado.Final() {
		ws = t.waiters[archivo]
		delete(t.waiters, archivo)
	}
	t.mu.Unlock()
	for _, w := range ws {
		close(w)
	}
	if r.Estado == EnProceso || (r.Estado.Valido() && r.Entrega != nil && r.Entrega.Estado == EntregaPendiente) {
		t.signal()
	}
}

var (
	backoffEnvio    = []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute}
	backoffConsulta = []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute}
	backoffEntrega  = []time.Duration{30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour}
)

func backoff(s []time.Duration, n int) time.Duration {
	if n <= 0 {
		return 0
	}
	if n > len(s) {
		return s[len(s)-1]
	}
	return s[n-1]
}

// retryable reports whether a submission error is transient (network,
// timeouts, 5xx, 429) and therefore puts the document in contingency.
func retryable(err error) bool {
	var apiErr *dgii.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= 500 || apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode == http.StatusRequestTimeout
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	return !errors.Is(err, context.Canceled)
}

func (t *Tracker) enviar(ctx context.Context, r *Registro) {
	r.Intentos++
	kind := store.EmitidoECF
	if r.Resumen {
		kind = store.EmitidoRFCE
	}
	data, err := t.cfg.Store.Get(ctx, kind, r.Archivo)
	if err != nil {
		r.Estado, r.UltimoError = ErrorEnvio, "documento firmado no encontrado: "+err.Error()
		return
	}
	if r.Resumen {
		resp, err := t.cfg.DGII.EnviarRFCE(ctx, r.Archivo, data)
		if err != nil && (resp == nil || resp.Estado == "") {
			t.fallaEnvio(r, err)
			return
		}
		r.Contingencia, r.UltimoError = false, ""
		r.EstadoDGII, r.Mensajes = resp.Estado, resp.Mensajes
		su := resp.SecuenciaUtilizada
		r.SecuenciaUtilizada = &su
		code := parseCodigo(resp.Codigo)
		r.CodigoDGII = code
		r.Estado = estadoDe(code, resp.Estado)
		if !r.Estado.Final() {
			// The RFCE service answers synchronously; anything else is
			// re-queried through the RFCE query service later.
			r.Estado, r.ProximoIntento = Pendiente, t.now().Add(backoff(backoffEnvio, r.Intentos))
		}
		t.log.Info("RFCE enviado", "archivo", r.Archivo, "estado", r.Estado)
		return
	}
	resp, err := t.cfg.DGII.EnviarECF(ctx, r.Archivo, data)
	if err == nil && resp.TrackID == "" {
		// DGII answered but refused the document.
		r.Estado = ErrorEnvio
		r.UltimoError = strings.TrimSpace("DGII no retornó TrackId: " + resp.Error + " " + resp.Mensaje)
		return
	}
	if err != nil {
		if msg := strings.TrimSpace(resp.Error + " " + resp.Mensaje); msg != "" && !retryable(err) {
			r.Estado, r.UltimoError = ErrorEnvio, msg
			return
		}
		t.fallaEnvio(r, err)
		return
	}
	r.Contingencia, r.UltimoError = false, ""
	r.TrackID, r.Estado = resp.TrackID, EnProceso
	r.Intentos = 0
	r.ProximoIntento = t.now().Add(backoff(backoffConsulta, 1))
	t.log.Info("e-CF enviado", "archivo", r.Archivo, "trackId", r.TrackID)
}

func (t *Tracker) fallaEnvio(r *Registro, err error) {
	r.UltimoError = err.Error()
	if !retryable(err) {
		r.Estado = ErrorEnvio
		t.log.Warn("DGII rechazó el envío", "archivo", r.Archivo, "error", err)
		return
	}
	r.Contingencia = true
	r.ProximoIntento = t.now().Add(backoff(backoffEnvio, r.Intentos))
	lvl := slog.LevelWarn
	if t.now().After(r.VenceContingencia) {
		lvl = slog.LevelError
		r.UltimoError += " (plazo de 72 horas de contingencia vencido)"
	}
	t.log.Log(context.Background(), lvl, "contingencia: e-CF pendiente de envío", "archivo", r.Archivo, "error", err, "proximoIntento", r.ProximoIntento)
}

func (t *Tracker) consultar(ctx context.Context, r *Registro) {
	r.Intentos++
	resp, err := t.cfg.DGII.ConsultarResultado(ctx, r.TrackID)
	if err != nil {
		r.UltimoError = err.Error()
		r.ProximoIntento = t.now().Add(backoff(backoffConsulta, r.Intentos+3))
		return
	}
	r.UltimoError = ""
	code := resp.Codigo
	r.CodigoDGII, r.EstadoDGII, r.Mensajes = &code, resp.Estado, resp.Mensajes
	su := resp.SecuenciaUtilizada
	r.SecuenciaUtilizada = &su
	r.FechaRecepcion = resp.FechaRecepcion
	switch code {
	case dgii.EstadoAceptado:
		r.Estado = Aceptado
	case dgii.EstadoAceptadoCondicional:
		r.Estado = AceptadoCondicional
	case dgii.EstadoRechazado:
		r.Estado = Rechazado
	case dgii.EstadoNoEncontrado:
		if r.Intentos >= maxIntentosNoEncontro {
			// DGII lost the submission: send it again.
			r.Estado, r.TrackID, r.Intentos = Pendiente, "", 0
		}
		r.ProximoIntento = t.now().Add(backoff(backoffConsulta, r.Intentos))
	default:
		r.ProximoIntento = t.now().Add(backoff(backoffConsulta, r.Intentos))
	}
	if r.Estado.Final() {
		t.log.Info("resultado DGII", "archivo", r.Archivo, "estado", r.Estado, "trackId", r.TrackID)
		if r.Entrega != nil && r.Entrega.Estado == EntregaPendiente {
			if r.Estado.Valido() {
				r.Entrega.ProximoIntento = t.now()
			} else {
				r.Entrega.Estado = EntregaNoAplica
			}
		}
	}
}

func parseCodigo(raw json.RawMessage) *int {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &n
}

func estadoDe(code *int, estado string) Estado {
	if code != nil {
		switch *code {
		case dgii.EstadoAceptado:
			return Aceptado
		case dgii.EstadoRechazado:
			return Rechazado
		case dgii.EstadoAceptadoCondicional:
			return AceptadoCondicional
		}
	}
	switch e := strings.ToLower(estado); {
	case strings.Contains(e, "condicional"):
		return AceptadoCondicional
	case strings.Contains(e, "aceptado"):
		return Aceptado
	case strings.Contains(e, "rechazado"):
		return Rechazado
	}
	return Pendiente
}

// destino resolves the buyer's reception URLs through the DGII directory,
// cached for one hour.
func (t *Tracker) destino(ctx context.Context, rnc string) (*dgii.Contribuyente, error) {
	t.dirMu.Lock()
	if e, ok := t.dir[rnc]; ok && t.now().Before(e.exp) {
		t.dirMu.Unlock()
		return e.c, nil
	}
	t.dirMu.Unlock()
	list, err := t.cfg.DGII.DirectorioPorRNC(ctx, rnc)
	var apiErr *dgii.APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
		list, err = nil, nil
	}
	if err != nil {
		return nil, err
	}
	var c *dgii.Contribuyente
	for i := range list {
		if list[i].URLRecepcion != "" {
			c = &list[i]
			break
		}
	}
	t.dirMu.Lock()
	t.dir[rnc] = dirEntry{c: c, exp: t.now().Add(time.Hour)}
	t.dirMu.Unlock()
	return c, nil
}

func (t *Tracker) entregar(ctx context.Context, r *Registro) {
	en := r.Entrega
	en.Intentos++
	fail := func(err error) {
		en.UltimoError = err.Error()
		if en.Intentos >= maxIntentosEntrega {
			en.Estado = EntregaError
			return
		}
		en.ProximoIntento = t.now().Add(backoff(backoffEntrega, en.Intentos))
	}
	dest, err := t.destino(ctx, r.RNCComprador)
	if err != nil {
		fail(fmt.Errorf("consultando directorio: %w", err))
		return
	}
	if dest == nil {
		en.Estado = EntregaNoElectronico
		en.UltimoError = ""
		return
	}
	en.URLRecepcion = dest.URLRecepcion
	data, err := t.cfg.Store.Get(ctx, store.EmitidoECF, r.Archivo)
	if err != nil {
		en.Estado, en.UltimoError = EntregaError, err.Error()
		return
	}
	ack, err := t.cfg.Receptor.EnviarECF(ctx, dgii.DestinoFrom(*dest), r.Archivo, data)
	if err != nil {
		fail(err)
		return
	}
	var arecf ecf.ARECF
	if err := xml.Unmarshal(ack, &arecf); err != nil {
		fail(fmt.Errorf("acuse de recibo inválido: %w", err))
		return
	}
	if _, err := xmldsig.Verify(ack); err != nil {
		t.log.Warn("acuse de recibo con firma inválida", "archivo", r.Archivo, "error", err)
	}
	_ = t.cfg.Store.Put(ctx, store.AcuseRecibido, r.Archivo, ack)
	d := arecf.DetalleAcusedeRecibo
	en.AcuseEstado, en.AcuseMotivo, en.Fecha, en.UltimoError = &d.Estado, d.CodigoMotivoNoRecibido, t.now(), ""
	switch {
	case d.Estado == ecf.AcuseRecibido, d.CodigoMotivoNoRecibido == ecf.MotivoEnvioDuplicado:
		en.Estado = EntregaEntregado
	default:
		en.Estado = EntregaRechazada
	}
	t.log.Info("e-CF entregado al receptor", "archivo", r.Archivo, "estado", en.Estado)
}
