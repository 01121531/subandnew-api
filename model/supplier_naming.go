package model

import "fmt"

type SupplierNamingRule struct {
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

type SupplierEffectiveNaming struct {
	SupplierNamingRule
	Source  string `json:"source"`
	Version string `json:"version"`
}

func ResolveSupplierNaming(owner Supplier, binding *SupplierBinding) *SupplierEffectiveNaming {
	rule := SupplierNamingRule{}
	if owner.NamingRule != nil {
		rule = *owner.NamingRule
	}
	result := &SupplierEffectiveNaming{SupplierNamingRule: rule, Source: "supplier", Version: fmt.Sprintf("s:%d:%d", owner.ID, owner.NamingVersion)}
	if binding != nil {
		if binding.NamingOverride != nil {
			result.SupplierNamingRule = *binding.NamingOverride
			result.Source = "binding"
			result.Version = fmt.Sprintf("b:%d:%d", binding.ID, binding.NamingVersion)
		} else {
			result.Version += fmt.Sprintf(":b:%d:%d", binding.ID, binding.NamingVersion)
		}
	}
	return result
}
