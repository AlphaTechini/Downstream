.PHONY: verify test

verify:
	python3 scripts/validate_yaml.py
	python3 scripts/check_evidence.py

# Go checks for the engine and the demo app. Needs Go 1.22 or newer; integration tests
# that need k6 skip themselves when it is not on PATH.
test:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...
