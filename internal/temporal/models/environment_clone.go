package models

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// EnvironmentCloneWorkflowInput represents input for the environment clone workflow.
type EnvironmentCloneWorkflowInput struct {
	// SourceEnvironmentID is the environment being cloned.
	SourceEnvironmentID string `json:"source_environment_id"`
	// TargetEnvironmentID is the environment to clone into.
	TargetEnvironmentID string `json:"target_environment_id"`
	TenantID            string `json:"tenant_id"`
	UserID              string `json:"user_id"`
}

func (e *EnvironmentCloneWorkflowInput) Validate() error {
	if e.SourceEnvironmentID == "" {
		return ierr.NewError("source environment ID is required").
			WithHint("Source environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if e.TargetEnvironmentID == "" {
		return ierr.NewError("target environment ID is required").
			WithHint("Target environment ID is required").
			Mark(ierr.ErrValidation)
	}
	if e.SourceEnvironmentID == e.TargetEnvironmentID {
		return ierr.NewError("source and target environment IDs cannot be the same").
			WithHint("Source and target environment IDs cannot be the same").
			Mark(ierr.ErrValidation)
	}
	if e.TenantID == "" {
		return ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}
