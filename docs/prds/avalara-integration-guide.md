# Avalara Integration Implementation Guide for Flexprice

## Overview

This guide provides step-by-step implementation details for integrating Avalara AvaTax into Flexprice's existing tax system while maintaining backward compatibility with the current TaxRate → TaxAssociation → TaxApplied architecture.

## Current Tax System Architecture

Based on the codebase analysis, Flexprice has a well-designed tax system:

```
TaxRate (defines rates/rules)
    ↓ (via TaxAssociation)
Invoice/LineItem → TaxApplied (actual tax calculations)
```

## Implementation Strategy

### Phase 1: Create Tax Provider Abstraction

### Phase 2: Implement Avalara Provider

### Phase 3: Integrate with Existing Tax Service

### Phase 4: Add Configuration & Fallback Logic

## Phase 1: Tax Provider Interface

Since Go doesn't have an official Avalara SDK, we'll use their REST API directly.

### 1.1 Create Tax Provider Interface

```go
// internal/service/taxprovider/interface.go
package taxprovider

import (
    "context"
    "time"
    "github.com/shopspring/decimal"
)

type TaxProvider interface {
    CalculateTax(ctx context.Context, req *TaxCalculationRequest) (*TaxCalculationResponse, error)
    CommitTax(ctx context.Context, req *TaxCommitRequest) (*TaxCommitResponse, error)
    VoidTax(ctx context.Context, req *TaxVoidRequest) error
    ValidateAddress(ctx context.Context, address *Address) (*ValidatedAddress, error)
}

type TaxCalculationRequest struct {
    DocumentCode    string
    DocumentType    string // "SalesInvoice", "ReturnInvoice"
    CompanyCode     string
    Date           time.Time
    CustomerCode   string
    Lines          []TaxLineItem
    Addresses      AddressInfo
    CurrencyCode   string
    ExemptionNo    string
}

type TaxLineItem struct {
    Number          string
    ItemCode        string
    TaxCode         string
    Description     string
    Amount          decimal.Decimal
    Quantity        decimal.Decimal
    Addresses       AddressInfo
    TaxIncluded     bool
}

type AddressInfo struct {
    ShipFrom *Address `json:"shipFrom,omitempty"`
    ShipTo   *Address `json:"shipTo,omitempty"`
    BillTo   *Address `json:"billTo,omitempty"`
}

type Address struct {
    Line1       string `json:"line1"`
    Line2       string `json:"line2,omitempty"`
    City        string `json:"city"`
    Region      string `json:"region"` // State/Province
    Country     string `json:"country"`
    PostalCode  string `json:"postalCode"`
}

type TaxCalculationResponse struct {
    TotalAmount    decimal.Decimal
    TotalTax       decimal.Decimal
    Lines          []TaxLineResponse
    Addresses      []AddressResponse
    Summary        []TaxSummary
}

type TaxLineResponse struct {
    LineNumber     string
    TaxableAmount  decimal.Decimal
    Tax            decimal.Decimal
    Rate           decimal.Decimal
    Details        []TaxDetail
}

type TaxDetail struct {
    TaxName        string
    Rate           decimal.Decimal
    Tax            decimal.Decimal
    TaxableAmount  decimal.Decimal
    JurisCode      string
    JurisName      string
    JurisType      string
}
```

### 1.2 Create Avalara HTTP Client

```go
// internal/service/taxprovider/avalara/client.go
package avalara

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

type Config struct {
    BaseURL     string
    AccountID   string
    LicenseKey  string
    CompanyCode string
    Timeout     time.Duration
    Environment string // "sandbox" or "production"
}

type Client struct {
    config     Config
    httpClient *http.Client
}

func NewClient(config Config) *Client {
    if config.Timeout == 0 {
        config.Timeout = 30 * time.Second
    }

    if config.BaseURL == "" {
        if config.Environment == "production" {
            config.BaseURL = "https://rest.avatax.com"
        } else {
            config.BaseURL = "https://sandbox-rest.avatax.com"
        }
    }

    return &Client{
        config: config,
        httpClient: &http.Client{
            Timeout: config.Timeout,
        },
    }
}

func (c *Client) makeRequest(ctx context.Context, method, endpoint string, body interface{}) (*http.Response, error) {
    var reqBody io.Reader
    if body != nil {
        jsonData, err := json.Marshal(body)
        if err != nil {
            return nil, fmt.Errorf("failed to marshal request body: %w", err)
        }
        reqBody = bytes.NewReader(jsonData)
    }

    req, err := http.NewRequestWithContext(ctx, method, c.config.BaseURL+endpoint, reqBody)
    if err != nil {
        return nil, fmt.Errorf("failed to create request: %w", err)
    }

    // Set headers
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "application/json")
    req.SetBasicAuth(c.config.AccountID, c.config.LicenseKey)

    resp, err := c.httpClient.Do(req)
    if err != nil {
        return nil, fmt.Errorf("request failed: %w", err)
    }

    if resp.StatusCode >= 400 {
        defer resp.Body.Close()
        body, _ := io.ReadAll(resp.Body)
        return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
    }

    return resp, nil
}
```

## Phase 2: Avalara Provider Implementation

### 2.1 Main Avalara Provider

```go
// internal/service/taxprovider/avalara/provider.go
package avalara

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "your-project/internal/service/taxprovider"
)

type Provider struct {
    client *Client
}

func NewProvider(config Config) *Provider {
    return &Provider{
        client: NewClient(config),
    }
}

func (p *Provider) CalculateTax(ctx context.Context, req *taxprovider.TaxCalculationRequest) (*taxprovider.TaxCalculationResponse, error) {
    // Convert Flexprice request to Avalara format
    avalaraReq := p.convertToAvalaraRequest(req)

    resp, err := p.client.makeRequest(ctx, "POST", "/api/v2/transactions/create", avalaraReq)
    if err != nil {
        return nil, fmt.Errorf("avalara tax calculation failed: %w", err)
    }
    defer resp.Body.Close()

    var avalaraResp AvalaraTransactionResponse
    if err := json.NewDecoder(resp.Body).Decode(&avalaraResp); err != nil {
        return nil, fmt.Errorf("failed to decode avalara response: %w", err)
    }

    // Convert Avalara response back to Flexprice format
    return p.convertFromAvalaraResponse(&avalaraResp), nil
}

func (p *Provider) CommitTax(ctx context.Context, req *taxprovider.TaxCommitRequest) (*taxprovider.TaxCommitResponse, error) {
    commitReq := AvalaraCommitRequest{
        Commit: true,
    }

    endpoint := fmt.Sprintf("/api/v2/companies/%s/transactions/%s/commit",
        p.client.config.CompanyCode, req.DocumentCode)

    resp, err := p.client.makeRequest(ctx, "POST", endpoint, commitReq)
    if err != nil {
        return nil, fmt.Errorf("avalara tax commit failed: %w", err)
    }
    defer resp.Body.Close()

    var avalaraResp AvalaraTransactionResponse
    if err := json.NewDecoder(resp.Body).Decode(&avalaraResp); err != nil {
        return nil, fmt.Errorf("failed to decode commit response: %w", err)
    }

    return &taxprovider.TaxCommitResponse{
        DocumentCode: avalaraResp.Code,
        Status:       avalaraResp.Status,
        CommittedAt:  time.Now(),
    }, nil
}

func (p *Provider) VoidTax(ctx context.Context, req *taxprovider.TaxVoidRequest) error {
    voidReq := AvalaraVoidRequest{
        Code: "DocVoided",
    }

    endpoint := fmt.Sprintf("/api/v2/companies/%s/transactions/%s/void",
        p.client.config.CompanyCode, req.DocumentCode)

    resp, err := p.client.makeRequest(ctx, "POST", endpoint, voidReq)
    if err != nil {
        return fmt.Errorf("avalara tax void failed: %w", err)
    }
    defer resp.Body.Close()

    return nil
}

// Conversion methods
func (p *Provider) convertToAvalaraRequest(req *taxprovider.TaxCalculationRequest) *AvalaraTransactionRequest {
    lines := make([]AvalaraLineItem, len(req.Lines))
    for i, line := range req.Lines {
        lines[i] = AvalaraLineItem{
            Number:      line.Number,
            ItemCode:    line.ItemCode,
            TaxCode:     line.TaxCode,
            Description: line.Description,
            Amount:      line.Amount,
            Quantity:    line.Quantity,
            Addresses:   convertAddresses(line.Addresses),
        }
    }

    return &AvalaraTransactionRequest{
        CompanyCode:  p.client.config.CompanyCode,
        Type:         req.DocumentType,
        Code:         req.DocumentCode,
        Date:         req.Date.Format("2006-01-02"),
        CustomerCode: req.CustomerCode,
        Lines:        lines,
        Addresses:    convertAddresses(req.Addresses),
        CurrencyCode: req.CurrencyCode,
    }
}
```

### 2.2 Avalara Data Types

```go
// internal/service/taxprovider/avalara/types.go
package avalara

import "github.com/shopspring/decimal"

type AvalaraTransactionRequest struct {
    CompanyCode  string              `json:"companyCode"`
    Type         string              `json:"type"`
    Code         string              `json:"code"`
    Date         string              `json:"date"`
    CustomerCode string              `json:"customerCode"`
    Lines        []AvalaraLineItem   `json:"lines"`
    Addresses    AvalaraAddressInfo  `json:"addresses"`
    CurrencyCode string              `json:"currencyCode,omitempty"`
}

type AvalaraLineItem struct {
    Number      string              `json:"number"`
    ItemCode    string              `json:"itemCode,omitempty"`
    TaxCode     string              `json:"taxCode,omitempty"`
    Description string              `json:"description"`
    Amount      decimal.Decimal     `json:"amount"`
    Quantity    decimal.Decimal     `json:"quantity,omitempty"`
    Addresses   AvalaraAddressInfo  `json:"addresses,omitempty"`
}

type AvalaraAddressInfo struct {
    ShipFrom *AvalaraAddress `json:"shipFrom,omitempty"`
    ShipTo   *AvalaraAddress `json:"shipTo,omitempty"`
    BillTo   *AvalaraAddress `json:"billTo,omitempty"`
}

type AvalaraAddress struct {
    Line1      string `json:"line1"`
    Line2      string `json:"line2,omitempty"`
    City       string `json:"city"`
    Region     string `json:"region"`
    Country    string `json:"country"`
    PostalCode string `json:"postalCode"`
}

type AvalaraTransactionResponse struct {
    ID           int64                    `json:"id"`
    Code         string                   `json:"code"`
    CompanyID    int64                    `json:"companyId"`
    Date         string                   `json:"date"`
    Status       string                   `json:"status"`
    Type         string                   `json:"type"`
    TotalAmount  decimal.Decimal          `json:"totalAmount"`
    TotalTax     decimal.Decimal          `json:"totalTax"`
    Lines        []AvalaraLineResponse    `json:"lines"`
    Addresses    []AvalaraAddressResponse `json:"addresses"`
    Summary      []AvalaraTaxSummary      `json:"summary"`
}

type AvalaraLineResponse struct {
    LineNumber    string                `json:"lineNumber"`
    TaxableAmount decimal.Decimal       `json:"taxableAmount"`
    Tax           decimal.Decimal       `json:"tax"`
    Rate          decimal.Decimal       `json:"rate"`
    Details       []AvaларaTaxDetail    `json:"details"`
}

type AvaларaTaxDetail struct {
    TaxName       string          `json:"taxName"`
    Rate          decimal.Decimal `json:"rate"`
    Tax           decimal.Decimal `json:"tax"`
    TaxableAmount decimal.Decimal `json:"taxableAmount"`
    JurisCode     string          `json:"jurisCode"`
    JurisName     string          `json:"jurisName"`
    JurisType     string          `json:"jurisType"`
}
```

## Phase 3: Integration with Existing Tax Service

### 3.1 Enhanced Tax Service

```go
// internal/service/tax_enhanced.go (extends existing tax.go)
package service

import (
    "context"
    "fmt"

    "your-project/internal/service/taxprovider"
    "your-project/internal/service/taxprovider/avalara"
    "your-project/internal/types"
)

type TaxProviderType string

const (
    TaxProviderInternal TaxProviderType = "internal"
    TaxProviderAvalara  TaxProviderType = "avalara"
)

type EnhancedTaxService struct {
    *TaxService // embed existing service
    providers   map[TaxProviderType]taxprovider.TaxProvider
    defaultProvider TaxProviderType
}

func NewEnhancedTaxService(
    taxService *TaxService,
    avalaraConfig *avalara.Config,
) *EnhancedTaxService {
    providers := make(map[TaxProviderType]taxprovider.TaxProvider)

    // Internal provider (existing logic)
    providers[TaxProviderInternal] = &InternalTaxProvider{taxService}

    // Avalara provider
    if avalaraConfig != nil {
        providers[TaxProviderAvalara] = avalara.NewProvider(*avalaraConfig)
    }

    return &EnhancedTaxService{
        TaxService:      taxService,
        providers:       providers,
        defaultProvider: TaxProviderInternal,
    }
}

func (s *EnhancedTaxService) CalculateInvoiceTaxes(
    ctx context.Context,
    invoice *types.Invoice,
    providerType TaxProviderType,
) ([]*types.TaxApplied, error) {
    provider, exists := s.providers[providerType]
    if !exists {
        return nil, fmt.Errorf("tax provider %s not available", providerType)
    }

    // Convert invoice to tax calculation request
    req := s.convertInvoiceToTaxRequest(invoice)

    // Calculate taxes using external provider
    response, err := provider.CalculateTax(ctx, req)
    if err != nil {
        // Fallback to internal provider
        if providerType != TaxProviderInternal {
            return s.CalculateInvoiceTaxes(ctx, invoice, TaxProviderInternal)
        }
        return nil, err
    }

    // Convert response to TaxApplied entities
    return s.convertTaxResponseToApplied(response, invoice)
}

func (s *EnhancedTaxService) convertInvoiceToTaxRequest(invoice *types.Invoice) *taxprovider.TaxCalculationRequest {
    lines := make([]taxprovider.TaxLineItem, len(invoice.LineItems))
    for i, item := range invoice.LineItems {
        lines[i] = taxprovider.TaxLineItem{
            Number:      fmt.Sprintf("%d", i+1),
            ItemCode:    item.ProductID,
            Description: item.Description,
            Amount:      item.Amount,
            Quantity:    item.Quantity,
            TaxCode:     item.TaxCode,
        }
    }

    return &taxprovider.TaxCalculationRequest{
        DocumentCode:  invoice.ID,
        DocumentType:  "SalesInvoice",
        Date:          invoice.Date,
        CustomerCode:  invoice.CustomerID,
        Lines:         lines,
        CurrencyCode:  invoice.Currency,
        Addresses: taxprovider.AddressInfo{
            BillTo: &taxprovider.Address{
                Line1:      invoice.BillingAddress.Line1,
                Line2:      invoice.BillingAddress.Line2,
                City:       invoice.BillingAddress.City,
                Region:     invoice.BillingAddress.State,
                Country:    invoice.BillingAddress.Country,
                PostalCode: invoice.BillingAddress.PostalCode,
            },
        },
    }
}
```

## Phase 4: Configuration & Error Handling

### 4.1 Configuration

```go
// internal/config/tax.go
package config

type TaxConfig struct {
    DefaultProvider string         `yaml:"default_provider" env:"TAX_DEFAULT_PROVIDER" env-default:"internal"`
    Avalara         AvalaraConfig  `yaml:"avalara"`
    FallbackEnabled bool           `yaml:"fallback_enabled" env:"TAX_FALLBACK_ENABLED" env-default:"true"`
}

type AvalaraConfig struct {
    Enabled     bool   `yaml:"enabled" env:"AVALARA_ENABLED" env-default:"false"`
    Environment string `yaml:"environment" env:"AVALARA_ENVIRONMENT" env-default:"sandbox"`
    AccountID   string `yaml:"account_id" env:"AVALARA_ACCOUNT_ID"`
    LicenseKey  string `yaml:"license_key" env:"AVALARA_LICENSE_KEY"`
    CompanyCode string `yaml:"company_code" env:"AVALARA_COMPANY_CODE"`
    BaseURL     string `yaml:"base_url" env:"AVALARA_BASE_URL"`
    Timeout     int    `yaml:"timeout" env:"AVALARA_TIMEOUT" env-default:"30"`
}
```

### 4.2 Error Handling & Monitoring

```go
// internal/service/taxprovider/errors.go
package taxprovider

import "errors"

var (
    ErrProviderUnavailable = errors.New("tax provider unavailable")
    ErrInvalidRequest      = errors.New("invalid tax calculation request")
    ErrRateLimited         = errors.New("tax provider rate limited")
    ErrAuthenticationFailed = errors.New("tax provider authentication failed")
)

type TaxProviderError struct {
    Provider string
    Code     string
    Message  string
    Err      error
}

func (e *TaxProviderError) Error() string {
    return fmt.Sprintf("tax provider %s error [%s]: %s", e.Provider, e.Code, e.Message)
}

func (e *TaxProviderError) Unwrap() error {
    return e.Err
}
```

## Testing Strategy

### Unit Tests

```go
// internal/service/taxprovider/avalara/provider_test.go
func TestAvalaraProvider_CalculateTax(t *testing.T) {
    // Mock HTTP responses
    // Test various scenarios
}
```

### Integration Tests

```go
// test/integration/tax_test.go
func TestTaxCalculation_WithAvalara(t *testing.T) {
    // Test against Avalara sandbox
}
```

## Deployment Checklist

1. **Environment Variables**:

   - `AVALARA_ENABLED=true`
   - `AVALARA_ACCOUNT_ID=your_account`
   - `AVALARA_LICENSE_KEY=your_key`
   - `AVALARA_COMPANY_CODE=your_company`

2. **Database Migration**: No schema changes needed - uses existing tax entities

3. **Monitoring**: Add metrics for tax calculation success/failure rates

4. **Feature Flags**: Use feature flags to gradually roll out Avalara

This implementation maintains your existing tax architecture while adding powerful external tax calculation capabilities through Avalara's REST API.
