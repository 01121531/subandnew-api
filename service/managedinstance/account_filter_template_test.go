package managedinstance

import (
	"testing"

	"github.com/01121531/subandnew-api/model"
	"github.com/stretchr/testify/require"
)

func TestAccountFilterTemplatesAreValidatedAndActorScoped(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ManagedAccountFilterTemplate{}))

	input := AccountFilterTemplateInput{
		Name: " Email filters ", MatchMode: AccountFilterMatchAll,
		Rules: []AccountFilterRule{
			{Field: "email", Operator: "contains", Values: []string{"gmail", "GMAIL", "outlook"}, ValueMode: AccountFilterValueAny},
			{Field: "available", Operator: "is", Values: []string{"available"}, ValueMode: AccountFilterValueAny},
		},
	}
	created, err := CreateAccountFilterTemplate(11, input)
	require.NoError(t, err)
	require.Equal(t, "Email filters", created.Name)
	require.Len(t, created.Rules[0].Values, 2)

	_, err = CreateAccountFilterTemplate(11, input)
	require.ErrorIs(t, err, ErrAccountFilterTemplateConflict)
	_, err = CreateAccountFilterTemplate(12, input)
	require.NoError(t, err)

	ownerTemplates, err := ListAccountFilterTemplates(11)
	require.NoError(t, err)
	require.Len(t, ownerTemplates, 1)
	otherTemplates, err := ListAccountFilterTemplates(12)
	require.NoError(t, err)
	require.Len(t, otherTemplates, 1)

	_, err = UpdateAccountFilterTemplate(created.Id, 12, input)
	require.ErrorIs(t, err, ErrAccountFilterTemplateMissing)
	updated, err := UpdateAccountFilterTemplate(created.Id, 11, AccountFilterTemplateInput{
		Name: "Only named", MatchMode: AccountFilterMatchAny,
		Rules: []AccountFilterRule{{Field: "name", Operator: "is_not_empty", ValueMode: AccountFilterValueAll}},
	})
	require.NoError(t, err)
	require.Equal(t, AccountFilterMatchAny, updated.MatchMode)
	require.Empty(t, updated.Rules[0].Values)

	require.ErrorIs(t, DeleteAccountFilterTemplate(created.Id, 12), ErrAccountFilterTemplateMissing)
	require.NoError(t, DeleteAccountFilterTemplate(created.Id, 11))
}

func TestAccountFilterTemplateRejectsInvalidRules(t *testing.T) {
	db := newManagedInstanceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.ManagedAccountFilterTemplate{}))

	tests := []AccountFilterTemplateInput{
		{Name: "", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "name", Operator: "contains", Values: []string{"a"}, ValueMode: AccountFilterValueAny}}},
		{Name: "bad mode", MatchMode: "xor", Rules: []AccountFilterRule{{Field: "name", Operator: "contains", Values: []string{"a"}, ValueMode: AccountFilterValueAny}}},
		{Name: "bad field", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "secret", Operator: "contains", Values: []string{"a"}, ValueMode: AccountFilterValueAny}}},
		{Name: "missing value", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "email", Operator: "contains", ValueMode: AccountFilterValueAny}}},
		{Name: "bad category operator", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "available", Operator: "contains", Values: []string{"available"}, ValueMode: AccountFilterValueAny}}},
	}
	for _, input := range tests {
		_, err := CreateAccountFilterTemplate(11, input)
		require.ErrorIs(t, err, ErrInvalidAccountFilterTemplate, input.Name)
	}
}
