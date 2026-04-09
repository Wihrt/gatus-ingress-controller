IMAGE_REGISTRY ?= ghcr.io
IMAGE_REPO ?= wihrt/gatus-controller
IMAGE_TAG ?= latest

.PHONY: build
build:
	go build ./...

.PHONY: test
test:
	go test ./... -v

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(IMAGE_TAG) .

.PHONY: docker-push
docker-push:
	docker push $(IMAGE_REGISTRY)/$(IMAGE_REPO):$(IMAGE_TAG)

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ./...
