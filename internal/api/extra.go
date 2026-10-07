package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/moisesbenzan/ecf-rd/internal/ecf"
	"github.com/moisesbenzan/ecf-rd/internal/emision"
	"github.com/moisesbenzan/ecf-rd/internal/httpx"
	"github.com/moisesbenzan/ecf-rd/internal/seguimiento"
	"github.com/moisesbenzan/ecf-rd/internal/store"
	"github.com/moisesbenzan/ecf-rd/internal/xmldsig"
	"github.com/moisesbenzan/ecf-rd/internal/xsd"
)

func (a *API) needTracker(w http.ResponseWriter) bool {
	if a.Tracker == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "el envío a DGII no está configurado (falta certificado)")
		return false
	}
	return true
}

// seguimiento returns the tracking record of an issued e-CF.
func (a *API) seguimiento(w http.ResponseWriter, r *http.Request) {
	if !a.needTracker(w) {
		return
	}
	reg, ok := a.Tracker.Get(fileName(r.PathValue("archivo")))
	if !ok {
		httpx.Error(w, http.StatusNotFound, "el documento no ha sido enviado")
		return
	}
	httpx.JSON(w, http.StatusOK, reg)
}

// reenviar schedules an immediate submission retry, or submits a stored
// e-CF that was issued without ?enviar=true.
func (a *API) reenviar(w http.ResponseWriter, r *http.Request) {
	if !a.needTracker(w) {
		return
	}
	archivo := fileName(r.PathValue("archivo"))
	err := a.Tracker.Reintentar(r.Context(), archivo)
	if errors.Is(err, store.ErrNotFound) {
		err = a.submitStored(r, archivo)
	}
	if err != nil {
		writeIssuanceError(w, err)
		return
	}
	reg, _ := a.Tracker.Get(archivo)
	httpx.JSON(w, http.StatusAccepted, reg)
}

func (a *API) submitStored(r *http.Request, archivo string) error {
	b, err := a.Store.Get(r.Context(), store.EmitidoECF, archivo)
	if err != nil {
		return err
	}
	e, err := ecf.Unmarshal(b)
	if err != nil {
		return err
	}
	code, err := xmldsig.SecurityCode(b)
	if err != nil {
		return err
	}
	h := &e.Encabezado
	reg := &seguimiento.Registro{
		Archivo: archivo, ENCF: h.IdDoc.ENCF, TipoeCF: int(h.IdDoc.TipoeCF), RNCEmisor: h.Emisor.RNCEmisor,
		FechaEmision: h.Emisor.FechaEmision, FechaHoraFirma: e.FechaHoraFirma, MontoTotal: h.Totales.MontoTotal.String(),
		CodigoSeguridad: code, Resumen: emision.EsResumen(e),
	}
	if h.Comprador != nil {
		reg.RNCComprador = h.Comprador.RNCComprador
	}
	return a.Tracker.Submit(r.Context(), reg)
}

// entregar retries the delivery of an accepted e-CF to the buyer.
func (a *API) entregar(w http.ResponseWriter, r *http.Request) {
	if !a.needTracker(w) {
		return
	}
	archivo := fileName(r.PathValue("archivo"))
	if err := a.Tracker.ReintentarEntrega(r.Context(), archivo); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeIssuanceError(w, err)
			return
		}
		httpx.Error(w, http.StatusConflict, err.Error())
		return
	}
	reg, _ := a.Tracker.Get(archivo)
	httpx.JSON(w, http.StatusAccepted, reg)
}

// acuse returns the ARECF returned by the buyer.
func (a *API) acuse(w http.ResponseWriter, r *http.Request) {
	a.serveStored(w, r, store.AcuseRecibido)
}

// envios lists tracking records (?estado=pendiente|en_proceso|aceptado|...).
func (a *API) envios(w http.ResponseWriter, r *http.Request) {
	if !a.needTracker(w) {
		return
	}
	list := a.Tracker.List(seguimiento.Estado(r.URL.Query().Get("estado")), intParam(r, "limite", 100))
	httpx.JSON(w, http.StatusOK, list)
}

func (a *API) recibidos(w http.ResponseWriter, r *http.Request) {
	list, err := a.Emision.ListarRecibidos(r.Context())
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []emision.Recibido{}
	}
	httpx.JSON(w, http.StatusOK, list)
}

func (a *API) recibido(w http.ResponseWriter, r *http.Request) {
	a.serveStored(w, r, store.RecibidoECF)
}

// aprobarRecibido issues the commercial approval of a received e-CF:
// {"estado": 1|2, "motivo": "..."} (?enviar=true sends it to the issuer
// and DGII).
func (a *API) aprobarRecibido(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Estado int    `json:"estado"`
		Motivo string `json:"motivo"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		httpx.Error(w, http.StatusBadRequest, "JSON inválido: "+err.Error())
		return
	}
	res, err := a.Emision.AprobarRecibido(r.Context(), fileName(r.PathValue("archivo")), body.Estado, body.Motivo, boolParam(r, "enviar", false))
	if err != nil {
		writeIssuanceError(w, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, res)
}

var esquemas = map[string]xsd.Document{
	"ecf-31": xsd.ECF31, "ecf-32": xsd.ECF32, "ecf-33": xsd.ECF33, "ecf-34": xsd.ECF34, "ecf-41": xsd.ECF41,
	"ecf-43": xsd.ECF43, "ecf-44": xsd.ECF44, "ecf-45": xsd.ECF45, "ecf-46": xsd.ECF46, "ecf-47": xsd.ECF47,
	"rfce": xsd.RFCE, "arecf": xsd.ARECF, "acecf": xsd.ACECF, "anecf": xsd.ANECF,
}

// validarXML validates a raw XML document against an official schema
// (ecf-31..ecf-47, rfce, arecf, acecf, anecf).
func (a *API) validarXML(w http.ResponseWriter, r *http.Request) {
	doc, ok := esquemas[r.PathValue("esquema")]
	if !ok {
		httpx.Error(w, http.StatusNotFound, "esquema desconocido")
		return
	}
	data, _, err := httpx.ReadXMLUpload(r)
	if err != nil || len(data) == 0 {
		httpx.Error(w, http.StatusBadRequest, "XML requerido")
		return
	}
	issues := ecf.ValidateXML(doc, data)
	if issues == nil {
		issues = ecf.Issues{}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"valido": !issues.HasErrors(), "hallazgos": issues})
}

type codigo struct {
	Codigo      string `json:"codigo"`
	Descripcion string `json:"descripcion"`
	Abreviatura string `json:"abreviatura,omitempty"`
	Tasa        string `json:"tasa,omitempty"`
	Clase       string `json:"clase,omitempty"`
}

// tablas exposes the DGII code tables: impuestos (Tabla I), monedas
// (Tabla II), provincias (Tabla III), unidades (Tabla IV) and tipos.
func (a *API) tablas(w http.ResponseWriter, r *http.Request) {
	var out []codigo
	switch r.PathValue("tabla") {
	case "impuestos":
		for _, i := range ecf.TablaImpuestos {
			out = append(out, codigo{Codigo: i.Codigo, Descripcion: i.Descripcion, Abreviatura: i.Nombre, Tasa: i.Tasa.String(), Clase: i.Clase.String()})
		}
	case "monedas":
		for k, v := range ecf.Monedas {
			out = append(out, codigo{Codigo: k, Descripcion: v})
		}
	case "provincias":
		for k, v := range ecf.ProvinciasMunicipios {
			out = append(out, codigo{Codigo: k, Descripcion: v})
		}
	case "unidades":
		for k, v := range ecf.UnidadesMedida {
			out = append(out, codigo{Codigo: strconv.Itoa(k), Descripcion: v.Nombre, Abreviatura: v.Abreviatura})
		}
	case "tipos":
		for _, t := range ecf.Tipos() {
			out = append(out, codigo{Codigo: t.String(), Descripcion: t.Name()})
		}
	default:
		httpx.Error(w, http.StatusNotFound, "tabla desconocida (impuestos, monedas, provincias, unidades, tipos)")
		return
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i].Codigo) != len(out[j].Codigo) {
			return len(out[i].Codigo) < len(out[j].Codigo)
		}
		return out[i].Codigo < out[j].Codigo
	})
	httpx.JSON(w, http.StatusOK, out)
}
