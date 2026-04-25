package cron

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"go.temporal.io/sdk/activity"
)

// WalletCreditExpiryActivities wraps wallet credit expiry logic.
type WalletCreditExpiryActivities struct {
	walletService      service.WalletService
	tenantService      service.TenantService
	environmentService service.EnvironmentService
	logger             *logger.Logger
}

func NewWalletCreditExpiryActivities(
	walletService service.WalletService,
	tenantService service.TenantService,
	environmentService service.EnvironmentService,
	log *logger.Logger,
) *WalletCreditExpiryActivities {
	return &WalletCreditExpiryActivities{
		walletService:      walletService,
		tenantService:      tenantService,
		environmentService: environmentService,
		logger:             log,
	}
}

const envPageSize = 100
const txPageSize = 500

// ExpireCreditsActivity finds and expires credits that have passed their expiry date across all tenants.
func (a *WalletCreditExpiryActivities) ExpireCreditsActivity(ctx context.Context) (*cronModels.WalletCreditExpiryWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting wallet credit expiry activity")

	tenants, err := a.tenantService.GetAllTenants(ctx)
	if err != nil {
		return nil, err
	}

	baseFilter := types.NewWalletTransactionFilter()
	baseFilter.Type = lo.ToPtr(types.TransactionTypeCredit)
	baseFilter.TransactionStatus = lo.ToPtr(types.TransactionStatusCompleted)
	baseFilter.ExpiryDateBefore = lo.ToPtr(time.Now().UTC().Add(-6 * time.Hour))
	baseFilter.CreditsAvailableGT = lo.ToPtr(decimal.Zero)
	baseFilter.QueryFilter = types.NewDefaultQueryFilter()
	baseFilter.QueryFilter.Limit = lo.ToPtr(txPageSize)

	result := &cronModels.WalletCreditExpiryWorkflowResult{}

	for _, tenant := range tenants {
		tenantCtx := context.WithValue(ctx, types.CtxTenantID, tenant.ID)

		// Paginate over environments to avoid missing tenants with more than envPageSize envs.
		envOffset := 0
		for {
			envFilter := types.GetDefaultFilter()
			envFilter.Limit = envPageSize
			envFilter.Offset = envOffset

			environments, err := a.environmentService.GetEnvironments(tenantCtx, envFilter)
			if err != nil {
				a.logger.Errorw("failed to get environments for tenant", "tenant_id", tenant.ID, "error", err)
				break
			}

			for _, env := range environments.Environments {
				envCtx := context.WithValue(tenantCtx, types.CtxEnvironmentID, env.ID)

				// Paginate over expired transactions to avoid large memory spikes.
				txOffset := 0
				processedInEnv := 0
				for {
					filter := *baseFilter
					qf := *baseFilter.QueryFilter
					qf.Offset = lo.ToPtr(txOffset)
					filter.QueryFilter = &qf

					transactions, err := a.walletService.ListWalletTransactionsByFilter(envCtx, &filter)
					if err != nil {
						a.logger.Errorw("failed to list expired credits",
							"tenant_id", tenant.ID, "environment_id", env.ID, "error", err)
						break
					}

					for i, tx := range transactions.Items {
						if (processedInEnv+i)%100 == 0 {
							activity.RecordHeartbeat(ctx, "processed tenant "+tenant.ID+" env "+env.ID)
						}
						result.Total++
						txCtx := context.WithValue(envCtx, types.CtxUserID, tx.CreatedBy)

						expireResult, err := a.walletService.ExpireCredits(txCtx, tx.ID)
						if err != nil {
							a.logger.Errorw("failed to expire credits",
								"transaction_id", tx.ID, "error", err)
							result.Failed++
							continue
						}

						if expireResult.Expired {
							result.Succeeded++
							continue
						}

						switch expireResult.SkipReason {
						case types.CreditExpirySkipReasonActiveSubscription:
							result.SkippedDueToActiveSubscription++
						case types.CreditExpirySkipReasonActiveInvoice:
							result.SkippedDueToActiveInvoice++
						}
					}

					processedInEnv += len(transactions.Items)
					if len(transactions.Items) < txPageSize {
						break
					}
					txOffset += txPageSize
				}

				activity.RecordHeartbeat(ctx, "processed tenant "+tenant.ID+" env "+env.ID)
			}

			if len(environments.Environments) < envPageSize {
				break
			}
			envOffset += envPageSize
		}
	}

	log.Info("Completed wallet credit expiry activity",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return result, nil
}
