// Package config loads service configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/moisesbenzan/ecf-rd/internal/dgii"
)

// Config is the service configuration.
type Config struct {
	Addr            string           // ECF_ADDR, default ":8080"
	Environment     dgii.Environment // ECF_AMBIENTE: testecf | certecf | ecf (default testecf)
	RNC             string           // ECF_RNC: RNC of the issuing taxpayer
	CertPath        string           // ECF_CERT_PATH: .p12/.pfx file
	CertPassword    string           // ECF_CERT_PASSWORD
	DevCert         bool             // ECF_DEV_CERT: generate a self-signed cert when none is configured (development only)
	APIKey          string           // ECF_API_KEY: protects /api/v1
	StorageDir      string           // ECF_STORAGE_DIR, default "./data"
	ReceptorAuth    bool             // ECF_RECEPTOR_AUTH: require bearer tokens on /fe/* reception endpoints
	ShutdownTimeout time.Duration    // ECF_SHUTDOWN_TIMEOUT, default 15s
}

// FromEnv reads the configuration from the environment.
func FromEnv() (Config, error) {
	c := Config{
		Addr:            getenv("ECF_ADDR", ":8080"),
		RNC:             os.Getenv("ECF_RNC"),
		CertPath:        os.Getenv("ECF_CERT_PATH"),
		CertPassword:    os.Getenv("ECF_CERT_PASSWORD"),
		APIKey:          os.Getenv("ECF_API_KEY"),
		StorageDir:      getenv("ECF_STORAGE_DIR", "./data"),
		ShutdownTimeout: 15 * time.Second,
	}
	env, err := dgii.ParseEnvironment(os.Getenv("ECF_AMBIENTE"))
	if err != nil {
		return c, err
	}
	c.Environment = env
	if c.DevCert, err = getbool("ECF_DEV_CERT"); err != nil {
		return c, err
	}
	if c.ReceptorAuth, err = getbool("ECF_RECEPTOR_AUTH"); err != nil {
		return c, err
	}
	if v := os.Getenv("ECF_SHUTDOWN_TIMEOUT"); v != "" {
		if c.ShutdownTimeout, err = time.ParseDuration(v); err != nil {
			return c, fmt.Errorf("ECF_SHUTDOWN_TIMEOUT: %w", err)
		}
	}
	if c.DevCert && c.Environment == dgii.Produccion {
		return c, fmt.Errorf("ECF_DEV_CERT no está permitido en el ambiente de producción")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getbool(k string) (bool, error) {
	v := os.Getenv(k)
	if v == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", k, err)
	}
	return b, nil
}
