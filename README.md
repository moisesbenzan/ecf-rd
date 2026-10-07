# ecf-rd

A Go web service for issuing, receiving and tracking **Comprobantes Fiscales Electrónicos (e-CF)**, following the electronic invoicing specification of the Dominican Republic's tax authority (DGII).

It implements the documentation published at
[Documentación sobre e-CF](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/documentacionSobreE-CF.aspx):

| Document | Implemented in |
|---|---|
| XSD e-CF 31–47, RFCE 32, ARECF, ACECF, ANECF, Semilla v1.0 (embedded verbatim) | `internal/xsd`: every document is checked against its official schema |
| Formato Comprobante Fiscal Electrónico (e-CF) v1.0 (Oct 2025), Tablas I–IV | `internal/ecf`: model, per-type requirements, calculations, validations, code tables |
| Formato Resumen Factura de Consumo, Acuse de Recibo, Aprobación Comercial, Anulación de e-NCF | `internal/ecf`: models and validators |
| Informe Técnico e-CF (Mar 2026) | Tolerance and rounding rules, ISC and other additional taxes (§12–15), Norma 07-07 / 03-08 / 10-07, operating model (§8–9), timbre URL contents (§18), contingency (§19) |
| Descripción Técnica Servicios DGII (Jan 2026) | `internal/dgii`: client for every service in the three environments |
| Descripción Técnica Servicios Emisores Electrónicos (Jan 2026) | `internal/receptor`: the receiver endpoints; file names and character rules |
| Firmado de e-CF | `internal/xmldsig`: enveloped XMLDSig, C14N, RSA-SHA256 |

## Features

### Documents
- **All 10 e-CF types**, plus RFCE, ARECF, ACECF and ANECF. JSON keys match the XML tag names exactly, and unknown keys are rejected.
- **Official XSD validation.** All 15 DGII schemas are embedded and compiled by a pure-Go validator, with no cgo. The validator covers element order, which elements each type allows, patterns, lengths, digits and code-table enumerations. The test suite cross-checks every generated document type against libxml2 (`xmllint`).
- **Code tables (I–IV).** Additional taxes with their rates and calculation class, currencies, the 582 province/municipality/district codes, and the 62 units of measure. A test asserts they match the XSD enumerations exactly.

### Calculations
Totals are computed using exact fixed-point decimals and the DGII rounding rule.
- Line amounts, including sub-discounts and sub-surcharges entered in % or $.
- Taxed bases at 18/16/0 %, exempt and non-billable amounts. Prices that include ITBIS (`IndicadorMontoGravado=1`) are handled.
- Global discounts and surcharges entered in % or $, assigned to the right tax indicator.
- **Additional taxes from Tabla I:**
  - ISC específico for alcohol and cigarettes
  - ISC ad valorem, including the bulk (granel) rule
  - Propina legal, CDT, insurance, telecommunications and first registration plate, with global discounts and surcharges apportioned per line
  - Norma 10-07

  ISC is included in the ITBIS base. The service reproduces the worked examples in the Informe Técnico (ISC 276.34; CDT 7.69 and 8.41).
- Withholding vs perception totals, `MontoPeriodo`, `ValorPagar`, and `IndicadorNotaCredito` (whether a credit note is more than 30 days after the original).
- **Foreign currency (`OtraMoneda`)** totals, taken from line detail or converted at `TipoCambio`.
- **Pagination (`Paginacion`)**: page ranges and per-page subtotals.

### Validation
Validation applies the requirement matrix for each type and these business rules:
- e-NCF series E–Z except P, and the sequence expiry date.
- The fixed indicator required by types 43, 44, 46 and 47.
- Retentions for types 41 and 47.
- Required ISC data per tax code.
- The NCF modification codes, including contingency replacement and type equivalence for paper NCFs.
- Credit notes cannot exceed the original e-CF.
- Global discount rules and dates.

**DGII tolerance rule:** each line may differ by ±1, and the total by ±(number of lines). Beyond that, DGII accepts the document conditionally. With `estricto=true`, the default, the service reports this as an error; with `estricto=false` it reports a warning.

### Signing
- Enveloped XMLDSig that matches DGII's reference structure, using a `.p12`/`.pfx` certificate.
- Generates the security code and the timbre URLs (the content of the QR code on printed invoices) for both e-CF and RFCE, returned by `emitir` so your own printing system can render them.
- Signatures verify under three independent implementations: this service, goxmldsig, and libxml2 C14N with OpenSSL.

### Operating model
The tracker in `internal/seguimiento` follows each document through DGII's operating model:
1. **Submission.** The e-CF goes to `recepcion`, or the RFCE goes to `recepcionfc` for type 32 under RD$250,000.
2. **Contingency.** If the network fails or DGII returns a 5xx error, the document is queued and retried with backoff. Delays past the 72-hour submission deadline are flagged. Ranges voided through ANECF are dropped from the queue.
3. **Result polling.** The TrackId is queried until the result is final: aceptado, aceptado condicional or rechazado.
4. **Delivery to the buyer.** If the DGII directory lists the buyer as electronic, the e-CF is delivered with authentication (semilla → signature → token). The signed ARECF the buyer returns is verified and stored.
5. **Commercial approval.** Approvals received for issued e-CF are matched against the stored document and recorded.

State is persisted as JSON in the store and reloaded on restart.

### Receiver endpoints (`/fe/*`, required by DGII)
- Routing ignores letter case. Seeds are single-use and tokens last one hour.
- Incoming e-CF are checked against the XSD (failure → motivo 1), the signature (motivo 2), duplicates (motivo 3) and the buyer RNC (motivo 4). Every response is a signed ARECF.
- Incoming approvals (ACECF) must match the e-CF actually issued: issuer RNC, e-NCF, issue date, total and buyer.

### Receiving documents
Received e-CF can be listed and viewed as XML. Commercial approvals are built from the stored e-CF and sent to the issuer (looked up in the DGII directory) and to DGII. Before sending, the service checks that DGII has accepted the e-CF.

## Running

```sh
go build -o ecfd ./cmd/ecfd

# Development, with a self-signed certificate (DGII will reject what it signs):
ECF_RNC=131880738 ECF_DEV_CERT=true ./ecfd

# Real certificate:
ECF_AMBIENTE=testecf ECF_RNC=131880738 \
ECF_CERT_PATH=/secrets/cert.p12 ECF_CERT_PASSWORD=... \
ECF_API_KEY=change-me ./ecfd
```

| Variable | Default | Description |
|---|---|---|
| `ECF_ADDR` | `:8080` | Listen address |
| `ECF_AMBIENTE` | `testecf` | `testecf` (pre-certificación), `certecf` (certificación), `ecf` (producción) |
| `ECF_RNC` | — | RNC of the issuing taxpayer. Documents for any other RNC are rejected. |
| `ECF_CERT_PATH` / `ECF_CERT_PASSWORD` | — | PKCS#12 certificate from a DGII-authorized certification authority |
| `ECF_DEV_CERT` | `false` | Self-signed certificate. Not allowed in production. |
| `ECF_API_KEY` | — | Required in `X-API-Key` (or `Authorization: Bearer`) for `/api/v1` |
| `ECF_STORAGE_DIR` | `./data` | Signed XML and tracking state |
| `ECF_RECEPTOR_AUTH` | `false` | Require bearer tokens on `/fe/recepcion` and `/fe/aprobacioncomercial` |

The receiver services must be reachable from the internet over HTTPS on standard ports. Terminate TLS at a reverse proxy, then register the URLs with DGII.

## API

### Issuing (`/api/v1`)

| Method | Path | Description |
|---|---|---|
| POST | `/ecf/validar` | Complete and validate an e-CF. Returns the findings and the completed document. |
| POST | `/ecf/xml` | Unsigned XML |
| POST | `/ecf/emitir` | Complete, validate, sign and store the e-CF, and optionally submit it (see the parameters below) |
| GET | `/ecf/{RNC+eNCF}` | Signed XML |
| GET | `/ecf/{RNC+eNCF}/estado` | Tracking record: DGII state, TrackId, messages, contingency, delivery, commercial approval |
| POST | `/ecf/{RNC+eNCF}/reenviar` | Retry now, or submit an e-CF that was issued without `enviar` |
| POST | `/ecf/{RNC+eNCF}/entregar` | Retry delivery to the buyer |
| GET | `/ecf/{RNC+eNCF}/acuse` | ARECF returned by the buyer |
| GET | `/envios?estado=&limite=` | Tracking records, filtered by state: `pendiente`, `en_proceso`, `aceptado`, `aceptado_condicional`, `rechazado`, `error_envio` or `anulado` |
| POST | `/anulacion?enviar=true` | Void e-NCF ranges (ANECF). Counts are computed for you. |

**Parameters for `POST /ecf/emitir`:**

| Parameter | Default | Effect |
|---|---|---|
| `completar` | `true` | Compute line amounts and totals before validating |
| `estricto` | `true` | Treat amounts outside DGII's tolerance as errors instead of warnings |
| `enviar` | `false` | Submit the document to DGII through the tracker |
| `esperar` | — | Seconds to wait for DGII's final result |
| `lineasporpagina` | — | Lines per printed page; builds the `Paginacion` section of the XML |

### Received documents

| Method | Path | Description |
|---|---|---|
| GET | `/recibidos` | Received e-CF, with commercial approval status |
| GET | `/recibidos/{archivo}` | Signed XML |
| POST | `/recibidos/{archivo}/aprobacion?enviar=true` | `{"estado":1}` accepts; `{"estado":2,"motivo":"..."}` rejects. Sent to the issuer and to DGII. |
| POST | `/aprobacion-comercial` | Sign (and send) a complete ACECF |

### DGII queries and utilities

| Method | Path | Description |
|---|---|---|
| GET | `/dgii/resultado/{trackId}` | Validation result |
| GET | `/dgii/estado?rncemisor&encf[&rnccomprador&codigoseguridad]` | Document status |
| GET | `/dgii/trackids?rncemisor&encf` | Submissions for an e-NCF |
| GET | `/dgii/rfce?rncemisor&encf&codigoseguridad` | RFCE status |
| GET | `/dgii/directorio[?rnc]` | Directory of electronic taxpayers |
| POST | `/xml/firmar` · `/xml/verificar` | Sign any DGII XML / verify its signature |
| POST | `/xml/validar/{ecf-31…ecf-47,rfce,arecf,acecf,anecf}` | Validate raw XML against the official XSD |
| GET | `/tablas/{impuestos,monedas,provincias,unidades,tipos}` | DGII code tables |
| GET | `/healthz` | Health check |

### Receiver endpoints (`/fe`)

| Method | Path | Description |
|---|---|---|
| GET | `/fe/autenticacion/api/semilla` | Single-use seed |
| POST | `/fe/autenticacion/api/validacioncertificado` | Signed seed (checked against the Semilla XSD) → token |
| POST | `/fe/recepcion/api/ecf` | e-CF → signed ARECF |
| POST | `/fe/aprobacioncomercial/api/ecf` | ACECF → HTTP 200/400 |

Example payloads are in `examples/`: a credit-fiscal invoice, a consumer invoice, and an alcohol invoice with ISC.

## Layout

```
cmd/ecfd             entry point
internal/app         wiring (shared by main and the end-to-end test)
internal/xsd         embedded official XSDs and the pure-Go schema validator
internal/ecf         models, Tablas I–IV, calculations (taxes, currency, pagination), validators, XML, timbre URLs
internal/xmldsig     XMLDSig signing and verification, PKCS#12 loading
internal/dgii        DGII web services client; client for other taxpayers' receivers
internal/seguimiento submission tracking, contingency, result polling, delivery, approvals
internal/emision     issuance and commercial-approval orchestration
internal/receptor    inbound /fe/* endpoints
internal/api         business REST API
internal/store       document storage (filesystem, memory)
```

## Tests

```sh
go test -race ./...
```

What the suite covers:
- Every e-CF type is validated by this service's validator and by libxml2.
- The auxiliary formats are checked, and the Informe Técnico and ANECF Annex I worked examples are reproduced.
- The code tables are compared with the XSD enumerations.
- Signatures are verified by independent verifiers.
- The tracker runs through an outage, contingency and recovery using a fake clock.
- An end-to-end test runs two live instances (issuer and buyer) against a fake DGII: issue → TrackId → accepted → delivery → ARECF → commercial approval, plus the RFCE path and duplicate rejection.

## Notes and limits

- **Printed representation is out of scope.** The service produces everything a printed invoice needs (signed XML, security code, signature date, timbre URL, `Paginacion` subtotals, and a `contingencia` flag in the tracking record), but rendering PDFs is left to the calling system.

- **Schema defects.** The official schemas contain two defects that strict processors reject but DGII's .NET stack tolerates: a type name with a leading space in ECF-31, and a non-XSD `(?:` group in RFCE-32. The embedded validator handles both the way .NET does. The `xmllint` cross-check runs against patched copies.
- **ISC rates.** Specific ISC amounts (codes 006–022) are adjusted by DGII every quarter. Pass the current rate in `Totales.ImpuestosAdicionales[].TasaImpuestoAdicional`. Otherwise the Tabla I default is used, with a warning.
- **Empty buyer section.** The ECF-32 schema requires the `<Comprador>` element. For anonymous consumer invoices the service emits it empty.
- **Untested against DGII.** The service has not been run end to end against DGII's real environments. That needs a certificate from an authorized certification authority and an RNC enabled in pre-certification. DGII's certification test set ("set de pruebas") is run from the Oficina Virtual and is not automated here.
