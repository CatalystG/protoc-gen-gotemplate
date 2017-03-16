.PHONY: build
build:
	go build -o protoc-gen-gotemplate .

.PHONY: install
install:
	go install .

.PHONY: test
test:	build
	cd examples/dummy && make
	cd examples/flow && make
	cd examples/concat && make
	cd examples/flow && make
	cd examples/sitemap && make
	cd examples/go-kit && make

.PHONY: docker.build
docker.build:
	docker build --pull -t moul/protoc-gen-gotemplate .

.PHONY: docker.push
docker.push: docker.build
	docker push moul/protoc-gen-gotemplate
