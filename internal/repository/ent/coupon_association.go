package ent

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/couponassociation"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainCouponAssociation "github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// CouponAssociationQuery type alias for better readability
type CouponAssociationQuery = *ent.CouponAssociationQuery

// CouponAssociationQueryOptions implements BaseQueryOptions for coupon association queries
type CouponAssociationQueryOptions struct{}

func (o CouponAssociationQueryOptions) ApplyTenantFilter(ctx context.Context, query CouponAssociationQuery) CouponAssociationQuery {
	return query.Where(couponassociation.TenantID(types.GetTenantID(ctx)))
}

func (o CouponAssociationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CouponAssociationQuery) CouponAssociationQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(couponassociation.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o CouponAssociationQueryOptions) ApplyStatusFilter(query CouponAssociationQuery, status string) CouponAssociationQuery {
	if status == "" {
		return query.Where(couponassociation.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(couponassociation.Status(status))
}

func (o CouponAssociationQueryOptions) ApplySortFilter(query CouponAssociationQuery, field string, order string) CouponAssociationQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o CouponAssociationQueryOptions) ApplyPaginationFilter(query CouponAssociationQuery, limit int, offset int) CouponAssociationQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the ent field name for coupon_association; delegates to ent's ValidColumn so new schema fields are supported automatically.
func (o CouponAssociationQueryOptions) GetFieldName(field string) string {
	if couponassociation.ValidColumn(field) {
		return field
	}
	return ""
}

// GetFieldResolver returns the ent field name for a given field with error handling
func (o CouponAssociationQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in coupon association query", field).
			WithHintf("Unknown field name '%s' in coupon association query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

// applyActiveOnlyFilter applies a filter to return only coupon associations active during the
// specified period, using the codebase-wide half-open [start, end) convention (see
// internal/ee/service/billing.go's period-advance logic for the same convention). Kept in sync
// with internal/testutil/inmemory_coupon_association_store.go's couponAssociationFilterFn and
// internal/ee/service/analytics_discount.go's analyticsCoupon.activeOverlaps.
//
// When ActiveOnly is true, the association's own window [start_date, end_date) must contain or
// overlap the query. Two distinct query modes share this function:
//   - Genuine range (both activePeriodStart and activePeriodEnd provided, non-equal): standard
//     half-open overlap — start_date < period_end AND (end_date IS NULL OR end_date > period_start).
//   - Point-in-time (at most one of activePeriodStart/activePeriodEnd provided, or neither —
//     defaults to now()): both collapse to a single instant T, and the correct check is POINT
//     MEMBERSHIP, not range overlap — start_date <= T AND (end_date IS NULL OR end_date > T).
//     Note the start comparison is non-strict here (<=), unlike the strict (<) comparison for a
//     genuine range: an association starting exactly at the query instant must count as active
//     at that instant, but an association starting exactly when a genuine range ends must not
//     (it hasn't started yet as of any point actually inside that range).
//
// A degenerate/voided association (start_date == end_date) is never active under either mode —
// this can't be left to the checks above: for a zero-width window, both the range-overlap and
// point-membership formulas can still evaluate true when the void point falls strictly inside
// the query period, since neither formula on its own encodes "this window contains nothing."
func applyActiveOnlyFilter(query CouponAssociationQuery, activePeriodStart, activePeriodEnd *time.Time) CouponAssociationQuery {
	var periodStart, periodEnd time.Time
	pointInTime := activePeriodStart == nil || activePeriodEnd == nil

	if activePeriodStart != nil && activePeriodEnd != nil {
		// Use provided period
		periodStart = activePeriodStart.UTC()
		periodEnd = activePeriodEnd.UTC()
	} else if activePeriodStart != nil {
		// Only ActivePeriodStart provided, use it for both checks
		periodStart = activePeriodStart.UTC()
		periodEnd = periodStart
	} else if activePeriodEnd != nil {
		// Only ActivePeriodEnd provided, use it for both checks
		periodEnd = activePeriodEnd.UTC()
		periodStart = periodEnd
	} else {
		// No period provided, use current time
		now := time.Now().UTC()
		periodStart = now
		periodEnd = now
	}

	startCondition := couponassociation.StartDateLT(periodEnd)
	if pointInTime {
		startCondition = couponassociation.StartDateLTE(periodEnd)
	}

	return query.Where(
		couponassociation.And(
			startCondition,
			couponassociation.Or(
				couponassociation.EndDateIsNil(),
				couponassociation.And(
					couponassociation.EndDateGT(periodStart),
					nonVoidedAssociation(),
				),
			),
		),
	)
}

// nonVoidedAssociation excludes rows where start_date == end_date — a degenerate, empty [t, t)
// window that must never be considered active for any period. Only meaningful when end_date is
// set (the caller only applies it inside the EndDateGT branch above), since a NULL end_date can
// never equal start_date.
func nonVoidedAssociation() predicate.CouponAssociation {
	return predicate.CouponAssociation(func(s *sql.Selector) {
		s.Where(sql.ColumnsNEQ(s.C(couponassociation.FieldStartDate), s.C(couponassociation.FieldEndDate)))
	})
}

// applyEntityQueryOptions applies entity-specific filters from CouponAssociationFilter
func (o CouponAssociationQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CouponAssociationFilter, query CouponAssociationQuery) (CouponAssociationQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply subscription ID filters
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(couponassociation.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	// Apply coupon ID filters
	if len(f.CouponIDs) > 0 {
		query = query.Where(couponassociation.CouponIDIn(f.CouponIDs...))
	}

	// Apply subscription line item ID filters
	if len(f.SubscriptionLineItemIDs) > 0 {
		query = query.Where(couponassociation.SubscriptionLineItemIDIn(f.SubscriptionLineItemIDs...))
	}

	// Apply subscription phase ID filters
	if len(f.SubscriptionPhaseIDs) > 0 {
		query = query.Where(couponassociation.SubscriptionPhaseIDIn(f.SubscriptionPhaseIDs...))
	}

	// Apply active filter based on start_date and end_date
	if f.ActiveOnly {
		query = applyActiveOnlyFilter(query, f.PeriodStart, f.PeriodEnd)
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[CouponAssociationQuery, predicate.CouponAssociation](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.CouponAssociation { return predicate.CouponAssociation(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[CouponAssociationQuery, couponassociation.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) couponassociation.OrderOption { return couponassociation.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Always load coupon relation since domain model includes it
	query = query.WithCoupon()

	return query, nil
}

type couponAssociationRepository struct {
	client     postgres.IClient
	log        *logger.Logger
	queryOpts  CouponAssociationQueryOptions
	redisCache cache.RedisCache
}

func NewCouponAssociationRepository(client postgres.IClient, log *logger.Logger, redisCache cache.RedisCache) domainCouponAssociation.Repository {
	return &couponAssociationRepository{
		client:     client,
		log:        log,
		queryOpts:  CouponAssociationQueryOptions{},
		redisCache: redisCache,
	}
}

func (r *couponAssociationRepository) Create(ctx context.Context, ca *domainCouponAssociation.CouponAssociation) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "create", map[string]interface{}{
		"association_id": ca.ID,
		"coupon_id":      ca.CouponID,
		"tenant_id":      ca.TenantID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if ca.EnvironmentID == "" {
		ca.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	client := r.client.Writer(ctx)

	create := client.CouponAssociation.Create().
		SetID(ca.ID).
		SetTenantID(ca.TenantID).
		SetCouponID(ca.CouponID).
		SetSubscriptionID(ca.SubscriptionID).
		SetStatus(string(ca.Status)).
		SetCreatedAt(ca.CreatedAt).
		SetUpdatedAt(ca.UpdatedAt).
		SetCreatedBy(ca.CreatedBy).
		SetUpdatedBy(ca.UpdatedBy).
		SetEnvironmentID(ca.EnvironmentID)

	// Handle optional subscription line item ID
	if ca.SubscriptionLineItemID != nil {
		create = create.SetSubscriptionLineItemID(*ca.SubscriptionLineItemID)
	}

	// Handle optional subscription phase ID
	if ca.SubscriptionPhaseID != nil {
		create = create.SetSubscriptionPhaseID(*ca.SubscriptionPhaseID)
	}

	// Handle optional start date (nullable in schema)
	if !ca.StartDate.IsZero() {
		create = create.SetStartDate(ca.StartDate)
	}

	// Handle optional end date
	if ca.EndDate != nil {
		create = create.SetEndDate(*ca.EndDate)
	}

	// Handle optional metadata
	if ca.Metadata != nil {
		create = create.SetMetadata(ca.Metadata)
	}

	result, err := create.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A coupon association with these attributes already exists").
				WithReportableDetails(map[string]interface{}{
					"association_id": ca.ID,
					"coupon_id":      ca.CouponID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create coupon association").
			WithReportableDetails(map[string]interface{}{
				"association_id": ca.ID,
				"coupon_id":      ca.CouponID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*ca = *domainCouponAssociation.FromEnt(result)
	return nil
}

func (r *couponAssociationRepository) Get(ctx context.Context, id string) (*domainCouponAssociation.CouponAssociation, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "get", map[string]interface{}{
		"association_id": id,
		"tenant_id":      types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedAssociation := r.GetCache(ctx, id); cachedAssociation != nil {
		return cachedAssociation, nil
	}

	client := r.client.Reader(ctx)

	result, err := client.CouponAssociation.Query().
		Where(
			couponassociation.ID(id),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		WithCoupon().
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Coupon association not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon association").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	associationData := domainCouponAssociation.FromEnt(result)
	r.SetCache(ctx, associationData)
	return associationData, nil
}

func (r *couponAssociationRepository) Update(ctx context.Context, ca *domainCouponAssociation.CouponAssociation) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "update", map[string]interface{}{
		"association_id": ca.ID,
		"tenant_id":      ca.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	update := client.CouponAssociation.UpdateOneID(ca.ID).
		Where(
			couponassociation.TenantID(ca.TenantID),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	// Handle optional metadata
	if ca.Metadata != nil {
		update = update.SetMetadata(ca.Metadata)
	}

	// Handle optional end date (can be updated)
	if ca.EndDate != nil {
		update = update.SetEndDate(*ca.EndDate)
	}

	_, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Coupon association not found").
				WithReportableDetails(map[string]interface{}{"id": ca.ID}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update coupon association").
			WithReportableDetails(map[string]interface{}{"id": ca.ID}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, ca.ID)
	return nil
}

func (r *couponAssociationRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "delete", map[string]interface{}{
		"association_id": id,
		"tenant_id":      types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	_, err := client.CouponAssociation.Delete().
		Where(
			couponassociation.ID(id),
			couponassociation.TenantID(types.GetTenantID(ctx)),
			couponassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Coupon association not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete coupon association").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, id)
	return nil
}

func (r *couponAssociationRepository) List(ctx context.Context, filter *types.CouponAssociationFilter) ([]*domainCouponAssociation.CouponAssociation, error) {
	if filter == nil {
		filter = &types.CouponAssociationFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	query := client.CouponAssociation.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	results, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupon associations").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCouponAssociation.FromEntList(results), nil
}

func (r *couponAssociationRepository) Count(ctx context.Context, filter *types.CouponAssociationFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon_association", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	query := client.CouponAssociation.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count coupon associations").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *couponAssociationRepository) SetCache(ctx context.Context, association *domainCouponAssociation.CouponAssociation) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "coupon_association", "set", map[string]interface{}{
		"association_id": association.ID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixCouponAssociation, association.ID)
	r.redisCache.Set(ctx, cacheKey, association, cache.ExpiryDefaultRedis)
}

func (r *couponAssociationRepository) GetCache(ctx context.Context, id string) *domainCouponAssociation.CouponAssociation {
	span, ctx := cache.StartRedisCacheSpan(ctx, "coupon_association", "get", map[string]interface{}{
		"association_id": id,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixCouponAssociation, id)
	value, found := r.redisCache.Get(ctx, cacheKey)
	if !found {
		return nil
	}
	ca, ok := cache.UnmarshalCacheValue[domainCouponAssociation.CouponAssociation](value)
	if !ok {
		return nil
	}
	return ca
}

func (r *couponAssociationRepository) DeleteCache(ctx context.Context, associationID string) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "coupon_association", "delete", map[string]interface{}{
		"association_id": associationID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixCouponAssociation, associationID)
	r.redisCache.Delete(ctx, cacheKey)
}
