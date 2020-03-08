mcldsp: $(shell find -name "*.go")
	go build -ldflags="-s -w" -o mcldsp
