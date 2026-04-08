package service

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/suite"
)

// ─────────────────────────────────────────────
// Suite definition
// ─────────────────────────────────────────────

type SubscriptionModificationServiceSuite struct {
	testutil.BaseServiceTestSuite
	service SubscriptionModificationService
}

func TestSubscriptionModificationServiceSuite(t *testing.T) {
	suite.Run(t, new(SubscriptionModificationServiceSuite))
}

func (s *SubscriptionModificationServiceSuite) SetupSuite() {
	s.BaseServiceTestSuite.SetupSuite()
}

func (s *SubscriptionModificationServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()
	s.service = NewSubscriptionModificationService(s.buildServiceParams())
}

func (s *SubscriptionModificationServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
}

func (s *SubscriptionModificationServiceSuite) buildServiceParams() ServiceParams {
	return ServiceParams{
		Logger:                     s.GetLogger(),
		Config:                     s.GetConfig(),
		DB:                         s.GetDB(),
		SubRepo:                    s.GetStores().SubscriptionRepo,
		SubscriptionLineItemRepo:   s.GetStores().SubscriptionLineItemRepo,
		SubscriptionPhaseRepo:      s.GetStores().SubscriptionPhaseRepo,
		SubScheduleRepo:            s.GetStores().SubscriptionScheduleRepo,
		PlanRepo:                   s.GetStores().PlanRepo,
		PriceRepo:                  s.GetStores().PriceRepo,
		PriceUnitRepo:              s.GetStores().PriceUnitRepo,
		EventRepo:                  s.GetStores().EventRepo,
		MeterRepo:                  s.GetStores().MeterRepo,
		CustomerRepo:               s.GetStores().CustomerRepo,
		InvoiceRepo:                s.GetStores().InvoiceRepo,
		InvoiceLineItemRepo:        s.GetStores().InvoiceLineItemRepo,
		EntitlementRepo:            s.GetStores().EntitlementRepo,
		EnvironmentRepo:            s.GetStores().EnvironmentRepo,
		FeatureRepo:                s.GetStores().FeatureRepo,
		TenantRepo:                 s.GetStores().TenantRepo,
		UserRepo:                   s.GetStores().UserRepo,
		AuthRepo:                   s.GetStores().AuthRepo,
		WalletRepo:                 s.GetStores().WalletRepo,
		PaymentRepo:                s.GetStores().PaymentRepo,
		CreditGrantRepo:            s.GetStores().CreditGrantRepo,
		CreditGrantApplicationRepo: s.GetStores().CreditGrantApplicationRepo,
		CouponRepo:                 s.GetStores().CouponRepo,
		CouponAssociationRepo:      s.GetStores().CouponAssociationRepo,
		CouponApplicationRepo:      s.GetStores().CouponApplicationRepo,
		AddonRepo:                  testutil.NewInMemoryAddonStore(),
		AddonAssociationRepo:       s.GetStores().AddonAssociationRepo,
		ConnectionRepo:             s.GetStores().ConnectionRepo,
		SettingsRepo:               s.GetStores().SettingsRepo,
		TaxAssociationRepo:         s.GetStores().TaxAssociationRepo,
		TaxRateRepo:                s.GetStores().TaxRateRepo,
		EventPublisher:             s.GetPublisher(),
		WebhookPublisher:           s.GetWebhookPublisher(),
		ProrationCalculator:        s.GetCalculator(),
		FeatureUsageRepo:           s.GetStores().FeatureUsageRepo,
		IntegrationFactory:         s.GetIntegrationFactory(),
	}
}

// ─────────────────────────────────────────────
// Test helpers
// ─────────────────────────────────────────────

func (s *SubscriptionModificationServiceSuite) createCustomer(externalID string) *customer.Customer {
	ctx := s.GetContext()
	c := &customer.Customer{
		ID:         types.GenerateUUIDWithPrefix(types.UUID_PREFIX_CUSTOMER),
		BaseModel:  types.GetDefaultBaseModel(ctx),
		ExternalID: externalID,
		Name:       "Test " + externalID,
		Email:      externalID + "@test.com",
	}
	s.Require().NoError(s.GetStores().CustomerRepo.Create(ctx, c))
	return c
}

func (s *SubscriptionModificationServiceSuite) createPlan() *plan.Plan {
	ctx := s.GetContext()
	p := &plan.Plan{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PLAN),
		Name:      "Test Plan",
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.Require().NoError(s.GetStores().PlanRepo.Create(ctx, p))
	return p
}

func (s *SubscriptionModificationServiceSuite) createActiveSub(customerID string) *subscription.Subscription {
	ctx := s.GetContext()
	now := s.GetNow()
	p := s.createPlan()
	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		CustomerID:         customerID,
		PlanID:             p.ID,
		Currency:           "USD",
		BillingPeriod:      types.BILLING_PERIOD_MONTHLY,
		BillingPeriodCount: 1,
		BillingCycle:       types.BillingCycleAnniversary,
		BillingAnchor:      now,
		SubscriptionStatus: types.SubscriptionStatusActive,
		SubscriptionType:   types.SubscriptionTypeStandalone,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.AddDate(0, 1, 0),
		StartDate:          now,
	}
	s.Require().NoError(s.GetStores().SubscriptionRepo.Create(ctx, sub))
	return sub
}

func (s *SubscriptionModificationServiceSuite) createFixedLineItem(subID, customerID string, qty decimal.Decimal, cadence types.InvoiceCadence) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        types.GenerateUUID(),
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       qty,
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
		StartDate:      now,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// createFixedPrice inserts a Price record into PriceRepo and returns it.
// Used by proration tests that need GetPrice to succeed.
func (s *SubscriptionModificationServiceSuite) createFixedPrice(
	amount decimal.Decimal,
	cadence types.InvoiceCadence,
) *price.Price {
	ctx := s.GetContext()
	p := &price.Price{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		Amount:         amount,
		Currency:       "USD",
		Type:           types.PRICE_TYPE_FIXED,
		BillingModel:   types.BILLING_MODEL_FLAT_FEE,
		BillingCadence: types.BILLING_CADENCE_RECURRING,
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
	}
	s.Require().NoError(s.GetStores().PriceRepo.Create(ctx, p))
	return p
}

// createFixedLineItemWithPrice creates a SubscriptionLineItem tied to a specific PriceID.
// Use this instead of createFixedLineItem when proration tests require GetPrice to resolve.
func (s *SubscriptionModificationServiceSuite) createFixedLineItemWithPrice(
	subID, customerID string,
	qty decimal.Decimal,
	cadence types.InvoiceCadence,
	priceID string,
) *subscription.SubscriptionLineItem {
	ctx := s.GetContext()
	now := s.GetNow()
	li := &subscription.SubscriptionLineItem{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		BaseModel:      types.GetDefaultBaseModel(ctx),
		SubscriptionID: subID,
		CustomerID:     customerID,
		PriceID:        priceID,
		PriceType:      types.PRICE_TYPE_FIXED,
		Quantity:       qty,
		Currency:       "USD",
		BillingPeriod:  types.BILLING_PERIOD_MONTHLY,
		InvoiceCadence: cadence,
		StartDate:      now,
		EntityType:     types.SubscriptionLineItemEntityTypePlan,
	}
	s.Require().NoError(s.GetStores().SubscriptionLineItemRepo.Create(ctx, li))
	return li
}

// setSubPeriod overrides CurrentPeriodStart and CurrentPeriodEnd on the subscription
// stored in SubRepo. Use in math-regression tests that need a deterministic calendar month.
func (s *SubscriptionModificationServiceSuite) setSubPeriod(subID string, start, end time.Time) {
	ctx := s.GetContext()
	sub, err := s.GetStores().SubscriptionRepo.Get(ctx, subID)
	s.Require().NoError(err)
	sub.CurrentPeriodStart = start
	sub.CurrentPeriodEnd = end
	sub.BillingAnchor = start
	s.Require().NoError(s.GetStores().SubscriptionRepo.Update(ctx, sub))
}

// ─────────────────────────────────────────────
// Inheritance tests
// ─────────────────────────────────────────────

// TestExecuteInheritance_Success verifies that a standalone subscription is promoted to parent
// and a child inherited subscription is created for the given external customer.
func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_Success() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-parent-001")
	child := s.createCustomer("ext-child-001")
	sub := s.createActiveSub(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type:                                     dto.SubscriptionModifyTypeInheritance,
		ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
	}

	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Two changed subscriptions: parent updated + child created
	s.Require().Len(resp.ChangedResources.Subscriptions, 2)

	actions := make(map[string]int)
	for _, cs := range resp.ChangedResources.Subscriptions {
		actions[cs.Action]++
	}
	s.Equal(1, actions["updated"], "expected one 'updated' entry")
	s.Equal(1, actions["created"], "expected one 'created' entry")

	// The parent subscription type should now be "parent"
	updatedSub, err := s.GetStores().SubscriptionRepo.Get(ctx, sub.ID)
	s.Require().NoError(err)
	s.Equal(types.SubscriptionTypeParent, updatedSub.SubscriptionType)
}

// TestExecuteInheritance_DuplicateChildRejected verifies that adding the same child twice
// returns an error on the second call.
func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_DuplicateChildRejected() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-parent-002")
	child := s.createCustomer("ext-child-002")
	sub := s.createActiveSub(parent.ID)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type:                                     dto.SubscriptionModifyTypeInheritance,
		ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
	}

	// First call should succeed
	_, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)

	// Second call with same child should fail
	_, err = s.service.Execute(ctx, sub.ID, req)
	s.Require().Error(err)
}

// TestExecuteInheritance_InheritedSubCannotAddChildren verifies that calling Execute on
// an inherited subscription returns an error.
func (s *SubscriptionModificationServiceSuite) TestExecuteInheritance_InheritedSubCannotAddChildren() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-parent-003")
	child := s.createCustomer("ext-child-003")
	grandchild := s.createCustomer("ext-grandchild-003")

	parentSub := s.createActiveSub(parent.ID)

	// Create the first inheritance (parent -> child)
	_, err := s.service.Execute(ctx, parentSub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type:                                     dto.SubscriptionModifyTypeInheritance,
		ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
	})
	s.Require().NoError(err)

	// Find the inherited (child) subscription
	filter := types.NewNoLimitSubscriptionFilter()
	filter.CustomerID = child.ID
	subs, err := s.GetStores().SubscriptionRepo.List(ctx, filter)
	s.Require().NoError(err)
	s.Require().Len(subs, 1)
	childSub := subs[0]
	s.Equal(types.SubscriptionTypeInherited, childSub.SubscriptionType)

	// Attempting to add children to an inherited subscription should fail
	_, err = s.service.Execute(ctx, childSub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type:                                     dto.SubscriptionModifyTypeInheritance,
		ExternalCustomerIDsToInheritSubscription: []string{grandchild.ExternalID},
	})
	s.Require().Error(err)
}

// ─────────────────────────────────────────────
// Quantity change tests
// ─────────────────────────────────────────────

// TestExecuteQuantityChange_VersionsLineItem verifies that after Execute, the old line item
// has EndDate set and a new one is created with the updated quantity.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_VersionsLineItem() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-001")
	sub := s.createActiveSub(cust.ID)
	oldQty := decimal.NewFromInt(5)
	li := s.createFixedLineItem(sub.ID, cust.ID, oldQty, types.InvoiceCadenceArrear)

	newQty := decimal.NewFromInt(10)
	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: newQty},
		},
	}

	resp, err := s.service.Execute(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Response should have exactly 2 changed line items (ended + created)
	s.Require().Len(resp.ChangedResources.LineItems, 2)

	actions := make(map[string]int)
	for _, cli := range resp.ChangedResources.LineItems {
		actions[cli.ChangeAction]++
	}
	s.Equal(1, actions["ended"], "expected one 'ended' entry")
	s.Equal(1, actions["created"], "expected one 'created' entry")

	// Verify old line item has EndDate set in the store
	oldLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.False(oldLI.EndDate.IsZero(), "old line item EndDate should be set after execute")

	// Verify new line item exists with updated quantity
	var newLIID string
	for _, cli := range resp.ChangedResources.LineItems {
		if cli.ChangeAction == "created" {
			newLIID = cli.ID
		}
	}
	s.Require().NotEmpty(newLIID)
	newLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, newLIID)
	s.Require().NoError(err)
	s.True(newQty.Equal(newLI.Quantity), "new line item should have updated quantity")
}

// TestExecuteQuantityChange_WrongSubscriptionRejected verifies that providing a line item
// from a different subscription returns an error.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_WrongSubscriptionRejected() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-002")
	sub1 := s.createActiveSub(cust.ID)
	sub2 := s.createActiveSub(cust.ID)

	// Create a line item belonging to sub2
	li := s.createFixedLineItem(sub2.ID, cust.ID, decimal.NewFromInt(3), types.InvoiceCadenceArrear)

	// Execute against sub1 with sub2's line item
	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(7)},
		},
	}

	_, err := s.service.Execute(ctx, sub1.ID, req)
	s.Require().Error(err)
}

// TestPreviewQuantityChange_DoesNotPersist verifies that after Preview, the original line item
// is unchanged (EndDate still zero).
func (s *SubscriptionModificationServiceSuite) TestPreviewQuantityChange_DoesNotPersist() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-003")
	sub := s.createActiveSub(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)

	req := dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(10)},
		},
	}

	resp, err := s.service.Preview(ctx, sub.ID, req)
	s.Require().NoError(err)
	s.Require().NotNil(resp)

	// Original line item should be untouched in store
	origLI, err := s.GetStores().SubscriptionLineItemRepo.Get(ctx, li.ID)
	s.Require().NoError(err)
	s.True(origLI.EndDate.IsZero(), "Preview must not persist changes; EndDate should still be zero")
}

// TestExecuteQuantityChange_InvalidRequestRejected verifies that empty LineItems or zero
// quantity are rejected with validation errors.
func (s *SubscriptionModificationServiceSuite) TestExecuteQuantityChange_InvalidRequestRejected() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-qty-004")
	sub := s.createActiveSub(cust.ID)
	li := s.createFixedLineItem(sub.ID, cust.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)

	// Empty line items slice
	_, err := s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type:      dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{},
	})
	s.Require().Error(err, "empty LineItems should be rejected")

	// Zero quantity
	_, err = s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyTypeQuantityChange,
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.Zero},
		},
	})
	s.Require().Error(err, "zero quantity should be rejected")
}

// ─────────────────────────────────────────────
// Validation tests
// ─────────────────────────────────────────────

// TestExecute_UnknownTypeRejected verifies that an unknown modification type returns a
// validation error.
func (s *SubscriptionModificationServiceSuite) TestExecute_UnknownTypeRejected() {
	ctx := s.GetContext()

	cust := s.createCustomer("ext-unknown-001")
	sub := s.createActiveSub(cust.ID)

	_, err := s.service.Execute(ctx, sub.ID, dto.ExecuteSubscriptionModifyRequest{
		Type: dto.SubscriptionModifyType("unknown"),
	})
	s.Require().Error(err)
}

// TestExecute_ModalityMixedPayloadRejected verifies that inheritance requests must not
// include line_items and quantity_change requests must not include external_customer_ids_to_inherit_subscription.
func (s *SubscriptionModificationServiceSuite) TestExecute_ModalityMixedPayloadRejected() {
	ctx := s.GetContext()

	parent := s.createCustomer("ext-mix-001")
	child := s.createCustomer("ext-mix-002")
	custQty := s.createCustomer("ext-mix-003")
	subInh := s.createActiveSub(parent.ID)
	subQty := s.createActiveSub(custQty.ID)
	li := s.createFixedLineItem(subQty.ID, custQty.ID, decimal.NewFromInt(5), types.InvoiceCadenceArrear)

	_, err := s.service.Execute(ctx, subInh.ID, dto.ExecuteSubscriptionModifyRequest{
		Type:                                     dto.SubscriptionModifyTypeInheritance,
		ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(9)},
		},
	})
	s.Require().Error(err, "inheritance with line_items should be rejected")

	_, err = s.service.Execute(ctx, subQty.ID, dto.ExecuteSubscriptionModifyRequest{
		Type:                                     dto.SubscriptionModifyTypeQuantityChange,
		ExternalCustomerIDsToInheritSubscription: []string{child.ExternalID},
		LineItems: []dto.LineItemQuantityChange{
			{ID: li.ID, Quantity: decimal.NewFromInt(7)},
		},
	})
	s.Require().Error(err, "quantity_change with external_customer_ids_to_inherit_subscription should be rejected")
}
