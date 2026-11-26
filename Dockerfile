FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /susanah ./cmd/susanah

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /susanah /susanah
EXPOSE 8686
USER nonroot:nonroot
ENTRYPOINT ["/susanah"]
CMD ["-config", "/config/susanah.yaml"]
