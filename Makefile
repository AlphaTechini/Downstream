.PHONY: verify
verify:
	python3 scripts/validate_yaml.py
	python3 scripts/check_evidence.py
