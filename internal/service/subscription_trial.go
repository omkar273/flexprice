package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(prices []*dto.PriceResponse) (int, error) {
	var first *int
	for _, pr := range prices {
		p := pr.Price
		if p.BillingCadence != types.BILLING_CADENCE_RECURRING || p.Type != types.PRICE_TYPE_FIXED {
			continue
		}
		d := p.TrialPeriodDays
		if first == nil {
			v := d
			first = &v
			continue
		}
		if *first != d {
			return 0, ierr.NewError("all recurring fixed plan prices must have the same trial_period_days").
				WithHint("Align trial_period_days on plan prices or override with subscription trial_period_days").
				WithReportableDetails(map[string]any{
					"expected": *first,
					"got":      d,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	if first == nil {
		return 0, nil
	}
	return *first, nil
}

func resolveEffectiveTrialPeriodDays(req *dto.CreateSubscriptionRequest, planPrices []*dto.PriceResponse) (int, error) {
	if req.TrialPeriodDays != nil {
		return *req.TrialPeriodDays, nil
	}
	return uniformTrialPeriodDaysAmongRecurringFixedPlanPrices(planPrices)
}

// trialDaysFromBounds returns whole days between trial start and end (floor of duration / 24h).
func trialDaysFromBounds(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	return int(end.Sub(start) / (24 * time.Hour))
}

// applyTrialWindowToSubscription sets TrialStart/TrialEnd from internal bounds (e.g. Stripe) or from
// resolved trial_period_days vs subscription StartDate. Returns effective trial days for recurring fixed line items.
func applyTrialWindowToSubscription(req *dto.CreateSubscriptionRequest, sub *subscription.Subscription, planPrices []*dto.PriceResponse) (effectiveTrialDays int, err error) {
	if req.TrialStart != nil && req.TrialEnd != nil {
		sub.TrialStart = req.TrialStart
		sub.TrialEnd = req.TrialEnd
		return trialDaysFromBounds(*req.TrialStart, *req.TrialEnd), nil
	}

	days, err := resolveEffectiveTrialPeriodDays(req, planPrices)
	if err != nil {
		return 0, err
	}
	if days <= 0 {
		sub.TrialStart, sub.TrialEnd = nil, nil
		return 0, nil
	}

	ts := sub.StartDate
	te := ts.AddDate(0, 0, days)
	sub.TrialStart = &ts
	sub.TrialEnd = &te
	return days, nil
}
