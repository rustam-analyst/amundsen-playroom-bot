package telegram

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"playroom-bot/internal/booking"
	"playroom-bot/internal/domain"
)

// availabilityWindow - на сколько вперёд от текущего момента показывать
// занятость по команде /free.
// TODO: пагинация - команда/кнопка "следующая неделя", чтобы смотреть дальше
// одного фиксированного окна.
const availabilityWindow = 7 * 24 * time.Hour

// handleStart отвечает на /start и /help кратким описанием бота.
func handleStart(ctx context.Context, bot *Bot, msg *tgbotapi.Message) error {
	text := "Привет! Я бот для брони игровой комнаты в атриуме 4й секции ЖК Амундсен.\n\n" +
		"Команды:\n" +
		"/book - забронировать слот\n" +
		"/my - мои брони\n" +
		"/cancel - отменить бронь\n" +
		"/free - проверить свободное время"
	return bot.Send(msg.Chat.ID, text)
}

// handleBook создаёт новую бронь. Формат: /book ДД.ММ ЧЧ:ММ часы.
func handleBook(ctx context.Context, bot *Bot, msg *tgbotapi.Message) error {
	// TODO: когда появится бронирование отдельных элементов комнаты
	// (PS_0, PS_1, kicker, movie, ...), сюда добавится ещё один аргумент -
	// тип брони; см. также TODO в sqlite.go про колонку resource.
	const usage = "формат: /book ДД.ММ ЧЧ:ММ часы\nнапример: /book 10.08 14:00 2"

	args := strings.Fields(msg.CommandArguments())
	if len(args) != 3 {
		return bot.Send(msg.Chat.ID, usage)
	}

	start, err := parseBookingStart(args[0], args[1])
	if err != nil {
		return bot.Send(msg.Chat.ID, err.Error()+"\n"+usage)
	}

	hours, err := strconv.Atoi(args[2])
	if err != nil || hours <= 0 {
		return bot.Send(msg.Chat.ID, "часы должны быть целым положительным числом\n"+usage)
	}
	end := start.Add(time.Duration(hours) * time.Hour)

	created, err := bot.service.Book(ctx, msg.From.ID, msg.From.UserName, start, end)
	if err != nil {
		return bot.Send(msg.Chat.ID, bookingErrorText(err))
	}

	text := fmt.Sprintf("Забронировано №%d: %s – %s", created.ID,
		created.StartTime.Format("02.01 15:04"), created.EndTime.Format("02.01 15:04"))
	return bot.Send(msg.Chat.ID, text)
}

// parseBookingStart парсит дату (ДД.ММ) и время (ЧЧ:ММ) в time.Time текущего
// года. Если получившаяся дата уже прошла в этом году, service.Book сам
// вернёт ErrPastTime - отдельно "перематывать" на следующий год не нужно.
func parseBookingStart(dateStr, timeStr string) (time.Time, error) {
	var day, month, hour, minute int
	if _, err := fmt.Sscanf(dateStr, "%d.%d", &day, &month); err != nil {
		return time.Time{}, fmt.Errorf("не понял дату %q, ожидался формат ДД.ММ", dateStr)
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("некорректная дата %q", dateStr)
	}
	// TODO: это грубая проверка диапазона, не календарная - например, 31.02
	// пройдёт её, а time.Date ниже молча "перекатит" такую дату в начало марта
	// вместо ошибки. Если это станет проблемой - проверять через сравнение
	// .Month() у результата time.Date с ожидаемым month.
	if _, err := fmt.Sscanf(timeStr, "%d:%d", &hour, &minute); err != nil {
		return time.Time{}, fmt.Errorf("не понял время %q, ожидался формат ЧЧ:ММ", timeStr)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("некорректное время %q", timeStr)
	}

	now := time.Now()
	return time.Date(now.Year(), time.Month(month), day, hour, minute, 0, 0, now.Location()), nil
}

// bookingErrorText превращает ошибку service.Book в понятный пользователю текст.
func bookingErrorText(err error) string {
	switch {
	case errors.Is(err, booking.ErrPastTime):
		return "нельзя бронировать прошедшее время"
	case errors.Is(err, booking.ErrInvalidDuration):
		return fmt.Sprintf("длительность брони должна быть от %s до %s", booking.MinSlotDuration, booking.MaxSlotDuration)
	case errors.Is(err, booking.ErrSlotTaken):
		return "это время уже занято, проверьте /free"
	default:
		return "не получилось забронировать: " + err.Error()
	}
}

// handleMyBookings показывает пользователю только его собственные брони.
func handleMyBookings(ctx context.Context, bot *Bot, msg *tgbotapi.Message) error {
	bookings, err := bot.service.MyBookings(ctx, msg.From.ID)
	if err != nil {
		return err
	}

	return bot.Send(msg.Chat.ID, formatMyBookings(bookings))
}

// formatMyBookings превращает список броней пользователя в текст сообщения.
func formatMyBookings(bookings []domain.Booking) string {
	if len(bookings) == 0 {
		return "У вас нет активных броней."
	}

	var b strings.Builder
	b.WriteString("Ваши брони:\n")
	for _, bk := range bookings {
		fmt.Fprintf(&b, "№%d: %s – %s\n", bk.ID,
			bk.StartTime.Format("02.01 15:04"), bk.EndTime.Format("02.01 15:04"))
	}
	return b.String()
}

// handleCancel отменяет бронь пользователя. Сервис сам проверит владельца.
func handleCancel(ctx context.Context, bot *Bot, msg *tgbotapi.Message) error {
	const usage = "формат: /cancel №брони\nнапример: /cancel 42"

	arg := strings.TrimSpace(msg.CommandArguments())
	bookingID, err := strconv.ParseInt(arg, 10, 64)
	if err != nil {
		return bot.Send(msg.Chat.ID, "не понял номер брони\n"+usage)
	}

	if err := bot.service.Cancel(ctx, msg.From.ID, bookingID); err != nil {
		return bot.Send(msg.Chat.ID, cancelErrorText(err))
	}

	return bot.Send(msg.Chat.ID, fmt.Sprintf("Бронь №%d отменена", bookingID))
}

// cancelErrorText превращает ошибку service.Cancel в понятный пользователю текст.
func cancelErrorText(err error) string {
	switch {
	case errors.Is(err, booking.ErrNotFound):
		return "брони с таким номером нет"
	case errors.Is(err, booking.ErrForbidden):
		return "это не ваша бронь"
	default:
		return "не получилось отменить: " + err.Error()
	}
}

// handleAvailability показывает занятые интервалы без указания владельца.
func handleAvailability(ctx context.Context, bot *Bot, msg *tgbotapi.Message) error {
	from := time.Now()
	to := from.Add(availabilityWindow)

	slots, err := bot.service.Availability(ctx, from, to)
	if err != nil {
		return err
	}

	return bot.Send(msg.Chat.ID, formatBusySlots(slots))
}

// formatBusySlots превращает список занятых интервалов в текст сообщения.
func formatBusySlots(slots []domain.Slot) string {
	if len(slots) == 0 {
		return "Свободно на ближайшую неделю!"
	}

	var b strings.Builder
	b.WriteString("Занято на ближайшую неделю:\n")
	for _, slot := range slots {
		fmt.Fprintf(&b, "%s – %s\n", slot.StartTime.Format("02.01 15:04"), slot.EndTime.Format("02.01 15:04"))
	}
	return b.String()
}
