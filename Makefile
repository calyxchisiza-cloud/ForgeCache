.PHONY: test race bench demo

test:
	go test ./...

race:
	go test -race ./...

bench:
	go test -bench=. -benchmem ./...

demo:
	go run ./cmd/demo
