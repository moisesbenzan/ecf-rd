// Package app assembles the service: storage, DGII client, tracker,
// issuance service, REST API and the receiver endpoints.
package app

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/api"
	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/emision"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/receptor"
	"github.com/moisesbenzan/ecf-rd/internal/seguimiento"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
	"github.com/moisesbenzan/ecf-rd/internal/xsd"
)

// Config holds the assembled dependencies.
type Config struct {
	Environment  dgii.Environment
	Endpoints    *dgii.Endpoints // overrides the official URLs (tests)
	RNC          string
	Signer       *xmldsig.Signer // nil disables signing, submission and reception
	Store        store.Store
	APIKey       string
	ReceptorAuth bool
	HTTPClient   *http.Client
	Logger       *slog.Logger
	Version      string
	// TrackerInterval overrides the tracker scan interval.
	TrackerInterval time.Duration
}

// App is a running service instance.
type App struct {
	Handler http.Handler
	Tracker *seguimiento.Tracker
	Service *emision.Service
	wg      sync.WaitGroup
}

// New wires the service and starts the tracker until ctx ends.
func New(ctx context.Context, cfg Config) (*App, error) {
	xsd.MustLoadAll()
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	endpoints := dgii.EndpointsFor(cfg.Environment)
	if cfg.Endpoints != nil {
		endpoints = *cfg.Endpoints
	}
	a := &App{}
	var (
		client *dgii.Client
		rc     *dgii.ReceptorClient
	)
	if cfg.Signer != nil {
		client = dgii.New(cfg.Environment, cfg.Signer, dgii.WithEndpoints(endpoints), dgii.WithHTTPClient(cfg.HTTPClient))
		rc = dgii.NewReceptorClient(cfg.HTTPClient, cfg.Signer)
		a.Tracker = seguimiento.New(seguimiento.Config{Store: cfg.Store, DGII: client, Receptor: rc, Logger: log, Interval: cfg.TrackerInterval})
		if err := a.Tracker.Load(ctx); err != nil {
			return nil, err
		}
		a.wg.Add(1)
		go func() { defer a.wg.Done(); a.Tracker.Run(ctx) }()
	}
	a.Service = &emision.Service{RNC: cfg.RNC, Signer: cfg.Signer, DGII: client, Receptor: rc, Tracker: a.Tracker, Store: cfg.Store, Endpoints: endpoints}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"status": "ok", "version": cfg.Version, "ambiente": cfg.Environment,
			"rnc": cfg.RNC, "certificado": cfg.Signer != nil,
		})
	})
	(&api.API{Emision: a.Service, DGII: client, Tracker: a.Tracker, Store: cfg.Store, APIKey: cfg.APIKey, Logger: log}).Register(mux)
	if cfg.Signer != nil {
		tracker := a.Tracker
		receptor.New(receptor.Config{
			RNC: cfg.RNC, Signer: cfg.Signer, Store: cfg.Store, RequireAuth: cfg.ReceptorAuth, Logger: log,
			OnAprobacion: func(ctx context.Context, ac *ecf.ACECF) {
				if err := tracker.RegistrarAprobacion(ctx, ac); err != nil {
					log.Warn("aprobación comercial sin seguimiento", "eNCF", ac.DetalleAprobacionComercial.ENCF, "error", err)
				}
			},
		}).Register(mux)
	} else {
		log.Warn("sin certificado digital: los servicios de recepción (/fe/*) y el envío a DGII están deshabilitados")
	}
	a.Handler = httpx.Recover(log, httpx.Logging(log, httpx.CaseInsensitive(mux)))
	return a, nil
}

// Wait blocks until background workers stop (after ctx is cancelled).
func (a *App) Wait() { a.wg.Wait() }
