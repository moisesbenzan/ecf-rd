// Command ecfd runs the e-CF web service: the REST API used by business
// systems to issue electronic fiscal receipts, and the standard reception
// endpoints DGII requires every electronic issuer to expose.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/api"
	"github.com/moisesbenzan/ecf-rd/internal/config"
	"github.com/moisesbenzan/ecf-rd/internal/dgii"
	"github.com/moisesbenzan/ecf-rd/internal/emision"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/receptor"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
)

var version = "dev"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	signer, err := loadSigner(cfg, log)
	if err != nil {
		return err
	}
	st, err := store.NewFS(cfg.StorageDir)
	if err != nil {
		return err
	}

	var client *dgii.Client
	if signer != nil {
		client = dgii.New(cfg.Environment, signer)
	}
	endpoints := dgii.EndpointsFor(cfg.Environment)

	svc := &emision.Service{RNC: cfg.RNC, Signer: signer, DGII: client, Store: st, Endpoints: endpoints}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		httpx.JSON(w, http.StatusOK, map[string]any{
			"status":      "ok",
			"version":     version,
			"ambiente":    cfg.Environment,
			"rnc":         cfg.RNC,
			"certificado": signer != nil,
		})
	})
	(&api.API{Emision: svc, DGII: client, Store: st, APIKey: cfg.APIKey, Logger: log}).Register(mux)
	if signer != nil {
		receptor.New(receptor.Config{
			RNC:         cfg.RNC,
			Signer:      signer,
			Store:       st,
			RequireAuth: cfg.ReceptorAuth,
			Logger:      log,
		}).Register(mux)
	} else {
		log.Warn("sin certificado digital: los servicios de recepción (/fe/*) y el envío a DGII están deshabilitados")
	}

	handler := httpx.Recover(log, httpx.Logging(log, httpx.CaseInsensitive(mux)))
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	errc := make(chan error, 1)
	go func() {
		log.Info("escuchando", "addr", cfg.Addr, "ambiente", cfg.Environment, "version", version)
		errc <- srv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("deteniendo")
		shCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shCtx)
	}
	return nil
}

func loadSigner(cfg config.Config, log *slog.Logger) (*xmldsig.Signer, error) {
	switch {
	case cfg.CertPath != "":
		s, err := xmldsig.LoadPKCS12File(cfg.CertPath, cfg.CertPassword)
		if err != nil {
			return nil, err
		}
		if err := xmldsig.CheckValidity(s.Certificate, time.Now()); err != nil {
			return nil, err
		}
		if id := xmldsig.SubjectID(s.Certificate); cfg.RNC != "" && id != cfg.RNC {
			log.Warn("el SN del certificado no coincide con ECF_RNC; debe ser un delegado autorizado del emisor",
				"sn", id, "rnc", cfg.RNC)
		}
		log.Info("certificado cargado", "sujeto", s.Certificate.Subject.String(), "expira", s.Certificate.NotAfter)
		return s, nil
	case cfg.DevCert:
		log.Warn("usando un certificado autofirmado de desarrollo: DGII rechazará los documentos firmados con él")
		id := cfg.RNC
		if id == "" {
			id = "000000000"
		}
		return xmldsig.NewTestSigner(id, "ecf-rd desarrollo")
	}
	return nil, nil
}
