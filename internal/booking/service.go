package booking

import (
	"context"
	"errors"
	"time"

	"playroom-bot/internal/domain"
)

// Ограничения на длительность одного слота брони.
const (
	MinSlotDuration = time.Hour
	MaxSlotDuration = 5 * time.Hour
)

// Часы работы комнаты: бронировать можно только внутри [BusinessHoursStart,
// BusinessHoursEnd). Используются и здесь для валидации, и в telegram-пакете
// для границ сетки /free - единственный источник правды на оба места.
const (
	BusinessHoursStart = 9  // 09:00
	BusinessHoursEnd   = 23 // 23:00
)

// MaxBookingsPerWeek - сколько активных броней может быть у одного
// пользователя в одну календарную неделю (Пн-Вс), независимо от их
// длительности. Неделя считается по дате СТАРТА новой брони, а не по "сейчас".
const MaxBookingsPerWeek = 3

var (
	ErrInvalidDuration      = errors.New("booking: слот должен быть от 1 до 5 часов")
	ErrSlotTaken            = errors.New("booking: время уже занято")
	ErrPastTime             = errors.New("booking: нельзя бронировать прошедшее время")
	ErrOutsideBusinessHours = errors.New("booking: бронировать можно только с 9:00 до 23:00")
	ErrWeeklyLimitReached   = errors.New("booking: не больше 3 броней в неделю")
)

// Service - бизнес-логика бронирования: валидация длительности слота,
// проверка пересечений с чужими бронями и контроль доступа (пользователь
// может отменить только свою бронь).
type Service interface {
	// Book создаёт бронь для userID на интервал [start, end).
	Book(ctx context.Context, userID int64, userName string, start, end time.Time) (domain.Booking, error)

	// Cancel отменяет бронь, только если она принадлежит userID.
	Cancel(ctx context.Context, userID int64, bookingID int64) error

	// MyBookings возвращает полные данные о бронях конкретного пользователя.
	MyBookings(ctx context.Context, userID int64) ([]domain.Booking, error)

	// Availability возвращает занятые интервалы за период без информации
	// о том, кто их забронировал - для отображения "занято/свободно".
	Availability(ctx context.Context, from, to time.Time) ([]domain.Slot, error)
}

type service struct {
	repo Repository // хранит зависимость - хранилище, реализующее Repository
}

// NewService создаёт Service поверх переданного хранилища r.
// Параметр называется "r", а не "repo", чтобы не совпадать с именем поля repo ниже.
func NewService(r Repository) Service {
	return &service{repo: r}
}

func (svc *service) Book(ctx context.Context, userID int64, userName string, start, end time.Time) (domain.Booking, error) {
	if start.Before(time.Now()) {
		return domain.Booking{}, ErrPastTime
	}

	duration := end.Sub(start)
	if duration < MinSlotDuration || duration > MaxSlotDuration {
		return domain.Booking{}, ErrInvalidDuration
	}

	dayStart := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, start.Location())
	businessStart := dayStart.Add(BusinessHoursStart * time.Hour)
	businessEnd := dayStart.Add(BusinessHoursEnd * time.Hour)
	if start.Before(businessStart) || end.After(businessEnd) {
		return domain.Booking{}, ErrOutsideBusinessHours
	}

	existing, err := svc.repo.ListByUser(ctx, userID)
	if err != nil {
		return domain.Booking{}, err
	}
	weekStart, weekEnd := weekBounds(start)
	weekCount := 0
	for _, bk := range existing {
		if !bk.StartTime.Before(weekStart) && bk.StartTime.Before(weekEnd) {
			weekCount++
		}
	}
	if weekCount >= MaxBookingsPerWeek {
		return domain.Booking{}, ErrWeeklyLimitReached
	}

	busy, err := svc.repo.ListBusySlots(ctx, start, end)
	if err != nil {
		return domain.Booking{}, err
	}
	if len(busy) > 0 {
		return domain.Booking{}, ErrSlotTaken
	}

	// Resource: domain.ResourceAll - пока бронируется только вся комната целиком.
	// TODO: когда Book сможет принимать конкретный ресурс (PS_0, kicker, ...),
	// заменить на параметр вместо жёстко заданного ResourceAll.
	return svc.repo.Create(ctx, domain.Booking{
		UserID:    userID,
		UserName:  userName,
		Resource:  domain.ResourceAll,
		StartTime: start,
		EndTime:   end,
	})
}

func (svc *service) Cancel(ctx context.Context, userID int64, bookingID int64) error {
	bk, err := svc.repo.GetByID(ctx, bookingID)
	if err != nil {
		return err // в том числе ErrNotFound, если брони с таким ID нет
	}
	if bk.UserID != userID {
		return ErrForbidden
	}
	return svc.repo.Cancel(ctx, bookingID)
}

func (svc *service) MyBookings(ctx context.Context, userID int64) ([]domain.Booking, error) {
	return svc.repo.ListByUser(ctx, userID)
}

func (svc *service) Availability(ctx context.Context, from, to time.Time) ([]domain.Slot, error) {
	return svc.repo.ListBusySlots(ctx, from, to)
}

// weekBounds возвращает [start, end) календарной недели (Пн 00:00 - следующий
// Пн 00:00), содержащей t. time.Weekday нумерует дни с воскресенья (0), поэтому
// воскресенье отдельно приводится к "7", чтобы не оказаться "перед" понедельником.
func weekBounds(t time.Time) (start, end time.Time) {
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())

	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // воскресенье - последний день недели, а не первый
	}
	daysSinceMonday := weekday - 1

	start = dayStart.AddDate(0, 0, -daysSinceMonday)
	end = start.AddDate(0, 0, 7)
	return start, end
}
