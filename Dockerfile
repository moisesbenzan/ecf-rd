FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ecfd ./cmd/ecfd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ecfd /ecfd
ENV ECF_ADDR=:8080 ECF_STORAGE_DIR=/data
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/ecfd"]
