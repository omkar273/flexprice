package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	authProvider "github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	domainAuth "github.com/flexprice/flexprice/internal/domain/auth"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
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
	userRepo        user.Repository
	tenantRepo      tenant.Repository
	authRepo        domainAuth.Repository
	cfg             *config.Configuration
	rbacService     *rbac.RBACService
	supabaseAuth    *supabase.Client
	settingsService SettingsService
	logger          *logger.Logger
}

func NewUserService(
	userRepo user.Repository,
	tenantRepo tenant.Repository,
	authRepo domainAuth.Repository,
	cfg *config.Configuration,
	rbacService *rbac.RBACService,
	supabaseAuth *supabase.Client,
	settingsService SettingsService,
	logger *logger.Logger,
) UserService {
	return &userService{
		userRepo:        userRepo,
		tenantRepo:      tenantRepo,
		authRepo:        authRepo,
		cfg:             cfg,
		rbacService:     rbacService,
		supabaseAuth:    supabaseAuth,
		settingsService: settingsService,
		logger:          logger,
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
	var password *string

	switch req.Type {
	case types.UserTypeUser:
		// invite user to the tenant
		newUser, password, err = s.InviteUser(ctx, req, currentUserID)
		if err != nil {
			return nil, err
		}

	case types.UserTypeServiceAccount:
		if s.rbacService == nil {
			return nil, ierr.NewError("RBAC not configured").
				WithHint("Service accounts require RBAC for role validation; provide a non-nil RBAC service.").
				Mark(ierr.ErrValidation)
		}
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
		Password:     *password,
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

// InviteUser invites a user to the tenant
func (s *userService) InviteUser(ctx context.Context, req *dto.CreateUserRequest, currentUserID string) (*user.User, *string, error) {

	var userID string
	tenantID := types.GetTenantID(ctx)

	// Check if user by email already exists
	existingUser, err := s.userRepo.GetByEmail(ctx, req.Email)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, nil, err
	}
	if existingUser != nil {
		return nil, nil, ierr.NewError("email already in use").
			WithHint("A user with this email already exists in this tenant").
			WithReportableDetails(map[string]interface{}{"email": req.Email}).
			Mark(ierr.ErrAlreadyExists)
	}
	// Enforce per-tenant user limit from add_user_config (GetSetting returns default when not set)
	svc, ok := s.settingsService.(*settingsService)
	if !ok || svc == nil {
		return nil, nil, ierr.NewError("settings service not configured").
			WithHint("User creation requires settings service for add_user_config.").
			Mark(ierr.ErrValidation)
	}
	addUserConfig, err := GetSetting[types.TenantConfig](svc, ctx, types.SettingKeyTenantConfig)
	if err != nil {
		return nil, nil, err
	}
	// ListByFilter uses tenant from context and repo filters by StatusPublished
	_, totalActiveUsers, err := s.userRepo.ListByFilter(ctx, &types.UserFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(1),
			Offset: lo.ToPtr(0),
			Status: lo.ToPtr(types.StatusPublished),
		},
	})
	if err != nil {
		return nil, nil, err
	}
	if totalActiveUsers >= int64(addUserConfig.MaxUsers) {
		return nil, nil, ierr.NewError("user limit reached: you cannot add any more users").
			WithHintf("Maximum %d user(s) allowed for this tenant. Limit reached.", addUserConfig.MaxUsers).
			WithReportableDetails(map[string]interface{}{"max_users": addUserConfig.MaxUsers, "current_active_users": totalActiveUsers}).
			Mark(ierr.ErrValidation)
	}
	password, err := generateSecurePassword()
	if err != nil {
		return nil, nil, ierr.WithError(err).WithHint("Failed to generate password").
			Mark(ierr.ErrSystem)
	}

	if s.cfg == nil {
		return nil, nil, ierr.NewError("auth configuration missing").
			WithHint("User creation requires auth provider configuration").
			Mark(ierr.ErrValidation)
	}

	switch s.cfg.Auth.Provider {
	case types.AuthProviderSupabase:
		if s.supabaseAuth == nil {
			return nil, nil, ierr.NewError("supabase auth not configured").
				WithHint("Supabase auth provider is selected but Supabase client is nil").
				Mark(ierr.ErrValidation)
		}
		// Create in Supabase first; only on success create in DB. We cannot rollback either side
		// This ordering ensures we never persist in DB without auth; if DB create fails after Supabase succeeds, caller sees the error.
		supabaseUser, err := s.supabaseAuth.Admin.CreateUser(ctx, supabase.AdminUserParams{
			Email:        req.Email,
			Password:     lo.ToPtr(password),
			EmailConfirm: true,
			AppMetadata: map[string]interface{}{
				"tenant_id": tenantID,
			},
		})
		if err != nil {
			return nil, nil, ierr.WithError(err).WithHint("Failed to create user in auth provider").
				Mark(ierr.ErrSystem)
		}
		userID = supabaseUser.ID

	case types.AuthProviderFlexprice:
		s.logger.InfowCtx(ctx,
			"supabase auth not configured, using flexprice auth provider",
			"email", req.Email,
			"tenant_id", tenantID,
		)
		if s.cfg == nil || s.authRepo == nil {
			return nil, nil, ierr.NewError("flexprice auth provider not configured").
				WithHint("Provide configuration and auth repository to create users without Supabase").
				Mark(ierr.ErrValidation)
		}

		flexpriceProvider := authProvider.NewFlexpriceAuth(s.cfg)
		authResp, err := flexpriceProvider.SignUp(ctx, authProvider.AuthRequest{
			Email:    req.Email,
			Password: password,
			TenantID: tenantID,
		})
		if err != nil {
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to create user in flexprice auth provider").
				Mark(ierr.ErrSystem)
		}
		userID = authResp.ID
		if err := s.authRepo.CreateAuth(ctx, domainAuth.NewAuth(authResp.ID, types.AuthProviderFlexprice, authResp.ProviderToken)); err != nil {
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to create authentication record").
				WithReportableDetails(map[string]interface{}{"user_id": userID}).
				Mark(ierr.ErrDatabase)
		}
	default:
		return nil, nil, ierr.NewError("auth provider not configured").
			WithHint("Auth provider not configured").
			Mark(ierr.ErrValidation)
	}

	newUser := &user.User{
		ID:    userID,
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

	if err := newUser.Validate(); err != nil {
		return nil, nil, err
	}
	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, nil, err
	}
	return newUser, &password, nil
}
