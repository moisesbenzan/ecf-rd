package dgii

import (
	"encoding/json"
	"strings"
	"time"
)

// Token is a DGII session token, valid for one hour at the time of writing.
type Token struct {
	Token    string    `json:"token"`
	Expira   time.Time `json:"expira"`
	Expedido time.Time `json:"expedido"`
}

// UnmarshalJSON accepts the "universal sortable" timestamps DGII documents
// (yyyy-MM-ddTHH:mm:ssZ), with or without fractional seconds or zone.
func (t *Token) UnmarshalJSON(b []byte) error {
	var raw struct {
		Token    string `json:"token"`
		Expira   string `json:"expira"`
		Expedido string `json:"expedido"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	t.Token = raw.Token
	t.Expira = parseTimestamp(raw.Expira)
	t.Expedido = parseTimestamp(raw.Expedido)
	return nil
}

func parseTimestamp(s string) time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.9999999", "2006-01-02T15:04:05", "2006-01-02 15:04:05Z"} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts
		}
	}
	return time.Time{}
}

// Valid reports whether the token is usable at t with the given safety margin.
func (t *Token) Valid(at time.Time, margin time.Duration) bool {
	return t != nil && t.Token != "" && at.Add(margin).Before(t.Expira)
}

// Semilla is the challenge returned by the authentication service.
type Semilla struct {
	Valor string `xml:"valor"`
	Fecha string `xml:"fecha"`
}

// RespuestaRecepcion is returned by the e-CF reception service.
type RespuestaRecepcion struct {
	TrackID string `json:"trackId"`
	Error   string `json:"error,omitempty"`
	Mensaje string `json:"mensaje,omitempty"`
}

// Mensaje is a validation message (codigo/valor).
type Mensaje struct {
	Codigo json.RawMessage `json:"codigo"` // DGII returns numbers or strings depending on the service
	Valor  string          `json:"valor"`
}

// Estado codes returned by the result and status queries.
const (
	EstadoNoEncontrado        = 0
	EstadoAceptado            = 1
	EstadoRechazado           = 2
	EstadoEnProceso           = 3
	EstadoAceptadoCondicional = 4
)

// RespuestaConsultaResultado is returned when querying a TrackId.
type RespuestaConsultaResultado struct {
	TrackID            string    `json:"trackId"`
	Codigo             int       `json:"codigo"`
	Estado             string    `json:"estado"`
	RNC                string    `json:"rnc"`
	ENCF               string    `json:"eNCF"`
	SecuenciaUtilizada bool      `json:"secuenciaUtilizada"`
	FechaRecepcion     string    `json:"fechaRecepcion"`
	Mensajes           []Mensaje `json:"mensajes"`
}

// Final reports whether the result will no longer change.
func (r *RespuestaConsultaResultado) Final() bool {
	return r.Codigo != EstadoEnProceso
}

// RespuestaRFCE is returned by the RFCE reception and query services.
type RespuestaRFCE struct {
	RNC                string          `json:"rnc,omitempty"`
	Codigo             json.RawMessage `json:"codigo"`
	Estado             string          `json:"estado"`
	Mensajes           []Mensaje       `json:"mensajes"`
	ENCF               string          `json:"encf"`
	SecuenciaUtilizada bool            `json:"secuenciaUtilizada"`
}

// RespuestaConsultaEstado is returned by the e-CF status query.
type RespuestaConsultaEstado struct {
	Codigo          int         `json:"codigo"`
	Estado          string      `json:"estado"`
	RNCEmisor       string      `json:"rncEmisor"`
	NCFElectronico  string      `json:"ncfElectronico"`
	MontoTotal      json.Number `json:"montoTotal"`
	TotalITBIS      json.Number `json:"totalITBIS"`
	FechaEmision    string      `json:"fechaEmision"`
	FechaFirma      string      `json:"fechaFirma"`
	RNCComprador    string      `json:"rncComprador"`
	CodigoSeguridad string      `json:"codigoSeguridad"`
	IDExtranjero    string      `json:"idExtranjero"`
}

// TrackingDetalle is an item returned by the TrackIds query.
type TrackingDetalle struct {
	TrackID        string `json:"trackId"`
	Estado         string `json:"estado"`
	FechaRecepcion string `json:"fechaRecepcion"`
}

// RespuestaAprobacionComercial is returned by the commercial approval service.
type RespuestaAprobacionComercial struct {
	Mensaje []string        `json:"mensaje"`
	Estado  string          `json:"estado"`
	Codigo  json.RawMessage `json:"codigo"`
}

// RespuestaAnulacionRango is returned by the range voiding service.
type RespuestaAnulacionRango struct {
	RNC      string   `json:"rnc"`
	Codigo   string   `json:"codigo"`
	Nombre   string   `json:"nombre"`
	Mensajes []string `json:"mensajes"`
}

// Contribuyente is an entry of the electronic taxpayers directory.
type Contribuyente struct {
	Nombre        string `json:"nombre"`
	RNC           string `json:"rnc"`
	URLRecepcion  string `json:"urlRecepcion"`
	URLAceptacion string `json:"urlAceptacion"`
	URLOpcional   string `json:"urlOpcional"`
}
