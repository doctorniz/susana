.PHONY: build test lint run docker

build:
	go build -o susana ./cmd/susana

test:
	go test -race ./...

lint:
	gofmt -l . | (! grep .)
	go vet ./...

run: build
	./susana -config config/susana.yaml

docker:
	docker build -t susana:dev .
