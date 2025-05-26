VERSION ?= 0.0.0
DOCKERHUB_USER ?= plan9better
LOCAL_REGISTRY ?= 192.168.10.201:5000

.PHONY: publish test all vlanman

publish:
	docker build -t $(DOCKERHUB_USER)/vlanman:$(VERSION) --platform linux/amd64 --file ./Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(DOCKERHUB_USER)/vlanman:$(VERSION) 

test:
	docker build -t $(LOCAL_REGISTRY)/vlanman:latest-dev --platform linux/amd64 --file ./Dockerfile --build-arg PLATFORM=amd64 .
	docker push $(LOCAL_REGISTRY)/vlanman:latest-dev 

vlanman:
	docker build -t $(LOCAL_REGISTRY)/vlanman:latest-dev --platform linux/arm64 --file ./Dockerfile .
	docker push $(LOCAL_REGISTRY)/vlanman:latest-dev 

vm: vlanman
