TARGET=./build
ARCHS=amd64 386
LDFLAGS="-s -w"
PROG=gochro

.DEFAULT_GOAL := build

.PHONY: all
all: clean update lint windows linux darwin

.PHONY: docker-update
docker-update:
	wget https://raw.githubusercontent.com/jessfraz/dotfiles/master/etc/docker/seccomp/chrome.json -O ./chrome.json
	docker pull golang:latest
	docker pull alpine:latest
	docker build --tag ${PROG}:dev .

.PHONY: docker-run
docker-run: docker-update
	docker run --init --rm -p 8000:8000 --security-opt seccomp=chrome.json ${PROG}:dev -host 0.0.0.0:8000 -debug -ignore-cert-errors

.PHONY: docker-run-daemon
docker-run-daemon: docker-update
	docker run --init --rm -d -p 8000:8000 --security-opt seccomp=chrome.json ${PROG}:dev -host 0.0.0.0:8000 -ignore-cert-errors

.PHONY: windows
windows:
	@mkdir -p ${TARGET} ; \
	for GOARCH in ${ARCHS}; do \
		echo "Building for windows $${GOARCH} ..." ; \
		GOOS=windows GOARCH=$${GOARCH} go build -ldflags=${LDFLAGS} -trimpath -o ${TARGET}/${PROG}-windows-$${GOARCH}.exe ; \
	done;

.PHONY: linux
linux:
	@mkdir -p ${TARGET} ; \
	for GOARCH in ${ARCHS}; do \
		echo "Building for linux $${GOARCH} ..." ; \
		GOOS=linux GOARCH=$${GOARCH} go build -ldflags=${LDFLAGS} -trimpath -o ${TARGET}/${PROG}-linux-$${GOARCH} ; \
	done;

.PHONY: darwin
darwin:
	@mkdir -p ${TARGET} ; \
	for GOARCH in ${ARCHS}; do \
		echo "Building for darwin $${GOARCH} ..." ; \
		GOOS=darwin GOARCH=$${GOARCH} go build -ldflags=${LDFLAGS} -trimpath -o ${TARGET}/${PROG}-darwin-$${GOARCH} ; \
	done;

.PHONY: clean
clean:
	@rm -rf ${TARGET}/*

.PHONY: lint
lint:
	"$$(go env GOPATH)/bin/golangci-lint" run ./...
	go mod tidy

.PHONY: lint-update
lint-update:
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin
	$$(go env GOPATH)/bin/golangci-lint --version

.PHONY: lint-docker
lint-docker:
	docker pull golangci/golangci-lint:latest
	docker run --rm -v $$(pwd):/app -w /app golangci/golangci-lint:latest golangci-lint run

.PHONY: update
update:
	go get -u
	go mod tidy -v
	go fmt ./...
	go vet ./...

.PHONY: build
build:
	go build -o gochro
