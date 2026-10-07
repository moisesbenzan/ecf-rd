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

	"github.com/moisesbenzan/ecf-rd/internal/app"
	"github.com/moisesbenzan/ecf-rd/internal/config"
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, app.Config{
		Environment:  cfg.Environment,
		RNC:          cfg.RNC,
		Signer:       signer,
		Store:        st,
		APIKey:       cfg.APIKey,
		ReceptorAuth: cfg.ReceptorAuth,
		Logger:       log,
		Version:      version,
	})
	if err != nil {
		return err
	}
	handler := application.Handler
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

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
		err := srv.Shutdown(shCtx)
		application.Wait()
		return err
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
