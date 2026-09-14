package authz

import (
	"context"
	"net/url"
	"strings"
)

func (a *DataAccess) CheckQuery(input url.Values) error {
	for key, values := range input {
		if strings.TrimSpace(strings.Join(values, "")) == "" {
			continue
		}
		if key == "sort" || key == "sort_by" {
			for _, value := range values {
				if err := a.CheckDataField(value); err != nil {
					return err
				}
			}
		}
		if DataFieldGroup(key) != "" {
			if err := a.CheckDataField(key); err != nil {
				return err
			}
		}
		if (key == "search" || key == "keyword") && (!a.HasField("email") || !a.HasField("vendor")) {
			return ErrDataForbidden
		}
	}
	return nil
}

func CheckContextQuery(ctx context.Context, instanceID int64, input url.Values) error {
	if err := CheckContextInstances(ctx, instanceID); err != nil {
		return err
	}
	if a := DataAccessFrom(ctx); a != nil {
		return a.CheckQuery(input)
	}
	return nil
}
