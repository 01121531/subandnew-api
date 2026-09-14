package managedinstance

import (
	"errors"

	"github.com/01121531/subandnew-api/model"
	"gorm.io/gorm"
)

var (
	ErrInstanceOrderConflict = errors.New("managed instance order changed; reload before saving")
	ErrInvalidInstanceOrder  = errors.New("instance order must contain every instance exactly once")
)

type OrderItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type OrderView struct {
	Version int64       `json:"version"`
	Items   []OrderItem `json:"items"`
}

func readOrder(tx *gorm.DB, version int64) (*OrderView, error) {
	view := &OrderView{Version: version, Items: []OrderItem{}}
	err := tx.Model(&model.ManagedInstance{}).Select("id", "name", "kind").
		Order(model.ManagedInstanceOrderSQL).Scan(&view.Items).Error
	return view, err
}

func GetOrder() (*OrderView, error) {
	var view *OrderView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		state, err := model.LockManagedInstanceOrder(tx)
		if err != nil {
			return err
		}
		view, err = readOrder(tx, state.Version)
		return err
	})
	return view, err
}

func SaveOrder(version int64, ids []int64, actorID int) (*OrderView, error) {
	var view *OrderView
	err := model.DB.Transaction(func(tx *gorm.DB) error {
		state, err := model.LockManagedInstanceOrder(tx)
		if err != nil {
			return err
		}
		if version != state.Version {
			return ErrInstanceOrderConflict
		}
		current, err := readOrder(tx, state.Version)
		if err != nil {
			return err
		}
		if len(ids) != len(current.Items) {
			return ErrInvalidInstanceOrder
		}
		remaining := make(map[int64]bool, len(current.Items))
		for _, item := range current.Items {
			remaining[item.ID] = true
		}
		for _, id := range ids {
			if !remaining[id] {
				return ErrInvalidInstanceOrder
			}
			delete(remaining, id)
		}
		for index, id := range ids {
			if err := tx.Model(&model.ManagedInstance{}).Where("id = ?", id).
				UpdateColumn("sort_order", index+1).Error; err != nil {
				return err
			}
		}
		if err := model.AdvanceManagedInstanceOrder(tx); err != nil {
			return err
		}
		if err := writeAudit(tx, 0, actorID, "order_update", map[string]any{"instance_ids": ids, "version": state.Version + 1}); err != nil {
			return err
		}
		view, err = readOrder(tx, state.Version+1)
		return err
	})
	return view, err
}

func appendInstanceOrder(tx *gorm.DB, instance *model.ManagedInstance) error {
	if _, err := model.LockManagedInstanceOrder(tx); err != nil {
		return err
	}
	var maximum int64
	if err := tx.Model(&model.ManagedInstance{}).Select("COALESCE(MAX(sort_order), 0)").Scan(&maximum).Error; err != nil {
		return err
	}
	instance.SortOrder = maximum + 1
	return model.AdvanceManagedInstanceOrder(tx)
}
