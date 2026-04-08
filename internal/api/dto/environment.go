package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type CreateEnvironmentRequest struct {
	Name string `json:"name" validate:"required"`
	Type string `json:"type" validate:"required"`
}

type UpdateEnvironmentRequest struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type EnvironmentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ListEnvironmentsResponse struct {
	Environments []EnvironmentResponse `json:"environments"`
	Total        int                   `json:"total"`
	Offset       int                   `json:"offset"`
	Limit        int                   `json:"limit"`
}

// CloneEnvironmentRequest represents the request to clone an environment's published features and plans.
// A new environment is always created from the name and type provided, then all published features
// and plans are cloned from the source environment into it.
type CloneEnvironmentRequest struct {
	// Name of the new environment (required)
	Name string `json:"name" validate:"required"`
	// Type of the new environment, e.g. "production" or "development" (required)
	Type types.EnvironmentType `json:"type" validate:"required"`
}

func (r *CloneEnvironmentRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if r.Type != types.EnvironmentDevelopment && r.Type != types.EnvironmentProduction {
		return ierr.NewError("invalid environment type").
			WithHintf("type must be one of: %s, %s", types.EnvironmentDevelopment, types.EnvironmentProduction).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// CloneEnvironmentResponse represents the async response when an environment clone workflow is started.
type CloneEnvironmentResponse struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
	Message    string `json:"message"`
}

func (r *CreateEnvironmentRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *CreateEnvironmentRequest) ToEnvironment(ctx context.Context) *environment.Environment {
	return &environment.Environment{
		ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_ENVIRONMENT),
		Name:      r.Name,
		Type:      types.EnvironmentType(r.Type),
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
}

func (r *UpdateEnvironmentRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func NewEnvironmentResponse(e *environment.Environment) *EnvironmentResponse {
	return &EnvironmentResponse{
		ID:        e.ID,
		Name:      e.Name,
		Type:      string(e.Type),
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
		UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
}
