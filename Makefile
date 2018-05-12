all: vet test testrace

deps:
	go get -d -v github.com/chalvern/grpc-go/...

updatedeps:
	go get -d -v -u -f github.com/chalvern/grpc-go/...

testdeps:
	go get -d -v -t github.com/chalvern/grpc-go/...

updatetestdeps:
	go get -d -v -t -u -f github.com/chalvern/grpc-go/...

build: deps
	go build github.com/chalvern/grpc-go/...

proto:
	@ if ! which protoc > /dev/null; then \
		echo "error: protoc not installed" >&2; \
		exit 1; \
	fi
	go generate github.com/chalvern/grpc-go/...

vet:
	./vet.sh

test: testdeps
	go test -cpu 1,4 -timeout 5m github.com/chalvern/grpc-go/...

testrace: testdeps
	go test -race -cpu 1,4 -timeout 7m github.com/chalvern/grpc-go/...

clean:
	go clean -i github.com/chalvern/grpc-go/...

.PHONY: \
	all \
	deps \
	updatedeps \
	testdeps \
	updatetestdeps \
	build \
	proto \
	vet \
	test \
	testrace \
	clean
