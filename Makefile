.PHONY: build test lint run docker

build:
	go build -o susanah ./cmd/susanah

test:
	go test -race ./...

lint:
	gofmt -l . | (! grep .)
	go vet ./...

run: build
	./susanah -config config/susanah.yaml

docker:
	docker build -t susanah:dev .
