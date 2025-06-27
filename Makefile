VERSION ?= 0.0.0
DOCKERHUB_USER ?= plan9better
LOCAL_REGISTRY ?= 192.168.10.201:5000

.PHONY: publish test all vlanman unit-test e2e-test test-all

publish:
	docker build -t $(DOCKERHUB_USER)/vlanman:$(VERSION) --platform linux/amd64 --file ./operator.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlanman:$(VERSION) 

	docker build -t $(DOCKERHUB_USER)/vlan-manager:$(VERSION) --platform linux/amd64 --file ./manager.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlan-manager:$(VERSION) 

test:
	docker build -t $(LOCAL_REGISTRY)/vlanman:latest-dev --platform linux/amd64 --file ./operator.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlanman:latest-dev 

	docker build -t $(LOCAL_REGISTRY)/vlan-manager:latest-dev --platform linux/amd64 --file ./manager.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlan-manager:latest-dev 

vlanman:
	docker build -t $(LOCAL_REGISTRY)/vlanman:latest-dev --platform linux/arm64 --file ./operator.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlanman:latest-dev 

manager:
	docker build -t $(LOCAL_REGISTRY)/vlan-manager:latest-dev --platform linux/arm64 --file ./manager.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlan-manager:latest-dev 

interface:
	docker build -t $(LOCAL_REGISTRY)/vlan-interface:latest-dev --platform linux/arm64 --file ./interface.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlan-interface:latest-dev 

all: vlanman manager interface

unit-test:
	go test ./internal/controller ./internal/webhook/v1 -race -count 100 

e2e-test:
	kubectl kuttl test ./test/e2e/
linters:
	nilaway -include-pkgs="dialo.ai/vlanman" ./...

test-all: unit-test e2e-test
