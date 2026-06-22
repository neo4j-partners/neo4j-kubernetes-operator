package controller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestFindCondition(t *testing.T) {
	conditions := []metav1.Condition{
		{Type: "Ready", Status: metav1.ConditionTrue, Reason: "AllGood"},
		{Type: "ServersHealthy", Status: metav1.ConditionTrue, Reason: "Healthy"},
	}

	t.Run("found", func(t *testing.T) {
		c := findCondition(conditions, "Ready")
		assert.NotNil(t, c)
		assert.Equal(t, "AllGood", c.Reason)
	})

	t.Run("not found", func(t *testing.T) {
		c := findCondition(conditions, "NonExistent")
		assert.Nil(t, c)
	})

	t.Run("empty slice", func(t *testing.T) {
		c := findCondition(nil, "Ready")
		assert.Nil(t, c)
	})
}

func TestUpsertCondition(t *testing.T) {
	t.Run("update existing", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Pending"},
		}
		updated := upsertCondition(conditions, metav1.Condition{
			Type: "Ready", Status: metav1.ConditionTrue, Reason: "Done",
		})
		assert.Len(t, updated, 1)
		assert.Equal(t, metav1.ConditionTrue, updated[0].Status)
		assert.Equal(t, "Done", updated[0].Reason)
	})

	t.Run("append new", func(t *testing.T) {
		conditions := []metav1.Condition{
			{Type: "Ready", Status: metav1.ConditionTrue},
		}
		updated := upsertCondition(conditions, metav1.Condition{
			Type: "ServersHealthy", Status: metav1.ConditionTrue,
		})
		assert.Len(t, updated, 2)
	})

	t.Run("empty slice", func(t *testing.T) {
		updated := upsertCondition(nil, metav1.Condition{
			Type: "Ready", Status: metav1.ConditionTrue,
		})
		assert.Len(t, updated, 1)
	})
}
