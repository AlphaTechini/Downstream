.PHONY: verify test verify-frontend

verify:
	python3 scripts/validate_yaml.py
	python3 scripts/check_evidence.py

# Go checks for the engine and the demo app. Needs Go 1.22 or newer; integration tests
# that need k6 skip themselves when it is not on PATH.
test:
	test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }
	go vet ./...
	go test ./...

# Frontend stage on the React and Svelte fixtures: planted regressions must be caught and a
# clean change must report none. Needs Linux, Go, Node 20.19+ with npm, and registry access.
verify-frontend:
	sh scripts/verify_frontend.sh 2>&1 | tee evidence/frontend-verify.txt
	test "$$(tail -n 1 evidence/frontend-verify.txt)" = "verify-frontend: PASS"
