FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /susana ./cmd/susana

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /susana /susana
EXPOSE 8686
USER nonroot:nonroot
ENTRYPOINT ["/susana"]
CMD ["-config", "/config/susana.yaml"]
