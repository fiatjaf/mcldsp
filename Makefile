dist: $(shell find . -name "*.go")
	mkdir -p dist
	gox -ldflags="-s -w" -tags="full" -osarch="darwin/amd64 linux/386 linux/amd64 linux/arm freebsd/amd64" -output="dist/mcldsp_{{.OS}}_{{.Arch}}"

mcldsp: $(shell find -name "*.go")
	go build -ldflags="-s -w" -o mcldsp
