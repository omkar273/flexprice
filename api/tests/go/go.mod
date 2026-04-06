module github.com/flexprice/flexprice/api/tests/go

go 1.22

require github.com/flexprice/go-sdk/v2 v2.0.16

require github.com/stretchr/testify v1.11.1 // indirect

// Local SDK generated in api/go (same module as published go-sdk)
replace github.com/flexprice/go-sdk/v2 => ../../go
