# ecf-rd

A Go web service for issuing and receiving **Comprobantes Fiscales Electrónicos (e-CF)**. It follows the electronic invoicing specification of the Dominican Republic's tax authority (DGII).

The implementation follows the official DGII documentation published at
[Documentación sobre e-CF](https://dgii.gov.do/cicloContribuyente/facturacion/comprobantesFiscalesElectronicosE-CF/Paginas/documentacionSobreE-CF.aspx):

| Document | Used for |
|---|---|
| Formato Comprobante Fiscal Electrónico (e-CF) v1.0 (Oct 2025) | Data model, which fields are required for each type, and validations |
| XSD e-CF 31–47, RFCE 32, ARECF, ACECF, ANECF v1.0 | Element names, element order, and value patterns |
| Descripción Técnica Servicios DGII (Jan 2026) | DGII client: environments, endpoints, authentication, responses |
| Descripción Técnica Servicios Emisores Electrónicos (Jan 2026) | Receiver endpoints (`/fe/...`), file names, character rules |
| Firmado de e-CF | XMLDSig structure (enveloped, C14N, RSA-SHA256) |

## Features

- **Complete e-CF model.** Covers all types (31, 32, 33, 34, 41, 43, 44, 45, 46, 47). Elements are emitted in the XSD sequence order, and empty tags are omitted as DGII requires. The JSON keys match the XML tag names exactly.
- **Automatic completion.** Line numbers and `MontoItem` are filled in. All totals that can be derived from the lines are computed: taxed bases at 18/16/0 %, ITBIS, exempt, non-billable, withholdings, and `MontoTotal`. Prices that include ITBIS (`IndicadorMontoGravado=1`) are handled, and so are global discounts and surcharges. `IndicadorNotaCredito` is also set.
- **Validation.** Enforces the per-type requirement matrix, e-NCF structure, RNC and cédula format, check-digit warnings, dates, lengths, decimals, and line and total arithmetic. Each finding names the exact field that failed.
- **Exact money arithmetic.** Uses a fixed-point decimal type and never float.
- **Digital signature.** Enveloped XMLDSig that matches DGII's reference structure. The signing certificate is loaded from `.p12`/`.pfx`. Security codes and QR (timbre) URLs are generated. The signatures pass three independent checks: this service's verifier, goxmldsig, and libxml2 C14N with OpenSSL.
- **RFCE.** Consumer invoices (type 32) under RD$250,000 automatically produce the signed summary that is sent to `recepcionfc`.
- **DGII client.** Handles the seed → signature → token flow, with token caching and automatic re-authentication on 401. Supported operations:
  - submitting an e-CF or RFCE
  - querying a result by trackId, document status, trackIds, or RFCE status
  - sending a commercial approval
  - voiding sequence ranges
  - the taxpayer directory
- **Receiver endpoints.** These are the services every electronic issuer must expose: `semilla`, `validacioncertificado`, `recepcion` (returns a signed ARECF and detects bad signatures, duplicates, and the wrong buyer RNC), and `aprobacioncomercial`. Routing ignores letter case.
- **Storage.** Every issued and received XML is stored on the filesystem, as DGII requires taxpayers to keep them.

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
| `ECF_DEV_CERT` | `false` | Generate a self-signed certificate. Not allowed in production. |
| `ECF_API_KEY` | — | Required in `X-API-Key` (or `Authorization: Bearer`) for `/api/v1` |
| `ECF_STORAGE_DIR` | `./data` | Where XML documents are kept |
| `ECF_RECEPTOR_AUTH` | `false` | Require bearer tokens on `/fe/recepcion` and `/fe/aprobacioncomercial` |

The receiver services must be reachable from the internet over HTTPS on standard ports. Terminate TLS at a reverse proxy or load balancer, then register the URLs with DGII.

## API

### Business API (`/api/v1`)

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/ecf/validar[?completar=false]` | Complete and validate an e-CF (JSON). Returns the findings and the completed document. |
| POST | `/api/v1/ecf/xml` | Return the unsigned XML |
| POST | `/api/v1/ecf/emitir[?enviar=true]` | Complete, validate, sign, and store. With `enviar=true` it also submits to DGII: the e-CF goes to `recepcion`, and type 32 under RD$250k goes to `recepcionfc` as an RFCE. |
| GET | `/api/v1/ecf/{RNC+eNCF}` | Fetch a stored signed e-CF |
| GET | `/api/v1/dgii/resultado/{trackId}` | Validation result |
| GET | `/api/v1/dgii/estado?rncemisor&encf[&rnccomprador&codigoseguridad]` | Document status |
| GET | `/api/v1/dgii/trackids?rncemisor&encf` | Submissions for an e-NCF |
| GET | `/api/v1/dgii/rfce?rncemisor&encf&codigoseguridad` | RFCE status |
| GET | `/api/v1/dgii/directorio[?rnc]` | Electronic taxpayers directory |
| POST | `/api/v1/aprobacion-comercial[?enviar=true]` | Sign (and send) an ACECF |
| POST | `/api/v1/anulacion[?enviar=true]` | Sign (and send) an ANECF. Counts and line numbers are computed for you. |
| POST | `/api/v1/xml/firmar` | Sign any DGII XML document |
| POST | `/api/v1/xml/verificar` | Verify a signature and return the signer and security code |
| GET | `/healthz` | Health check |

Example:

```sh
curl -s -X POST localhost:8080/api/v1/ecf/emitir \
  -H 'Content-Type: application/json' -H 'X-API-Key: change-me' \
  --data @examples/factura-credito-fiscal-31.json
```

```json
{
  "eNCF": "E310000000001",
  "archivo": "131880738E310000000001.xml",
  "fechaHoraFirma": "07-10-2026 14:56:16",
  "codigoSeguridad": "inXLSA",
  "urlTimbre": "https://ecf.dgii.gov.do/testecf/consultatimbre?rncemisor=131880738&rnccomprador=101672919&encf=E310000000001&...",
  "montoTotal": "4602.00",
  "esResumen": false,
  "xml": "<?xml version=\"1.0\" encoding=\"utf-8\"?><ECF>...<Signature ...>...</Signature></ECF>"
}
```

Unknown JSON keys are rejected, so a misspelled DGII tag fails loudly instead of being dropped.

### Receiver endpoints (`/fe`, required by DGII)

| Method | Path | Description |
|---|---|---|
| GET | `/fe/autenticacion/api/semilla` | Returns a single-use seed |
| POST | `/fe/autenticacion/api/validacioncertificado` | Signed seed → token (1 h) |
| POST | `/fe/recepcion/api/ecf` | Receives an e-CF and answers with a signed ARECF |
| POST | `/fe/aprobacioncomercial/api/ecf` | Receives an ACECF (HTTP 200/400) |

## Layout

```
cmd/ecfd            service entry point
internal/decimal    fixed-point decimal type
internal/ecf        e-CF/RFCE/ARECF/ACECF/ANECF models, completion, validation, XML, QR URLs
internal/xmldsig    XMLDSig signing and verification, PKCS#12 loading
internal/dgii       DGII web services client and client for other taxpayers' receivers
internal/receptor   inbound /fe/* endpoints
internal/emision    issuance orchestration
internal/api        business REST API
internal/store      document storage (filesystem, memory)
```

## Tests

```sh
go test ./...
go test -bench . ./internal/ecf ./internal/xmldsig
```

## Known gaps / next steps

- **Selective consumption taxes and other additional taxes.** ISC específico, ISC ad valorem, and other additional taxes (Tabla I codes 001–039) are passed through as given and added to `MontoTotal`. Their per-code formulas and rate tables are not computed yet.
- **Code tables.** Validation against the provinces/municipalities, units of measure, and currencies tables is not implemented yet.
- **Item-level foreign currency.** Item consistency checks for `OtraMoneda` are not implemented yet.
- **Paginación** subtotals are not validated.
- **Submission tracking.** Asynchronous polling of `ConsultarResultado` after submission, with persistence of trackIds and states, is not implemented. Today the trackId is returned and queried on demand.
- **Printed representation.** A PDF with a QR code is not generated. The QR URL is.
- **Delivery to buyers.** Issued e-CF are not yet delivered to the buyer's receiver automatically. `dgii.ReceptorClient` already implements the transport.
- **Test against DGII.** The service has not been run end to end against the pre-certification environment, which needs a real certificate and an enabled RNC.
