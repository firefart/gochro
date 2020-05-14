TARGET=./build
ARCHS=amd64 386
LDFLAGS="-s -w"
PROG=gochro

.DEFAULT_GOAL := build

all: clean update lint windows linux darwin

docker-update:
	wget https://raw.githubusercontent.com/jessfraz/dotfiles/master/etc/docker/seccomp/chrome.json -O ./chrome.json
	docker pull golang:latest
	docker pull alpine:latest
	docker build --tag ${PROG}:dev .

docker-run: docker-update
	docker run --init --rm -p 8000:8000 --security-opt seccomp=chrome.json ${PROG}:dev -host 0.0.0.0:8000 -debug -ignore-cert-errors

docker-run-daemon: docker-update
	docker run --init --rm -d -p 8000:8000 --security-opt seccomp=chrome.json ${PROG}:dev -host 0.0.0.0:8000 -ignore-cert-errors

windows:
	@mkdir -p ${TARGET} ; \
	for GOARCH in ${ARCHS}; do \
		echo "Building for windows $${GOARCH} ..." ; \
		GOOS=windows GOARCH=$${GOARCH} go build -ldflags=${LDFLAGS} -trimpath -o ${TARGET}/${PROG}-windows-$${GOARCH}.exe ; \
	done;

linux:
	@mkdir -p ${TARGET} ; \
	for GOARCH in ${ARCHS}; do \
		echo "Building for linux $${GOARCH} ..." ; \
		GOOS=linux GOARCH=$${GOARCH} go build -ldflags=${LDFLAGS} -trimpath -o ${TARGET}/${PROG}-linux-$${GOARCH} ; \
	done;

darwin:
	@mkdir -p ${TARGET} ; \
	for GOARCH in ${ARCHS}; do \
		echo "Building for darwin $${GOARCH} ..." ; \
		GOOS=darwin GOARCH=$${GOARCH} go build -ldflags=${LDFLAGS} -trimpath -o ${TARGET}/${PROG}-darwin-$${GOARCH} ; \
	done;

clean:
	@rm -rf ${TARGET}/*

lint:
	@if [ ! -f "$$(go env GOPATH)/bin/golangci-lint" ]; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin v1.26.0; \
	fi
	"$$(go env GOPATH)/bin/golangci-lint" run ./...
	go mod tidy

update:
	go get -u
	go mod tidy -v
	go fmt ./...
	go vet ./...

build:
	go build -o gochro
