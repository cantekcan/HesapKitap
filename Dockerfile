# syntax=docker/dockerfile:1
# Üretilen kod (templ, sqlc, app.css) repoda olduğu için derlemede ek araç gerekmez.

FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/hesapkitap ./cmd/hesapkitap

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/hesapkitap /hesapkitap
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/hesapkitap"]
