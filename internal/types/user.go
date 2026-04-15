package types

import ierr "github.com/flexprice/flexprice/internal/errors"

// UserType represents the type of user
type UserType string

const (
	UserTypeUser           UserType = "user"
	UserTypeServiceAccount UserType = "service_account"
)

// Validate validates the user type
func (ut UserType) Validate() error {
	switch ut {
	case UserTypeUser, UserTypeServiceAccount:
		return nil
	default:
		return ierr.NewError("invalid user type").
			WithHint("User type must be 'user' or 'service_account'").
			Mark(ierr.ErrValidation)
	}
}

type UserFilter struct {
	*QueryFilter
	*TimeRangeFilter
	*DSLFilter

	// Specific filters for users
	UserIDs []string  `json:"user_ids,omitempty" form:"user_ids" validate:"omitempty"`
	Type    *UserType `json:"type,omitempty" form:"type" validate:"omitempty,oneof=user service_account"`
	Roles   []string  `json:"roles,omitempty" form:"roles" validate:"omitempty"`
}
