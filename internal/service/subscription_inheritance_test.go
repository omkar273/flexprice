package service

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func TestValidateHierarchyWorkflowConflict_parentCustomer(t *testing.T) {
	parentID := "cust_parent"

	t.Run("parent creating PARENT conflicts with existing STANDALONE", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, parentID, types.SubscriptionTypeParent, types.SubscriptionTypeStandalone)
		require.Error(t, err)
	})

	t.Run("parent creating STANDALONE conflicts with existing PARENT", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, parentID, types.SubscriptionTypeStandalone, types.SubscriptionTypeParent)
		require.Error(t, err)
	})

	t.Run("parent creating STANDALONE conflicts with existing INHERITED", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, parentID, types.SubscriptionTypeStandalone, types.SubscriptionTypeInherited)
		require.Error(t, err)
	})

	t.Run("no conflict parent PARENT with existing INHERITED", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, parentID, types.SubscriptionTypeParent, types.SubscriptionTypeInherited)
		require.NoError(t, err)
	})

	t.Run("no conflict parent STANDALONE with existing STANDALONE when new is PARENT skipped", func(t *testing.T) {
		// Parent + existing standalone + new standalone is allowed (no branch matches conflict for parent case)
		err := validateHierarchyWorkflowConflict(parentID, parentID, types.SubscriptionTypeStandalone, types.SubscriptionTypeStandalone)
		require.NoError(t, err)
	})
}

func TestValidateHierarchyWorkflowConflict_childCustomer(t *testing.T) {
	parentID := "cust_parent"
	childID := "cust_child"

	t.Run("child with existing STANDALONE", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, childID, types.SubscriptionTypeParent, types.SubscriptionTypeStandalone)
		require.Error(t, err)
	})

	t.Run("child with existing PARENT", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, childID, types.SubscriptionTypeParent, types.SubscriptionTypeParent)
		require.Error(t, err)
	})

	t.Run("no conflict child with existing INHERITED", func(t *testing.T) {
		err := validateHierarchyWorkflowConflict(parentID, childID, types.SubscriptionTypeParent, types.SubscriptionTypeInherited)
		require.NoError(t, err)
	})
}
