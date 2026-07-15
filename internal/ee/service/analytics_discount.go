package service

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
)

// analyticsCoupon is the applicator's pure view of a coupon association: the coupon plus the
// active window from its association. It intentionally has no coupon_association dependency so
// the applicator stays trivially unit-testable. Callers are responsible for validity/cadence/type
// filtering: the applicator does NOT call IsValid and assumes only applicable percentage coupons
// are passed in.
type analyticsCoupon struct {
	Coupon    *coupon.Coupon
	StartDate time.Time
	EndDate   *time.Time
}

// activeAt reports whether the association window [StartDate, EndDate) covers t. EndDate nil =
// open-ended. Start inclusive, end exclusive — matches the codebase-wide billing-period
// convention (start inclusive, end exclusive) — intended to stay in sync with the repo's
// CouponAssociation ActiveOnly filter (internal/repository/ent/coupon_association.go), which is
// being migrated to the same convention in a separate task. A degenerate window (EndDate ==
// StartDate, i.e. voided) is naturally never active for any t under this check.
func (ac analyticsCoupon) activeAt(t time.Time) bool {
	if t.Before(ac.StartDate) {
		return false
	}
	if ac.EndDate != nil && !t.Before(*ac.EndDate) {
		return false
	}
	return true
}

// activeOverlaps reports whether the association window [StartDate, EndDate) overlaps the
// half-open query range [start, end). EndDate nil = open-ended. This is intended to match the
// repo's CouponAssociation ActiveOnly filter once it's migrated to the same half-open convention
// (a separate, still-pending task) — as of this commit that filter still uses inclusive
// StartDateLTE/EndDateGTE bounds. Callers of this method always pass genuine, non-degenerate
// RangeStart/RangeEnd (never a collapsed point-in-time query), so no point-membership branch is
// needed here (contrast with the repository-layer filter, which does need one).
// A degenerate window (EndDate == StartDate, i.e. voided) must be excluded explicitly: the
// half-open overlap check alone is not sufficient for a zero-width interval (it would
// incorrectly report overlap for any query range that contains the voided point).
func (ac analyticsCoupon) activeOverlaps(start, end time.Time) bool {
	if ac.EndDate != nil && ac.EndDate.Equal(ac.StartDate) {
		return false
	}
	if ac.EndDate != nil && !ac.EndDate.After(start) {
		return false
	}
	return ac.StartDate.Before(end)
}

type discountInput struct {
	Currency    string
	SubTotal    decimal.Decimal
	Points      []events.UsageAnalyticPoint // empty => non-windowed
	LineCoupons []*analyticsCoupon          // applied first
	SubCoupons  []*analyticsCoupon          // applied after, compounding
	RangeStart  time.Time                   // used when Points is empty
	RangeEnd    time.Time                   // used when Points is empty
}

type discountOutput struct {
	TotalDiscount  decimal.Decimal
	SubTotal       decimal.Decimal
	PointDiscounts []events.UsageAnalyticPoint // aligned to input Points; nil when non-windowed
}

// compound applies the active line coupons then sub coupons onto base, using the canonical
// coupon.ApplyDiscount math (which rounds to currency precision and caps at >= 0), and returns
// the total discount (base - remaining).
func compound(base decimal.Decimal, currency string, line, sub []*analyticsCoupon, active func(*analyticsCoupon) bool) decimal.Decimal {
	remaining := base
	apply := func(cs []*analyticsCoupon) {
		for _, c := range cs {
			if remaining.LessThanOrEqual(decimal.Zero) || !active(c) {
				continue
			}
			remaining = c.Coupon.ApplyDiscount(remaining, currency).FinalPrice
		}
	}
	apply(line)
	apply(sub)
	return base.Sub(remaining)
}

// ApplyAnalyticsDiscounts computes per-window (or all-or-nothing when non-windowed) percentage
// discounts for one analytic item. Pure: no DB, no service deps.
func ApplyAnalyticsDiscounts(in *discountInput) *discountOutput {
	if len(in.LineCoupons) == 0 && len(in.SubCoupons) == 0 {
		return &discountOutput{
			TotalDiscount:  decimal.Zero,
			SubTotal:       in.SubTotal,
			PointDiscounts: in.Points,
		}
	}

	// Non-windowed input.
	// so in case points are empty, we apply the discounts to subscription level only internally we still apply coupons for line items and roll up to subscription level.
	if len(in.Points) == 0 {

		discount := compound(in.SubTotal, in.Currency, in.LineCoupons, in.SubCoupons,
			func(c *analyticsCoupon) bool { return c.activeOverlaps(in.RangeStart, in.RangeEnd) })
		return &discountOutput{TotalDiscount: discount, SubTotal: in.SubTotal.Sub(discount)}
	}

	totalDiscount := decimal.Zero
	for i := range in.Points {
		discount := compound(in.Points[i].Cost, in.Currency, in.LineCoupons, in.SubCoupons,
			func(c *analyticsCoupon) bool { return c.activeAt(in.Points[i].Timestamp) })
		totalDiscount = totalDiscount.Add(discount)
		in.Points[i].Discount = discount
	}
	return &discountOutput{
		TotalDiscount:  totalDiscount,
		SubTotal:       in.SubTotal.Sub(totalDiscount),
		PointDiscounts: in.Points,
	}
}
