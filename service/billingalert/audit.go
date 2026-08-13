package billingalert

import (
	"encoding/json"

	"github.com/01121531/subandnew-api/model"
)

func WriteAudit(actorID int, action string, resource string, resourceID int64, outcome string, details any) {
	encoded, err := json.Marshal(details)
	if err != nil {
		encoded = []byte(`{}`)
	}
	_ = model.DB.Create(&model.BillingAudit{
		ActorID: actorID, Action: action, Resource: resource, ResourceID: resourceID,
		Outcome: outcome, Details: string(encoded),
	}).Error
}
