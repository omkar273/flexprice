package activities

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const ActivityPrefix = "EnvironmentActivities"

// EnvironmentActivities contains all environment-clone-related activities.
type EnvironmentActivities struct {
	featureService service.FeatureService
	planService    service.PlanService
}

// NewEnvironmentActivities creates a new EnvironmentActivities instance.
func NewEnvironmentActivities(featureService service.FeatureService, planService service.PlanService) *EnvironmentActivities {
	return &EnvironmentActivities{
		featureService: featureService,
		planService:    planService,
	}
}

// CloneEnvironmentFeaturesInput represents the input for cloning features across environments.
type CloneEnvironmentFeaturesInput struct {
	SourceEnvironmentID string `json:"source_environment_id"`
	TargetEnvironmentID string `json:"target_environment_id"`
	TenantID            string `json:"tenant_id"`
	UserID              string `json:"user_id"`
}

// CloneEnvironmentFeaturesOutput represents the result of cloning features.
type CloneEnvironmentFeaturesOutput struct {
	FeaturesCloned int      `json:"features_cloned"`
	FeatureIDs     []string `json:"feature_ids"`
	Errors         []string `json:"errors,omitempty"`
}

// CloneEnvironmentFeatures fetches all published features from the source environment
// and clones each into the target environment.
func (a *EnvironmentActivities) CloneEnvironmentFeatures(ctx context.Context, input CloneEnvironmentFeaturesInput) (*CloneEnvironmentFeaturesOutput, error) {
	log := logger.GetLogger()

	// Set context to source environment for fetching
	sourceCtx := types.SetTenantID(ctx, input.TenantID)
	sourceCtx = types.SetEnvironmentID(sourceCtx, input.SourceEnvironmentID)
	sourceCtx = types.SetUserID(sourceCtx, input.UserID)

	// Fetch all published features from source environment
	filter := types.NewNoLimitFeatureFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	featuresResp, err := a.featureService.GetFeatures(sourceCtx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list features from source environment: %w", err)
	}

	output := &CloneEnvironmentFeaturesOutput{}
	for _, f := range featuresResp.Items {
		cloneReq := dto.CloneFeatureRequest{
			Name:                f.Name,
			LookupKey:           f.LookupKey,
			TargetEnvironmentID: input.TargetEnvironmentID,
		}

		cloned, err := a.featureService.CloneFeature(sourceCtx, f.ID, cloneReq)
		if err != nil {
			errMsg := fmt.Sprintf("feature %s (%s): %v", f.ID, f.LookupKey, err)
			log.Warnw("env_clone_feature_skipped", "error", errMsg)
			output.Errors = append(output.Errors, errMsg)
			continue
		}
		output.FeaturesCloned++
		output.FeatureIDs = append(output.FeatureIDs, cloned.ID)
	}

	log.Infow("env_clone_features_completed",
		"source_env", input.SourceEnvironmentID,
		"target_env", input.TargetEnvironmentID,
		"features_cloned", output.FeaturesCloned,
		"errors", len(output.Errors),
	)

	return output, nil
}

// CloneEnvironmentPlansInput represents the input for cloning plans across environments.
type CloneEnvironmentPlansInput struct {
	SourceEnvironmentID string `json:"source_environment_id"`
	TargetEnvironmentID string `json:"target_environment_id"`
	TenantID            string `json:"tenant_id"`
	UserID              string `json:"user_id"`
}

// CloneEnvironmentPlansOutput represents the result of cloning plans.
type CloneEnvironmentPlansOutput struct {
	PlansCloned int      `json:"plans_cloned"`
	PlanIDs     []string `json:"plan_ids"`
	Errors      []string `json:"errors,omitempty"`
}

// CloneEnvironmentPlans fetches all published plans from the source environment
// and clones each into the target environment.
func (a *EnvironmentActivities) CloneEnvironmentPlans(ctx context.Context, input CloneEnvironmentPlansInput) (*CloneEnvironmentPlansOutput, error) {
	log := logger.GetLogger()

	// Set context to source environment for fetching
	sourceCtx := types.SetTenantID(ctx, input.TenantID)
	sourceCtx = types.SetEnvironmentID(sourceCtx, input.SourceEnvironmentID)
	sourceCtx = types.SetUserID(sourceCtx, input.UserID)

	// Fetch all published plans from source environment
	filter := types.NewNoLimitPlanFilter()
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)
	plansResp, err := a.planService.GetPlans(sourceCtx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list plans from source environment: %w", err)
	}

	output := &CloneEnvironmentPlansOutput{}
	for _, p := range plansResp.Items {
		cloneReq := dto.ClonePlanRequest{
			Name:                p.Name,
			LookupKey:           p.LookupKey,
			TargetEnvironmentID: input.TargetEnvironmentID,
		}

		cloned, err := a.planService.ClonePlan(sourceCtx, p.ID, cloneReq)
		if err != nil {
			errMsg := fmt.Sprintf("plan %s (%s): %v", p.ID, p.LookupKey, err)
			log.Warnw("env_clone_plan_skipped", "error", errMsg)
			output.Errors = append(output.Errors, errMsg)
			continue
		}
		output.PlansCloned++
		output.PlanIDs = append(output.PlanIDs, cloned.ID)
	}

	log.Infow("env_clone_plans_completed",
		"source_env", input.SourceEnvironmentID,
		"target_env", input.TargetEnvironmentID,
		"plans_cloned", output.PlansCloned,
		"errors", len(output.Errors),
	)

	return output, nil
}
