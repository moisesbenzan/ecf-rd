// Package dgii is a client for the DGII electronic invoicing web services
// described in "Descripción Técnica Servicios DGII".
package dgii

import (
	"fmt"
	"strings"
)

// Environment selects the DGII environment.
type Environment string

// DGII environments.
const (
	// PreCertificacion (TesteCF) is the integration sandbox. Submissions are
	// kept for 60 days and every taxpayer has test sequences 1..10,000,000.
	PreCertificacion Environment = "testecf"
	// Certificacion (CerteCF) is used to pass the certification test set.
	Certificacion Environment = "certecf"
	// Produccion (eCF) is the production environment with fiscal validity.
	Produccion Environment = "ecf"
)

// ParseEnvironment accepts the DGII path names (testecf, certecf, ecf) and
// friendly aliases.
func ParseEnvironment(s string) (Environment, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "testecf", "test", "precertificacion", "pre-certificacion", "":
		return PreCertificacion, nil
	case "certecf", "cert", "certificacion":
		return Certificacion, nil
	case "ecf", "prod", "produccion", "production":
		return Produccion, nil
	}
	return "", fmt.Errorf("dgii: ambiente desconocido %q (testecf, certecf, ecf)", s)
}

// Hosts used by DGII. ecf.dgii.gov.do serves most services; fc.dgii.gov.do
// serves the consumer-invoice summary (RFCE) services.
const (
	HostECF = "https://ecf.dgii.gov.do"
	HostFC  = "https://fc.dgii.gov.do"
)

// Endpoints holds the base URLs of every DGII service for an environment.
// Each field is overridable for testing or if DGII relocates a service.
type Endpoints struct {
	Autenticacion       string // GET /api/autenticacion/semilla, POST /api/autenticacion/validarsemilla
	Recepcion           string // POST /api/facturaselectronicas
	RecepcionFC         string // POST /api/recepcion/ecf
	ConsultaRFCE        string // GET /api/Consultas/Consulta
	ConsultaResultado   string // GET /api/consultas/estado?trackid=
	ConsultaEstado      string // GET /api/consultas/estado?rncemisor=...
	ConsultaTrackIds    string // GET /api/trackids/consulta
	AprobacionComercial string // POST /api/aprobacioncomercial
	AnulacionRangos     string // POST /api/operaciones/anularrango
	ConsultaDirectorio  string // GET /api/consultas/listado, /api/consultas/obtenerdirectorioporrnc
	ConsultaTimbre      string // QR URL base for e-CF
	ConsultaTimbreFC    string // QR URL base for RFCE
}

// EndpointsFor returns the official base URLs for env.
func EndpointsFor(env Environment) Endpoints {
	e := string(env)
	fcEnv := e
	if env == Certificacion {
		// The documentation spells this path "Certecf"; paths are
		// case-insensitive on DGII servers but we follow the document.
		fcEnv = "Certecf"
	}
	return Endpoints{
		Autenticacion:       HostECF + "/" + e + "/autenticacion",
		Recepcion:           HostECF + "/" + e + "/recepcion",
		RecepcionFC:         HostFC + "/" + fcEnv + "/recepcionfc",
		ConsultaRFCE:        HostFC + "/" + e + "/consultarfce",
		ConsultaResultado:   HostECF + "/" + e + "/consultaresultado",
		ConsultaEstado:      HostECF + "/" + e + "/consultaestado",
		ConsultaTrackIds:    HostECF + "/" + e + "/consultatrackids",
		AprobacionComercial: HostECF + "/" + e + "/aprobacioncomercial",
		AnulacionRangos:     HostECF + "/" + e + "/anulacionrangos",
		ConsultaDirectorio:  HostECF + "/" + e + "/consultadirectorio",
		ConsultaTimbre:      HostECF + "/" + e + "/consultatimbre",
		ConsultaTimbreFC:    HostFC + "/" + e + "/consultatimbrefc",
	}
}
