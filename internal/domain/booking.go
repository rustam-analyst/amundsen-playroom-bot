// Package domain содержит основные бизнес-сущности приложения,
// не зависящие ни от Telegram, ни от способа хранения данных.
package domain

import "time"

// BookingStatus - статус брони.
type BookingStatus string

const (
	StatusActive    BookingStatus = "active"
	StatusCancelled BookingStatus = "cancelled"
)

// Resource - конкретный элемент комнаты, который бронируется.
// Пока есть только ResourceAll (комната целиком) - колонка и поле уже
// заведены заранее, чтобы не делать миграцию БД позже.
// TODO: когда появится бронирование отдельных элементов - добавить
// ResourcePS0, ResourcePS1, ResourceKicker, ResourceMovie и т.п., и провести
// реальную миграцию данных с ResourceAll на конкретные значения.
type Resource string

const ResourceAll Resource = "all"

// Booking - бронь игровой комнаты.
// Содержит данные о владельце, поэтому наружу (другим пользователям)
// отдавать Booking целиком нельзя - только через Slot.
type Booking struct {
	ID        int64
	UserID    int64  // Telegram user id владельца брони
	UserName  string // отображаемое имя владельца (видно только ему самому)
	Resource  Resource
	StartTime time.Time
	EndTime   time.Time
	Status    BookingStatus
	CreatedAt time.Time
}

// Duration - длительность брони.
func (bk Booking) Duration() time.Duration {
	return bk.EndTime.Sub(bk.StartTime)
}

// Slot - занятой промежуток времени без привязки к владельцу.
// Используется для отображения доступности комнаты так, чтобы
// не раскрывать, кто именно её забронировал.
type Slot struct {
	StartTime time.Time
	EndTime   time.Time
}
