# Every client against the shared cases.
test: test-typescript test-python test-go

test-typescript:
    cd typescript && node --test test/conformance.test.ts

test-python:
    cd python && python3 -m unittest discover -s tests

test-go:
    cd go && go test ./...

fmt:
    cd go && gofmt -w .
