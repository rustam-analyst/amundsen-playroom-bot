// Package telegram отвечает за приём апдейтов от Telegram и их
// диспетчеризацию в обработчики команд. Бизнес-логику сам пакет не содержит -
// вызывает booking.Service.
package telegram // объявляем пакет "telegram"

import (
	"context" // для ctx в Run/dispatch/CommandHandler
	"log"     // для log.Printf - логирование ошибок и старта бота

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5" // клиент Telegram Bot API, импортирован под алиасом tgbotapi

	"playroom-bot/internal/booking" // интерфейс Service - бизнес-логика бронирования
)

// Bot - обёртка над tgbotapi.BotAPI с диспетчером команд.
type Bot struct {
	api      *tgbotapi.BotAPI          // клиент Telegram Bot API
	service  booking.Service           // бизнес-логика, вызывается из обработчиков команд
	handlers map[string]CommandHandler // карта "имя команды" -> "функция-обработчик"
}

// CommandHandler обрабатывает одну команду (/start, /book, /my, /cancel, ...).
// ctx позволяет прокидывать таймауты/отмену в вызовы booking.Service.
type CommandHandler func(ctx context.Context, bot *Bot, msg *tgbotapi.Message) error // именованный тип функции - "рецепт" обработчика

// New создаёт бота поверх токена и сервиса бронирования.
func New(token string, debug bool, service booking.Service) (*Bot, error) { // принимает токен, флаг отладки и Service, возвращает (*Bot, error)
	api, err := tgbotapi.NewBotAPI(token) // создаём клиент Telegram API по токену
	if err != nil {                       // если токен неверный или сеть недоступна
		return nil, err // пробрасываем ошибку без дополнительного оборачивания
	}
	api.Debug = debug // включаем подробное логирование запросов к Telegram API, если задано

	bot := &Bot{ // создаём Bot через struct literal
		api:      api,                             // сохраняем клиент API
		service:  service,                         // сохраняем бизнес-логику
		handlers: make(map[string]CommandHandler), // создаём пустую карту обработчиков (map нужно инициализировать через make перед использованием)
	}
	bot.registerHandlers() // заполняем карту handlers конкретными функциями

	return bot, nil // возвращаем готовый Bot без ошибки
}

// registerHandlers связывает текст команды с её обработчиком.
// Реализации самих хендлеров - в handlers.go.
func (bot *Bot) registerHandlers() { // pointer receiver - меняем map внутри существующего Bot
	bot.handlers["start"] = handleStart       // команда /start -> функция handleStart (из handlers.go)
	bot.handlers["help"] = handleStart        // команда /help -> та же функция handleStart
	bot.handlers["book"] = handleBook         // команда /book -> handleBook
	bot.handlers["my"] = handleMyBookings     // команда /my -> handleMyBookings
	bot.handlers["cancel"] = handleCancel     // команда /cancel -> handleCancel
	bot.handlers["free"] = handleAvailability // команда /free -> handleAvailability
}

// Send - вспомогательный метод для отправки сообщения; вынесен, чтобы
// обработчики не работали с tgbotapi.BotAPI напрямую.
func (bot *Bot) Send(chatID int64, text string) error { // принимает ID чата и текст, возвращает error
	_, err := bot.api.Send(tgbotapi.NewMessage(chatID, text)) // формируем сообщение и отправляем через клиент API; результат (Message) не нужен - "_"
	return err                                                // возвращаем ошибку отправки (или nil)
}

// Run запускает long-polling цикл получения апдейтов и блокирует выполнение,
// пока не отменят ctx.
func (bot *Bot) Run(ctx context.Context) error { // блокирующий вызов - выполняется, пока ctx не отменят
	log.Printf("бот запущен: @%s", bot.api.Self.UserName) // логируем имя бота при старте

	u := tgbotapi.NewUpdate(0) // конфигурация long-polling запроса, начиная с апдейта 0
	u.Timeout = 60             // таймаут long-polling запроса в секундах

	updates := bot.api.GetUpdatesChan(u) // получаем канал, в который библиотека будет присылать новые апдейты

	for { // бесконечный цикл обработки событий
		select { // ждём одно из двух событий одновременно
		case <-ctx.Done(): // ctx отменили (Ctrl+C/SIGTERM, см. main.go)
			bot.api.StopReceivingUpdates() // просим библиотеку прекратить long-polling
			return ctx.Err()               // возвращаем причину отмены как ошибку
		case update := <-updates: // пришёл новый апдейт от Telegram
			bot.dispatch(ctx, update) // передаём его в диспетчер
		}
	}
}

// dispatch находит обработчик по команде и вызывает его, логируя ошибку.
func (bot *Bot) dispatch(ctx context.Context, update tgbotapi.Update) {
	if update.Message == nil || !update.Message.IsCommand() { // игнорируем не-команды и пустые сообщения
		return
	}

	handler, ok := bot.handlers[update.Message.Command()] // ищем обработчик в map; ok - нашли ли ключ (comma-ok идиома)
	if !ok {                                              // команда неизвестна
		return // молча игнорируем
	}

	if err := handler(ctx, bot, update.Message); err != nil { // вызываем найденный обработчик
		log.Printf("handler %q: %v", update.Message.Command(), err) // логируем ошибку, если обработчик её вернул
	}
}
