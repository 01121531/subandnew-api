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
		{Name: "bad metric operator", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "requests", Operator: "contains", Values: []string{"10"}, ValueMode: AccountFilterValueAny}}},
		{Name: "bad metric value", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "amount", Operator: "gte", Values: []string{"many"}, ValueMode: AccountFilterValueAny}}},
		{Name: "bad range", MatchMode: AccountFilterMatchAll, Rules: []AccountFilterRule{{Field: "tokens", Operator: "between", Values: []string{"10"}, ValueMode: AccountFilterValueAny}}},
	}
	for _, input := range tests {
		_, err := CreateAccountFilterTemplate(11, input)
		require.ErrorIs(t, err, ErrInvalidAccountFilterTemplate, input.Name)
	}
}

func TestNormalizeAccountFilterAcceptsMetricAndChinaTimeRules(t *testing.T) {
	_, rules, err := NormalizeAccountFilter(AccountFilterMatchAll, []AccountFilterRule{
		{Field: "name", Operator: "starts_with", Values: []string{"allen"}, ValueMode: AccountFilterValueAny},
		{Field: "email", Operator: "ends_with", Values: []string{"@example.com"}, ValueMode: AccountFilterValueAny},
		{Field: "requests", Operator: "gte", Values: []string{"100"}, ValueMode: AccountFilterValueAny},
		{Field: "amount", Operator: "between", Values: []string{"1.25", "9.50"}, ValueMode: AccountFilterValueAny},
		{Field: "created_at", Operator: "lt", Values: []string{"2026-08-30 00:00"}, ValueMode: AccountFilterValueAny},
	}, true)
	require.NoError(t, err)
	require.Len(t, rules, 5)

	chinaTime, err := ParseAccountFilterMetricValue("created_at", "2026-08-29 09:30")
	require.NoError(t, err)
	require.Equal(t, float64(1787967000), chinaTime)
}
