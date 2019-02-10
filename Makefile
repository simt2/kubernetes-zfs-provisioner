SHELL := /usr/bin/env bash

prepare:
	sudo zfs create test/volumes	
	sudo chown $$(whoami) $$(zfs get -Ho value mountpoint test/volumes)
	sudo zfs allow $$(whoami) create,destroy,snapshot test/volumes

test:
	sudo zfs create test/volumes
	sudo -E go test `go list ./... | grep -v vendor | grep pkg`
.PHONY: test

clean: 
	sudo zfs destroy -r test/volumes
.PHONY: clean

build: 
	mkdir -p bin
	env GOOS=linux go build -o bin/zfs-provisioner cmd/zfs-provisioner/main.go
.PHONY: build