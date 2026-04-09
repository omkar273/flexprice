package workflows

import (
	"time"

	environmentActivities "github.com/flexprice/flexprice/internal/temporal/activities/environment"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowEnvironmentClone         = "EnvironmentCloneWorkflow"
	ActivityCloneEnvironmentFeatures = "CloneEnvironmentFeatures"
	ActivityCloneEnvironmentPlans    = "CloneEnvironmentPlans"
)

// EnvironmentCloneResult is the output of the environment clone workflow.
type EnvironmentCloneResult struct {
	FeaturesCloned int      `json:"features_cloned"`
	PlansCloned    int      `json:"plans_cloned"`
	FeatureIDs     []string `json:"feature_ids"`
	PlanIDs        []string `json:"plan_ids"`
	Errors         []string `json:"errors,omitempty"`
}

// EnvironmentCloneWorkflow orchestrates cloning all published features and plans
// from a source environment into a target environment.
// Activity 1: Clone features (must run first since plans may reference features via entitlements)
// Activity 2: Clone plans (runs after features are cloned)
func EnvironmentCloneWorkflow(ctx workflow.Context, in models.EnvironmentCloneWorkflowInput) (*EnvironmentCloneResult, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour * 1,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	result := &EnvironmentCloneResult{}

	// Activity 1: Clone all published features
	featuresInput := environmentActivities.CloneEnvironmentFeaturesInput{
		SourceEnvironmentID: in.SourceEnvironmentID,
		TargetEnvironmentID: in.TargetEnvironmentID,
		TenantID:            in.TenantID,
		UserID:              in.UserID,
	}
	var featuresOutput environmentActivities.CloneEnvironmentFeaturesOutput
	if err := workflow.ExecuteActivity(ctx, ActivityCloneEnvironmentFeatures, featuresInput).Get(ctx, &featuresOutput); err != nil {
		return nil, err
	}
	result.FeaturesCloned = featuresOutput.FeaturesCloned
	result.FeatureIDs = featuresOutput.FeatureIDs
	result.Errors = append(result.Errors, featuresOutput.Errors...)

	// Activity 2: Clone all published plans
	plansInput := environmentActivities.CloneEnvironmentPlansInput{
		SourceEnvironmentID: in.SourceEnvironmentID,
		TargetEnvironmentID: in.TargetEnvironmentID,
		TenantID:            in.TenantID,
		UserID:              in.UserID,
	}
	var plansOutput environmentActivities.CloneEnvironmentPlansOutput
	if err := workflow.ExecuteActivity(ctx, ActivityCloneEnvironmentPlans, plansInput).Get(ctx, &plansOutput); err != nil {
		return nil, err
	}
	result.PlansCloned = plansOutput.PlansCloned
	result.PlanIDs = plansOutput.PlanIDs
	result.Errors = append(result.Errors, plansOutput.Errors...)

	return result, nil
}
