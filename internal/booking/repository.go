package booking

import (
	"context"
	"errors"
	"time"

	"playroom-bot/internal/domain"
)

// ErrNotFound возвращается, когда брони с таким ID не существует.
var ErrNotFound = errors.New("booking: not found")

// ErrForbidden возвращается при попытке прочитать/изменить чужую бронь.
var ErrForbidden = errors.New("booking: forbidden")

// Repository - слой хранения броней. Реализации: internal/storage/sqlite.
//
// Разделение методов не случайно: ListBusySlots не должен раскрывать,
// кому принадлежит бронь, а ListByUser отдаёт полные данные, но только
// для конкретного userID. Это отражает требование безопасности -
// видеть можно факт чужой брони, но не её детали.
type Repository interface {
	Create(ctx context.Context, bk domain.Booking) (domain.Booking, error)
	GetByID(ctx context.Context, id int64) (domain.Booking, error)
	ListByUser(ctx context.Context, userID int64) ([]domain.Booking, error)
	ListBusySlots(ctx context.Context, from, to time.Time) ([]domain.Slot, error)
	Cancel(ctx context.Context, id int64) error
}
