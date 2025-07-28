VERSION ?= 0.1.1
DOCKERHUB_USER ?= plan9better
LOCAL_REGISTRY ?= 192.168.10.201:5000

.PHONY: publish test all vlanman unit-test e2e-test test-all

publish:
	docker build -t $(DOCKERHUB_USER)/vlanman:$(VERSION) --platform linux/amd64 --file ./operator.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlanman:$(VERSION) 

	docker build -t $(DOCKERHUB_USER)/vlan-manager:$(VERSION) --platform linux/amd64 --file ./manager.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlan-manager:$(VERSION) 

	docker build -t $(DOCKERHUB_USER)/vlan-worker:$(VERSION) --platform linux/amd64 --file ./worker.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlan-worker:$(VERSION) 

	docker build -t $(DOCKERHUB_USER)/vlan-interface:$(VERSION) --platform linux/amd64 --file ./interface.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlan-interface:$(VERSION) 

test:
	docker build -t $(LOCAL_REGISTRY)/vlanman:latest-dev --platform linux/amd64 --file ./operator.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlanman:latest-dev 

	docker build -t $(LOCAL_REGISTRY)/vlan-manager:latest-dev --platform linux/amd64 --file ./manager.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlan-manager:latest-dev 

	docker build -t $(LOCAL_REGISTRY)/vlan-worker:latest-dev --platform linux/amd64 --file ./worker.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlan-worker:latest-dev 

	docker build -t $(LOCAL_REGISTRY)/vlan-interface:latest-dev --platform linux/amd64 --file ./interface.Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlan-interface:latest-dev 

test-local:
	docker build -t $(LOCAL_REGISTRY)/vlanman:$(VERSION) --platform linux/arm64 --file ./operator.Dockerfile --build-arg PLATFORM=arm64 .
	docker push $(LOCAL_REGISTRY)/vlanman:$(VERSION) 

	docker build -t $(LOCAL_REGISTRY)/vlan-manager:$(VERSION) --platform linux/arm64 --file ./manager.Dockerfile --build-arg PLATFORM=arm64 .
	docker push $(LOCAL_REGISTRY)/vlan-manager:$(VERSION) 

	docker build -t $(LOCAL_REGISTRY)/vlan-worker:$(VERSION) --platform linux/arm64 --file ./worker.Dockerfile --build-arg PLATFORM=arm64 .
	docker push $(LOCAL_REGISTRY)/vlan-worker:$(VERSION) 

	docker build -t $(LOCAL_REGISTRY)/vlan-interface:$(VERSION) --platform linux/arm64 --file ./interface.Dockerfile --build-arg PLATFORM=arm64 .
	docker push $(LOCAL_REGISTRY)/vlan-interface:$(VERSION) 


vlanman:
	docker build -t $(LOCAL_REGISTRY)/vlanman:latest-dev --platform linux/arm64 --file ./operator.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlanman:latest-dev 

manager:
	docker build -t $(LOCAL_REGISTRY)/vlan-manager:latest-dev --platform linux/arm64 --file ./manager.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlan-manager:latest-dev 

interface:
	docker build -t $(LOCAL_REGISTRY)/vlan-interface:latest-dev --platform linux/arm64 --file ./interface.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlan-interface:latest-dev 

worker:
	docker build -t $(LOCAL_REGISTRY)/vlan-worker:latest-dev --platform linux/arm64 --file ./worker.Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlan-worker:latest-dev 

all: vlanman manager interface worker

unit-test:
	go test ./internal/controller ./internal/webhook/v1 -race -count 100 

e2e-test:
	kubectl kuttl test ./test/e2e/
linters:
	nilaway -include-pkgs="dialo.ai/vlanman" ./internal/controller ./internal/webhook/v1

test-all: unit-test e2e-test
