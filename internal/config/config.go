// Package config отвечает за загрузку настроек приложения из переменных
// окружения (и .env файла при локальной разработке).
package config // объявляем пакет "config" - имя, под которым его импортируют

import (
	"errors" // для создания ошибок через errors.New
	"os"     // для чтения переменных окружения через os.Getenv

	"github.com/joho/godotenv" // сторонняя библиотека - загрузка .env файла
)

// Config - настройки, необходимые для запуска бота.
type Config struct { // структура из трёх полей
	// BotToken - токен, выданный @BotFather.
	BotToken string // токен Telegram-бота
	// DBPath - путь к файлу SQLite базы данных.
	DBPath string // путь к файлу БД на диске
	// Debug включает подробное логирование запросов к Telegram API.
	Debug bool // флаг отладочного режима
}

// Load читает .env (если он есть - отсутствие не является ошибкой)
// и переменные окружения, возвращая готовый Config.
func Load() (Config, error) { // без аргументов, возвращает (Config, error)
	_ = godotenv.Load() // подгружаем .env в окружение процесса; ошибку игнорируем - файла может не быть

	cfg := Config{ // создаём Config через struct literal
		BotToken: os.Getenv("BOT_TOKEN"),       // читаем переменную окружения BOT_TOKEN
		DBPath:   os.Getenv("DB_PATH"),         // читаем переменную окружения DB_PATH
		Debug:    os.Getenv("DEBUG") == "true", // true, если DEBUG равно строке "true"
	}

	if cfg.BotToken == "" { // токен обязателен - без него бот не сможет подключиться
		return Config{}, errors.New("config: BOT_TOKEN не задан") // возвращаем пустой Config и ошибку
	}
	if cfg.DBPath == "" { // путь к БД необязателен - есть разумное значение по умолчанию
		cfg.DBPath = "playroom.db" // подставляем дефолтный путь
	}

	return cfg, nil // успех: заполненный Config и nil вместо ошибки
}
