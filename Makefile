all: test

test:
	go fmt ./...
	go test ./...

upgrade:
	go get -u ./...
	go mod tidy
