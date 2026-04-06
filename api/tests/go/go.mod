module github.com/flexprice/flexprice/api/tests/go

go 1.22

require github.com/flexprice/go-sdk/v2 v2.1.0

require github.com/stretchr/testify v1.11.1 // indirect

// Integration tests run against the generated SDK in this repo. Remove for published-module-only checks.
replace github.com/flexprice/go-sdk/v2 => ../../go
