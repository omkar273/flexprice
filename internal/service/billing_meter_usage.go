package service

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	priceDomain "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// CalculateMeterUsageCharges is the meter_usage counterpart of CalculateFeatureUsageCharges.
// It is functionally identical except that bucketed meter and windowed commitment queries
// are routed to MeterUsageRepo.GetUsageForBucketedMeters instead of
// FeatureUsageRepo.GetUsageForBucketedMeters (reads from meter_usage table, not feature_usage).
func (s *billingService) CalculateMeterUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
	opts *CalculateFeatureUsageChargesOpts,
) ([]dto.CreateInvoiceLineItemRequest, decimal.Decimal, error) {

	if usage == nil {
		return nil, decimal.Zero, nil
	}

	var querySource types.UsageSource
	if opts != nil {
		querySource = opts.Source
	}

	usageCharges := make([]dto.CreateInvoiceLineItemRequest, 0)
	totalUsageCost := decimal.Zero
	asOf := time.Now().UTC()

	// Cumulative subscription commitment
	var useCumulativePath bool
	var totalPriorBase decimal.Decimal
	var commitmentStart, commitmentEnd time.Time
	commitmentAmount := lo.FromPtr(sub.CommitmentAmount)
	overageFactor := lo.FromPtr(sub.OverageFactor)
	if sub.HasCommitment() && sub.CommitmentDuration != nil &&
		types.BillingPeriod(*sub.CommitmentDuration) != sub.BillingPeriod &&
		commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1)) {
		var ok bool
		commitmentStart, commitmentEnd, ok = getSubscriptionCommitmentPeriodBounds(sub, periodStart)
		if ok {
			priorBase, hasPrior, err := s.getCumulativePriorBaseFromInvoices(ctx, sub.ID, commitmentStart, periodStart, overageFactor)
			if err != nil {
				return nil, decimal.Zero, err
			}
			if hasPrior {
				useCumulativePath = true
				totalPriorBase = priorBase
			}
		}
	}

	type baseChargeInfo struct {
		item                   *subscription.SubscriptionLineItem
		matchingCharge         *dto.SubscriptionUsageByMetersResponse
		baseAmount             decimal.Decimal
		quantityForCalculation decimal.Decimal
		priceUnitAmount        decimal.Decimal
		displayName            *string
		metadata               types.Metadata
	}
	baseChargesForCumulative := make([]baseChargeInfo, 0)

	subscriptionService := NewSubscriptionService(s.ServiceParams)
	aggregatedEntitlements, err := subscriptionService.GetAggregatedSubscriptionEntitlements(ctx, sub.ID, nil)
	if err != nil {
		return nil, decimal.Zero, err
	}

	entitlementsByMeterID := make(map[string]*dto.AggregatedEntitlement)
	for _, feature := range aggregatedEntitlements.Features {
		if feature.Feature != nil && types.FeatureType(feature.Feature.Type) == types.FeatureTypeMetered &&
			feature.Feature.MeterID != "" && feature.Entitlement != nil {
			entitlementsByMeterID[feature.Feature.MeterID] = feature.Entitlement
		}
	}

	priceService := NewPriceService(s.ServiceParams)

	meterIDs := make([]string, 0)
	for _, item := range sub.LineItems {
		if item.PriceType == types.PRICE_TYPE_USAGE && item.MeterID != "" {
			meterIDs = append(meterIDs, item.MeterID)
		}
	}
	meterIDs = lo.Uniq(meterIDs)

	meterFilter := types.NewNoLimitMeterFilter()
	meterFilter.MeterIDs = meterIDs
	meters, err := s.MeterRepo.List(ctx, meterFilter)
	if err != nil {
		return nil, decimal.Zero, err
	}

	meterMap := make(map[string]*meter.Meter)
	for _, m := range meters {
		meterMap[m.ID] = m
	}

	extCustomerIDsForUsage, err := s.getChildExternalCustomerIDsForSubscription(ctx, sub)
	if err != nil {
		return nil, decimal.Zero, err
	}
	eventService := NewEventService(s.EventRepo, s.MeterRepo, s.EventPublisher, s.Logger, s.Config)

	chargesByLineItemID := make(map[string]*dto.SubscriptionUsageByMetersResponse)
	for _, charge := range usage.Charges {
		chargesByLineItemID[charge.SubscriptionLineItemID] = charge
	}

	for _, item := range sub.LineItems {
		if item.PriceType != types.PRICE_TYPE_USAGE {
			continue
		}

		var matchingCharges []*dto.SubscriptionUsageByMetersResponse
		if charges, ok := chargesByLineItemID[item.ID]; ok {
			matchingCharges = append(matchingCharges, charges)
		}

		if len(matchingCharges) == 0 {
			s.Logger.Debugw("no matching charge found for usage line item",
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)
			continue
		}

		meter, meterOk := meterMap[item.MeterID]
		if !meterOk {
			return nil, decimal.Zero, ierr.NewError("meter not found").
				WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
				WithReportableDetails(map[string]interface{}{
					"meter_id": item.MeterID,
				}).
				Mark(ierr.ErrNotFound)
		}

		for _, matchingCharge := range matchingCharges {
			quantityForCalculation := decimal.NewFromFloat(matchingCharge.Quantity)
			matchingEntitlement, entitlementOk := entitlementsByMeterID[item.MeterID]

			var cachedBucketedUsageResult *events.AggregationResult

			// Handle bucketed meters (max or sum) — uses meter_usage table
			if (meter.IsBucketedMaxMeter() || meter.IsBucketedSumMeter()) && matchingCharge.Price != nil {
				aggType := types.AggregationMax
				groupBy := meter.Aggregation.GroupBy
				if meter.IsBucketedSumMeter() {
					aggType = types.AggregationSum
					groupBy = ""
				}
				meterUsageParams := &events.MeterUsageQueryParams{
					TenantID:            types.GetTenantID(ctx),
					EnvironmentID:       types.GetEnvironmentID(ctx),
					ExternalCustomerIDs: extCustomerIDsForUsage,
					MeterID:             item.MeterID,
					StartTime:           item.GetPeriodStart(periodStart),
					EndTime:             item.GetPeriodEnd(periodEnd),
					AggregationType:     aggType,
					WindowSize:          meter.Aggregation.BucketSize,
					BillingAnchor:       &sub.BillingAnchor,
					GroupByProperty:     groupBy,
					UseFinal:            querySource.UseFinal(),
				}
				usageResult, err := s.MeterUsageRepo.GetUsageForBucketedMeters(ctx, meterUsageParams)
				if err != nil {
					return nil, decimal.Zero, err
				}
				cachedBucketedUsageResult = usageResult

				cost := calculateBucketedMeterCost(ctx, priceService, matchingCharge.Price, usageResult, groupBy != "")
				matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(cost.Amount, matchingCharge.Price.Currency)
				matchingCharge.Quantity = cost.Quantity.InexactFloat64()
				quantityForCalculation = cost.Quantity
			}

			// Apply entitlement adjustments for bucketed meters
			if !matchingCharge.IsOverage && entitlementOk && matchingEntitlement.IsEnabled &&
				(meter.IsBucketedMaxMeter() || meter.IsBucketedSumMeter()) {
				if matchingEntitlement.UsageLimit != nil {
					usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
					adjustedQuantity := decimal.Max(quantityForCalculation.Sub(usageAllowed), decimal.Zero)
					if !adjustedQuantity.Equal(quantityForCalculation) {
						quantityForCalculation = adjustedQuantity
						if matchingCharge.Price != nil {
							adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
							matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
						}
					}
				} else {
					quantityForCalculation = decimal.Zero
					matchingCharge.Amount = 0
				}
			}

			// Apply entitlement adjustments for non-bucketed meters
			if !matchingCharge.IsOverage && entitlementOk && matchingEntitlement.IsEnabled && !meter.IsBucketedMaxMeter() && !meter.IsBucketedSumMeter() {
				if matchingEntitlement.UsageLimit != nil {
					if (matchingEntitlement.UsageResetPeriod) == types.EntitlementUsageResetPeriod(sub.BillingPeriod) {
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
						quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)

					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY {
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:             item.MeterID,
							PriceID:             item.PriceID,
							ExternalCustomerIDs: extCustomerIDsForUsage,
							StartTime:           item.GetPeriodStart(periodStart),
							EndTime:             item.GetPeriodEnd(periodEnd),
							WindowSize:          types.WindowSizeDay,
							Filters:             meter.ToFilterMap(),
							Meter:               meter,
						}
						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						dailyLimit := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						totalBillableQuantity := decimal.Zero
						s.Logger.Debugw("calculating daily usage charges",
							"subscription_id", sub.ID,
							"line_item_id", item.ID,
							"meter_id", item.MeterID,
							"daily_limit", dailyLimit,
							"num_daily_windows", len(usageResult.Results))

						for _, dailyResult := range usageResult.Results {
							dailyUsage := dailyResult.Value
							dailyOverage := decimal.Max(decimal.Zero, dailyUsage.Sub(dailyLimit))
							if dailyOverage.GreaterThan(decimal.Zero) {
								totalBillableQuantity = totalBillableQuantity.Add(dailyOverage)
								s.Logger.Debugw("daily overage calculated",
									"subscription_id", sub.ID,
									"line_item_id", item.ID,
									"date", dailyResult.WindowSize,
									"daily_usage", dailyUsage,
									"daily_limit", dailyLimit,
									"daily_overage", dailyOverage)
							}
						}
						quantityForCalculation = totalBillableQuantity

					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY {
						usageRequest := &dto.GetUsageByMeterRequest{
							MeterID:             item.MeterID,
							PriceID:             item.PriceID,
							ExternalCustomerIDs: extCustomerIDsForUsage,
							StartTime:           item.GetPeriodStart(periodStart),
							EndTime:             item.GetPeriodEnd(periodEnd),
							BillingAnchor:       &sub.BillingAnchor,
							WindowSize:          types.WindowSizeMonth,
							Filters:             meter.ToFilterMap(),
							Meter:               meter,
						}
						usageResult, err := eventService.GetUsageByMeter(ctx, usageRequest)
						if err != nil {
							return nil, decimal.Zero, err
						}

						monthlyLimit := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						totalBillableQuantity := decimal.Zero
						s.Logger.Debugw("calculating monthly usage charges",
							"subscription_id", sub.ID,
							"line_item_id", item.ID,
							"meter_id", item.MeterID,
							"monthly_limit", monthlyLimit,
							"num_monthly_windows", len(usageResult.Results))

						for _, monthlyResult := range usageResult.Results {
							monthlyUsage := monthlyResult.Value
							monthlyOverage := decimal.Max(decimal.Zero, monthlyUsage.Sub(monthlyLimit))
							if monthlyOverage.GreaterThan(decimal.Zero) {
								totalBillableQuantity = totalBillableQuantity.Add(monthlyOverage)
								s.Logger.Debugw("monthly overage calculated",
									"subscription_id", sub.ID,
									"line_item_id", item.ID,
									"month", monthlyResult.WindowSize,
									"monthly_usage", monthlyUsage,
									"monthly_limit", monthlyLimit,
									"monthly_overage", monthlyOverage)
							}
						}
						quantityForCalculation = totalBillableQuantity

					} else if matchingEntitlement.UsageResetPeriod == types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER {
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						quantityForCalculation, err = s.calculateNeverResetUsage(ctx, sub, item, extCustomerIDsForUsage, eventService, periodStart, periodEnd, usageAllowed)
						if err != nil {
							return nil, decimal.Zero, err
						}
					} else {
						usageAllowed := decimal.NewFromFloat(float64(*matchingEntitlement.UsageLimit))
						adjustedQuantity := decimal.NewFromFloat(matchingCharge.Quantity).Sub(usageAllowed)
						quantityForCalculation = decimal.Max(adjustedQuantity, decimal.Zero)
					}

					if matchingCharge.Price != nil {
						adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
						matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
					}
				} else {
					quantityForCalculation = decimal.Zero
					matchingCharge.Amount = 0
				}
			} else if !matchingCharge.IsOverage && !meter.IsBucketedMaxMeter() && !meter.IsBucketedSumMeter() && matchingCharge.Price != nil {
				adjustedAmount := priceService.CalculateCost(ctx, matchingCharge.Price, quantityForCalculation)
				matchingCharge.Amount = priceDomain.FormatAmountToFloat64WithPrecision(adjustedAmount, matchingCharge.Price.Currency)
			}

			lineItemAmount := decimal.NewFromFloat(matchingCharge.Amount)

			var commitmentInfo *types.CommitmentInfo

			// Cumulative path
			if useCumulativePath {
				baseAmount := lineItemAmount
				if matchingCharge.IsOverage && overageFactor.GreaterThan(decimal.Zero) {
					baseAmount = lineItemAmount.Div(overageFactor)
				}
				metadata := types.Metadata{
					"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
				}
				displayName := lo.ToPtr(item.DisplayName)
				if matchingCharge.IsOverage {
					metadata["is_overage"] = "true"
					metadata["overage_factor"] = fmt.Sprintf("%v", matchingCharge.OverageFactor)
					metadata["description"] = fmt.Sprintf("%s (Overage Charge)", item.DisplayName)
					displayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
				}
				if entitlementOk && matchingEntitlement != nil && matchingEntitlement.IsEnabled {
					switch matchingEntitlement.UsageResetPeriod {
					case types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
						metadata["usage_reset_period"] = "daily"
					case types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
						metadata["usage_reset_period"] = "monthly"
					case types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
						metadata["usage_reset_period"] = "never"
					}
				}
				var priceUnitAmount decimal.Decimal
				if item.PriceUnit != nil {
					priceUnit, err := s.PriceUnitRepo.GetByCode(ctx, lo.FromPtr(item.PriceUnit))
					if err == nil {
						converted, convErr := priceunit.ConvertToPriceUnitAmount(ctx, lineItemAmount, priceUnit.ConversionRate, priceUnit.BaseCurrency)
						if convErr == nil {
							priceUnitAmount = converted
						}
					}
				}
				baseChargesForCumulative = append(baseChargesForCumulative, baseChargeInfo{
					item:                   item,
					matchingCharge:         matchingCharge,
					baseAmount:             baseAmount,
					quantityForCalculation: quantityForCalculation,
					priceUnitAmount:        priceUnitAmount,
					displayName:            displayName,
					metadata:               metadata,
				})
				continue
			}

			// Apply line-item commitment
			if item.HasCommitment() {
				if matchingCharge.Price == nil {
					s.Logger.Debugw("skipping commitment application due to missing price",
						"subscription_id", sub.ID,
						"line_item_id", item.ID,
						"price_id", item.PriceID)
				} else {
					commitmentCalc := newCommitmentCalculator(s.Logger, priceService)

					if item.CommitmentWindowed {
						// For window commitment, we need bucketed values — use meter_usage table
						meter, ok := meterMap[item.MeterID]
						if !ok {
							return nil, decimal.Zero, ierr.NewError("meter not found for window commitment").
								WithHint(fmt.Sprintf("Meter with ID %s not found", item.MeterID)).
								WithReportableDetails(map[string]interface{}{
									"meter_id":     item.MeterID,
									"line_item_id": item.ID,
								}).
								Mark(ierr.ErrNotFound)
						}

						linePeriodStart := item.GetPeriodStart(periodStart)
						linePeriodEnd := item.GetPeriodEnd(periodEnd)
						effectiveCommitmentEnd := asOf
						if effectiveCommitmentEnd.Before(linePeriodStart) {
							effectiveCommitmentEnd = linePeriodStart
						}
						if effectiveCommitmentEnd.After(linePeriodEnd) {
							effectiveCommitmentEnd = linePeriodEnd
						}

						commitmentUsageResult := cachedBucketedUsageResult
						if commitmentUsageResult == nil {
							meterUsageParams := &events.MeterUsageQueryParams{
								TenantID:            types.GetTenantID(ctx),
								EnvironmentID:       types.GetEnvironmentID(ctx),
								ExternalCustomerIDs: extCustomerIDsForUsage,
								MeterID:             item.MeterID,
								StartTime:           linePeriodStart,
								EndTime:             effectiveCommitmentEnd,
								AggregationType:     meter.Aggregation.Type,
								WindowSize:          meter.Aggregation.BucketSize,
								BillingAnchor:       &sub.BillingAnchor,
								GroupByProperty:     meter.Aggregation.GroupBy,
								UseFinal:            querySource.UseFinal(),
							}
							fetchedResult, fetchErr := s.MeterUsageRepo.GetUsageForBucketedMeters(ctx, meterUsageParams)
							if fetchErr != nil {
								return nil, decimal.Zero, fetchErr
							}
							commitmentUsageResult = fetchedResult
						}

						bucketedValues := s.fillBucketedValuesForWindowedCommitment(
							item,
							commitmentUsageResult,
							linePeriodStart,
							effectiveCommitmentEnd,
							meter.Aggregation.BucketSize,
							&sub.BillingAnchor,
							meter.Aggregation.Type,
						)

						adjustedAmount, info, err := commitmentCalc.applyWindowCommitmentToLineItem(
							ctx, item, bucketedValues, matchingCharge.Price)
						if err != nil {
							return nil, decimal.Zero, err
						}

						lineItemAmount = adjustedAmount
						matchingCharge.Amount = adjustedAmount.InexactFloat64()
						commitmentInfo = info
					} else {
						adjustedAmount, info, err := commitmentCalc.applyCommitmentToLineItem(
							ctx, item, lineItemAmount, matchingCharge.Price)
						if err != nil {
							return nil, decimal.Zero, err
						}

						lineItemAmount = adjustedAmount
						matchingCharge.Amount = adjustedAmount.InexactFloat64()
						commitmentInfo = info
					}
				}
			}

			totalUsageCost = totalUsageCost.Add(lineItemAmount)

			metadata := types.Metadata{
				"description": fmt.Sprintf("%s (Usage Charge)", item.DisplayName),
			}
			displayName := lo.ToPtr(item.DisplayName)
			if matchingCharge.IsOverage {
				metadata["is_overage"] = "true"
				metadata["overage_factor"] = fmt.Sprintf("%v", matchingCharge.OverageFactor)
				metadata["description"] = fmt.Sprintf("%s (Overage Charge)", item.DisplayName)
				displayName = lo.ToPtr(fmt.Sprintf("%s (Overage)", item.DisplayName))
			}

			if !matchingCharge.IsOverage && entitlementOk && matchingEntitlement != nil && matchingEntitlement.IsEnabled {
				switch matchingEntitlement.UsageResetPeriod {
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_DAILY:
					metadata["usage_reset_period"] = "daily"
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_MONTHLY:
					metadata["usage_reset_period"] = "monthly"
				case types.ENTITLEMENT_USAGE_RESET_PERIOD_NEVER:
					metadata["usage_reset_period"] = "never"
				}
			}

			s.Logger.Debugw("meter usage charges for line item",
				"amount", matchingCharge.Amount,
				"quantity", matchingCharge.Quantity,
				"is_overage", matchingCharge.IsOverage,
				"subscription_id", sub.ID,
				"line_item_id", item.ID,
				"price_id", item.PriceID)

			var priceUnitAmount decimal.Decimal
			if item.PriceUnit != nil {
				priceUnit, err := s.PriceUnitRepo.GetByCode(ctx, lo.FromPtr(item.PriceUnit))
				if err != nil {
					s.Logger.Warnw("failed to get price unit",
						"error", err,
						"price_unit", lo.FromPtr(item.PriceUnit))
					return nil, decimal.Zero, err
				}
				convertedAmount, err := priceunit.ConvertToPriceUnitAmount(ctx, lineItemAmount, priceUnit.ConversionRate, priceUnit.BaseCurrency)
				if err != nil {
					s.Logger.Warnw("failed to convert amount to price unit",
						"error", err,
						"price_unit", lo.FromPtr(item.PriceUnit),
						"amount", lineItemAmount)
					return nil, decimal.Zero, err
				}
				priceUnitAmount = convertedAmount
			}

			usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
				EntityID:         lo.ToPtr(item.EntityID),
				EntityType:       lo.ToPtr(string(item.EntityType)),
				PlanDisplayName:  lo.ToPtr(item.PlanDisplayName),
				PriceType:        lo.ToPtr(string(item.PriceType)),
				PriceID:          lo.ToPtr(item.PriceID),
				MeterID:          lo.ToPtr(item.MeterID),
				MeterDisplayName: lo.ToPtr(item.MeterDisplayName),
				PriceUnit:        item.PriceUnit,
				PriceUnitAmount:  lo.ToPtr(priceUnitAmount),
				DisplayName:      displayName,
				Amount:           lineItemAmount,
				Quantity:         quantityForCalculation,
				PeriodStart:      lo.ToPtr(item.GetPeriodStart(periodStart)),
				PeriodEnd:        lo.ToPtr(item.GetPeriodEnd(periodEnd)),
				Metadata:         metadata,
				CommitmentInfo:   commitmentInfo,
			})
		}
	}

	// Cumulative path: allocate within_commitment, add overage line, add true-up
	if useCumulativePath {
		totalCurrentBase := decimal.Zero
		for _, bc := range baseChargesForCumulative {
			totalCurrentBase = totalCurrentBase.Add(bc.baseAmount)
		}
		isLastPeriod := isLastPeriodOfCommitmentPeriod(periodEnd, commitmentEnd)
		result := applyCumulativeSubscriptionCommitment(
			commitmentAmount, overageFactor, totalCurrentBase, totalPriorBase,
			sub.EnableTrueUp, isLastPeriod, s.Logger,
		)

		for _, bc := range baseChargesForCumulative {
			var allocatedAmount decimal.Decimal
			if totalCurrentBase.GreaterThan(decimal.Zero) {
				allocatedAmount = bc.baseAmount.Div(totalCurrentBase).Mul(result.WithinCommitment)
			}
			roundedAmount := types.RoundToCurrencyPrecision(allocatedAmount, sub.Currency)
			displayQuantity := bc.quantityForCalculation
			if bc.baseAmount.GreaterThan(decimal.Zero) {
				displayQuantity = bc.quantityForCalculation.Mul(allocatedAmount).Div(bc.baseAmount)
			}
			displayQuantity = types.RoundToCurrencyPrecision(displayQuantity, sub.Currency)
			usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
				EntityID:         lo.ToPtr(bc.item.EntityID),
				EntityType:       lo.ToPtr(string(bc.item.EntityType)),
				PlanDisplayName:  lo.ToPtr(bc.item.PlanDisplayName),
				PriceType:        lo.ToPtr(string(bc.item.PriceType)),
				PriceID:          lo.ToPtr(bc.item.PriceID),
				MeterID:          lo.ToPtr(bc.item.MeterID),
				MeterDisplayName: lo.ToPtr(bc.item.MeterDisplayName),
				PriceUnit:        bc.item.PriceUnit,
				PriceUnitAmount:  lo.ToPtr(bc.priceUnitAmount),
				DisplayName:      bc.displayName,
				Amount:           roundedAmount,
				Quantity:         displayQuantity,
				PeriodStart:      lo.ToPtr(bc.item.GetPeriodStart(periodStart)),
				PeriodEnd:        lo.ToPtr(bc.item.GetPeriodEnd(periodEnd)),
				Metadata:         bc.metadata,
			})
			totalUsageCost = totalUsageCost.Add(roundedAmount)
		}

		if result.OverageAmount.GreaterThan(decimal.Zero) {
			planDisplayName := ""
			for _, item := range sub.LineItems {
				if item.PlanDisplayName != "" {
					planDisplayName = item.PlanDisplayName
					break
				}
			}
			roundedOverage := types.RoundToCurrencyPrecision(result.OverageAmount, sub.Currency)
			overageQuantity := types.RoundToCurrencyPrecision(result.OverageBase, sub.Currency)
			usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
				EntityID:        lo.ToPtr(sub.PlanID),
				EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
				PlanDisplayName: lo.ToPtr(planDisplayName),
				PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				DisplayName:     lo.ToPtr(fmt.Sprintf("%s Overage", planDisplayName)),
				Amount:          roundedOverage,
				Quantity:        overageQuantity,
				PeriodStart:     &periodStart,
				PeriodEnd:       &periodEnd,
				PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
				Metadata: types.Metadata{
					"is_overage":     "true",
					"overage_factor": overageFactor.String(),
					"description":    "Overage charge (cumulative commitment)",
				},
			})
			totalUsageCost = totalUsageCost.Add(roundedOverage)
		}

		if result.TrueUpAmount.GreaterThan(decimal.Zero) {
			planDisplayName := ""
			for _, item := range sub.LineItems {
				if item.PlanDisplayName != "" {
					planDisplayName = item.PlanDisplayName
					break
				}
			}
			roundedTrueUp := types.RoundToCurrencyPrecision(result.TrueUpAmount, sub.Currency)
			usageCharges = append(usageCharges, dto.CreateInvoiceLineItemRequest{
				EntityID:        lo.ToPtr(sub.PlanID),
				EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
				PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
				PlanDisplayName: lo.ToPtr(planDisplayName),
				DisplayName:     lo.ToPtr(fmt.Sprintf("%s True Up", planDisplayName)),
				Amount:          roundedTrueUp,
				Quantity:        decimal.NewFromInt(1),
				PeriodStart:     &periodStart,
				PeriodEnd:       &periodEnd,
				PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
				Metadata: types.Metadata{
					"is_commitment_trueup": "true",
					"description":          "Remaining commitment amount for commitment period",
					"commitment_amount":    commitmentAmount.String(),
					"commitment_utilized":  result.CommitmentUtilized.String(),
				},
			})
			totalUsageCost = totalUsageCost.Add(roundedTrueUp)
		}

		return usageCharges, totalUsageCost, nil
	}

	// Non-cumulative commitment true-up
	hasCommitment := commitmentAmount.GreaterThan(decimal.Zero) && overageFactor.GreaterThan(decimal.NewFromInt(1))
	if hasCommitment {
		if !usage.HasOverage && sub.EnableTrueUp {
			remainingCommitment := s.calculateRemainingCommitment(usage, commitmentAmount)
			if remainingCommitment.GreaterThan(decimal.Zero) {
				planDisplayName := ""
				for _, item := range sub.LineItems {
					if item.PlanDisplayName != "" {
						planDisplayName = item.PlanDisplayName
						break
					}
				}
				precision := types.GetCurrencyPrecision(sub.Currency)
				roundedRemainingCommitment := remainingCommitment.Round(precision)
				commitmentUtilized := commitmentAmount.Sub(roundedRemainingCommitment)
				trueUpLineItem := dto.CreateInvoiceLineItemRequest{
					EntityID:        lo.ToPtr(sub.PlanID),
					EntityType:      lo.ToPtr(string(types.SubscriptionLineItemEntityTypePlan)),
					PriceType:       lo.ToPtr(string(types.PRICE_TYPE_FIXED)),
					PlanDisplayName: lo.ToPtr(planDisplayName),
					DisplayName:     lo.ToPtr(fmt.Sprintf("%s True Up", planDisplayName)),
					Amount:          roundedRemainingCommitment,
					Quantity:        decimal.NewFromInt(1),
					PeriodStart:     &periodStart,
					PeriodEnd:       &periodEnd,
					PriceID:         lo.ToPtr(types.GenerateUUIDWithPrefix(types.UUID_PREFIX_PRICE)),
					Metadata: types.Metadata{
						"is_commitment_trueup": "true",
						"description":          "Remaining commitment amount for billing period",
						"commitment_amount":    commitmentAmount.String(),
						"commitment_utilized":  commitmentUtilized.String(),
					},
				}
				usageCharges = append(usageCharges, trueUpLineItem)
				totalUsageCost = totalUsageCost.Add(roundedRemainingCommitment)
			}
		}
	}

	return usageCharges, totalUsageCost, nil
}

// calculateAllMeterUsageCharges is the meter_usage counterpart of calculateAllFeatureUsageCharges.
// It calculates fixed charges + usage charges, routing bucketed/windowed queries through
// MeterUsageRepo instead of FeatureUsageRepo.
func (s *billingService) calculateAllMeterUsageCharges(
	ctx context.Context,
	sub *subscription.Subscription,
	usage *dto.GetUsageBySubscriptionResponse,
	periodStart,
	periodEnd time.Time,
) (*BillingCalculationResult, error) {
	fixedCharges, fixedTotal, err := s.CalculateFixedCharges(ctx, sub, periodStart, periodEnd)
	if err != nil {
		return nil, err
	}

	usageCharges, usageTotal, err := s.CalculateMeterUsageCharges(ctx, sub, usage, periodStart, periodEnd, &CalculateFeatureUsageChargesOpts{Source: types.UsageSourceInvoiceCreation})
	if err != nil {
		return nil, err
	}

	return &BillingCalculationResult{
		FixedCharges: fixedCharges,
		UsageCharges: usageCharges,
		TotalAmount:  fixedTotal.Add(usageTotal),
		Currency:     sub.Currency,
	}, nil
}
