package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/nedpals/supabase-go"
	"github.com/samber/lo"
	"github.com/sethvargo/go-password/password"
)

type UserService interface {
	GetUserInfo(ctx context.Context) (*dto.UserResponse, error)
	CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error)
	ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error)
}

type userService struct {
	userRepo     user.Repository
	tenantRepo   tenant.Repository
	rbacService  *rbac.RBACService
	supabaseAuth *supabase.Client
}

func NewUserService(
	userRepo user.Repository,
	tenantRepo tenant.Repository,
	rbacService *rbac.RBACService,
	supabaseAuth *supabase.Client,
) UserService {
	return &userService{
		userRepo:     userRepo,
		tenantRepo:   tenantRepo,
		rbacService:  rbacService,
		supabaseAuth: supabaseAuth,
	}
}

func (s *userService) GetUserInfo(ctx context.Context) (*dto.UserResponse, error) {
	userID := types.GetUserID(ctx)
	if userID == "" {
		return nil, ierr.NewError("user ID is required").
			WithHint("User ID is required").
			Mark(ierr.ErrValidation)
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation)
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	tenant, err := s.tenantRepo.GetByID(ctx, user.TenantID)
	if err != nil {
		return nil, err
	}

	return dto.NewUserResponse(user, tenant), nil
}

func (s *userService) CreateUser(ctx context.Context, req *dto.CreateUserRequest) (*dto.CreateUserResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant ID is required").
			WithHint("Tenant ID is required in context").
			Mark(ierr.ErrValidation)
	}

	// Verify tenant exists
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Get current user ID for audit fields
	currentUserID := types.GetUserID(ctx)
	if currentUserID == "" {
		currentUserID = "system"
	}

	var newUser *user.User
	var password string

	switch req.Type {
	case types.UserTypeUser:
		existingUser, err := s.userRepo.GetByEmail(ctx, req.Email)
		if err != nil && !ierr.IsNotFound(err) {
			return nil, err
		}
		if existingUser != nil {
			return nil, ierr.NewError("email already in use").
				WithHint("A user with this email already exists in this tenant").
				WithReportableDetails(map[string]interface{}{"email": req.Email}).
				Mark(ierr.ErrAlreadyExists)
		}
		plainPassword, err := generateSecurePassword()
		if err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to generate password").Mark(ierr.ErrSystem)
		}
		password = plainPassword

		// Create Supabase user first when client is available; use its ID as our user ID for consistency
		if s.supabaseAuth != nil {
			supabaseUser, err := s.supabaseAuth.Admin.CreateUser(ctx, supabase.AdminUserParams{
				Email:        req.Email,
				Password:     lo.ToPtr(plainPassword),
				EmailConfirm: true,
				AppMetadata: map[string]interface{}{
					"tenant_id": tenantID,
				},
			})
			if err != nil {
				return nil, ierr.WithError(err).WithHint("Failed to create user in auth provider").Mark(ierr.ErrSystem)
			}
			newUser = &user.User{
				ID:    supabaseUser.ID,
				Email: req.Email,
				Type:  types.UserTypeUser,
				Roles: []string{},
				BaseModel: types.BaseModel{
					TenantID:  tenantID,
					Status:    types.StatusPublished,
					CreatedBy: currentUserID,
					UpdatedBy: currentUserID,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			}
		} else {
			newUser = &user.User{
				ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
				Email: req.Email,
				Type:  types.UserTypeUser,
				Roles: []string{},
				BaseModel: types.BaseModel{
					TenantID:  tenantID,
					Status:    types.StatusPublished,
					CreatedBy: currentUserID,
					UpdatedBy: currentUserID,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			}
		}
		if err := newUser.Validate(); err != nil {
			return nil, err
		}
		if err := s.userRepo.Create(ctx, newUser); err != nil {
			return nil, err
		}
	case types.UserTypeServiceAccount:
		for _, role := range req.Roles {
			if !s.rbacService.ValidateRole(role) {
				return nil, ierr.NewError("invalid role").
					WithHint("Role '" + role + "' does not exist").
					WithReportableDetails(map[string]interface{}{"role": role}).
					Mark(ierr.ErrValidation)
			}
		}
		newUser = &user.User{
			ID:    types.GenerateUUIDWithPrefix(types.UUID_PREFIX_USER),
			Email: "",
			Type:  types.UserTypeServiceAccount,
			Roles: req.Roles,
			BaseModel: types.BaseModel{
				TenantID:  tenantID,
				Status:    types.StatusPublished,
				CreatedBy: currentUserID,
				UpdatedBy: currentUserID,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			},
		}
		if err := newUser.Validate(); err != nil {
			return nil, err
		}
		if err := s.userRepo.Create(ctx, newUser); err != nil {
			return nil, err
		}
	default:
		return nil, ierr.NewError("invalid user type").WithHint("Type must be 'user' or 'service_account'").Mark(ierr.ErrValidation)
	}

	return &dto.CreateUserResponse{
		UserResponse: dto.NewUserResponse(newUser, tenant),
		Password:     password,
	}, nil
}

// generateSecurePassword returns a cryptographically secure random password via sethvargo/go-password (crypto/rand).
func generateSecurePassword() (string, error) {
	// 16 chars, 4 digits, 2 symbols, allow upper, no repeat (strong, copyable)
	return password.Generate(16, 4, 2, false, false)
}

func (s *userService) ListUsersByFilter(ctx context.Context, filter *types.UserFilter) (*dto.ListUsersResponse, error) {
	// Get tenant ID from context
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		return nil, ierr.NewError("tenant_id not found in context").
			WithHint("Authentication context is missing tenant information").
			Mark(ierr.ErrValidation)
	}

	// Get users by filter from repository (tenantID comes from context in repo)
	users, total, err := s.userRepo.ListByFilter(ctx, filter)
	if err != nil {
		return nil, err
	}

	// Get tenant for response construction
	tenant, err := s.tenantRepo.GetByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}

	// Convert to DTOs
	userResponses := make([]*dto.UserResponse, len(users))
	for i, u := range users {
		userResponses[i] = dto.NewUserResponse(u, tenant)
	}

	return &dto.ListUsersResponse{
		Items:      userResponses,
		Pagination: types.NewPaginationResponse(int(total), filter.GetLimit(), filter.GetOffset()),
	}, nil
}
