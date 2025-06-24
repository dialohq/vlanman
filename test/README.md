# Testing Structure for VlanMan

This directory contains the test infrastructure for the VlanMan Kubernetes operator.

## Structure

```
test/
├── README.md
└── e2e/
    ├── kuttl-test.yaml           # KUTTL test suite configuration
    └── tests/
        └── validation/           # Validation webhook tests
            ├── 00-setup.yaml     # Test environment setup
            ├── 01-valid-network.yaml        # Valid VlanNetwork creation
            ├── 01-assert.yaml               # Assertions for valid creation
            ├── 02-duplicate-vlan-id.yaml    # Duplicate VLAN ID test
            ├── 02-errors.yaml               # Expected errors
            ├── 03-all-nodes-excluded.yaml   # All nodes excluded test
            └── 03-errors.yaml               # Expected errors
```

## Unit Tests

Unit tests are located alongside the source code:
- `internal/webhook/v1/validating_webhook_test.go` - Tests for webhook HTTP handlers
- `internal/webhook/v1/validator_test.go` - Tests for validation logic

### Running Unit Tests

```bash
# Run all unit tests
make unit-test

# Run specific package tests
go test ./internal/webhook/v1/ -v
```

## E2E Tests

End-to-end tests use KUTTL (Kubernetes Test Framework) to test the operator in a real Kubernetes environment.

### Running E2E Tests

```bash
# Run e2e tests (requires kubectl and kubernetes cluster)
make e2e-test

# Run all tests
make test-all
```

## Test Coverage

Current test coverage includes:
- ✅ Webhook action detection (create/update/delete/unknown)
- ✅ Admission response generation (allowed/denied)
- ✅ VlanNetwork validation:
  - ✅ Minimum nodes validation (excluding nodes)
  - ✅ Unique VLAN ID validation
  - ✅ JSON unmarshaling validation
- ✅ HTTP request handling
- ✅ Integration with fake Kubernetes client

## Future Test Additions

As development progresses, add tests for:
- [ ] Controller logic tests
- [ ] Mutating webhook tests  
- [ ] Pod creation and networking tests
- [ ] Error handling and edge cases
- [ ] Performance tests