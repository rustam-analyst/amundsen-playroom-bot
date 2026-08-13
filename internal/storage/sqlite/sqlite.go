// Package sqlite - реализация booking.Repository поверх SQLite.
package sqlite // объявляем пакет "sqlite"

import (
	"context"      // для ctx во всех методах ниже
	"database/sql" // стандартный, независимый от конкретной БД интерфейс SQL
	"errors"       // для errors.Is - отличить sql.ErrNoRows от прочих ошибок
	"fmt"          // для fmt.Errorf - создание ошибок с форматированием
	"time"         // для типа time.Time в ListBusySlots

	_ "modernc.org/sqlite" // анонимный импорт - регистрирует драйвер "sqlite" в database/sql

	"playroom-bot/internal/booking" // интерфейс Repository, который этот файл реализует
	"playroom-bot/internal/domain"  // типы Booking и Slot
)

// schema - SQL, создающий структуру БД; выполняется один раз при каждом Open
// (IF NOT EXISTS делает это безопасным при повторных запусках).
//
// resource заведена заранее, со значением по умолчанию 'all' (вся комната) -
// чтобы потом, когда появится бронирование отдельных элементов (PS_0, PS_1,
// kicker, movie, ...), не пришлось делать миграцию существующей БД. См. TODO
// у domain.Resource, в Create и в ListBusySlots.
const schema = `
CREATE TABLE IF NOT EXISTS bookings ( -- таблица "bookings", создаётся, если её ещё нет
	id         INTEGER PRIMARY KEY AUTOINCREMENT, -- первичный ключ, растёт автоматически
	user_id    INTEGER NOT NULL,                  -- Telegram id владельца брони
	user_name  TEXT NOT NULL,                      -- отображаемое имя владельца
	resource   TEXT NOT NULL DEFAULT 'all',        -- что забронировано - пока всегда 'all'
	start_time DATETIME NOT NULL,                  -- начало брони
	end_time   DATETIME NOT NULL,                  -- конец брони
	status     TEXT NOT NULL DEFAULT 'active',     -- 'active' или 'cancelled'
	created_at DATETIME NOT NULL                   -- когда бронь была создана
);
CREATE INDEX IF NOT EXISTS idx_bookings_time ON bookings (start_time, end_time); -- ускоряет поиск пересечений по времени
CREATE INDEX IF NOT EXISTS idx_bookings_user ON bookings (user_id); -- ускоряет выборку броней конкретного пользователя
`

// Store - подключение к SQLite, реализующее booking.Repository.
type Store struct {
	db *sql.DB // единственное поле - пул соединений к БД (потокобезопасен, не одно физическое соединение)
}

// Open открывает файл БД по пути path, создаёт таблицы при первом запуске
// и возвращает готовый Store.
func Open(path string) (*Store, error) { // принимает путь к файлу, возвращает (*Store, error)
	// _time_format=sqlite: без этого параметра драйвер пишет time.Time через
	// Go-шный String(), который сам же не умеет распарсить обратно при чтении
	// DATETIME-колонок - явно фиксируем совместимый формат на запись и чтение.
	dsn := path + "?_time_format=sqlite"
	conn, err := sql.Open("sqlite", dsn) // открыть/создать файл БД через зарегистрированный драйвер "sqlite"; conn - локальная переменная, НЕ поле Store
	if err != nil {                      // sql.Open редко возвращает ошибку сразу, но проверяем на всякий случай
		return nil, fmt.Errorf("sqlite: open %s: %w", path, err) // оборачиваем ошибку контекстом, %w сохраняет исходную err
	}

	if _, err := conn.Exec(schema); err != nil { // выполняем SQL создания таблиц; первое значение (Result) не нужно - "_"
		conn.Close()                                            // если схема не применилась - закрываем уже открытое соединение
		return nil, fmt.Errorf("sqlite: apply schema: %w", err) // и возвращаем обёрнутую ошибку
	}

	return &Store{db: conn}, nil // всё ок - создаём Store, кладём conn в поле db, возвращаем указатель на Store
}

// Close закрывает соединение с БД.
func (store *Store) Close() error { // pointer receiver - работаем с тем же самым Store, что открыли
	return store.db.Close() // делегируем закрытие соединения самому *sql.DB
}

// Проверка на этапе компиляции: *Store обязан реализовывать все методы booking.Repository.
var _ booking.Repository = (*Store)(nil)

func (store *Store) Create(ctx context.Context, bk domain.Booking) (domain.Booking, error) {
	// Status и CreatedAt - свойства самого момента создания, а не входные
	// данные от вызывающего кода: новая бронь всегда активна и всегда
	// создаётся "сейчас" на сервере, а не в момент, который мог прислать клиент.
	// bk - копия (Booking передаётся по значению), так что эти присваивания
	// не влияют на bk у вызывающего кода.
	bk.Status = domain.StatusActive
	bk.CreatedAt = time.Now()

	result, err := store.db.ExecContext(ctx,
		`INSERT INTO bookings (user_id, user_name, resource, start_time, end_time, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		bk.UserID, bk.UserName, bk.Resource, bk.StartTime, bk.EndTime, bk.Status, bk.CreatedAt)
	if err != nil {
		return domain.Booking{}, fmt.Errorf("sqlite: create booking: %w", err)
	}

	id, err := result.LastInsertId() // id, присвоенный SQLite через AUTOINCREMENT
	if err != nil {
		return domain.Booking{}, fmt.Errorf("sqlite: create booking: get id: %w", err)
	}
	bk.ID = id

	return bk, nil
}

func (store *Store) GetByID(ctx context.Context, id int64) (domain.Booking, error) {
	var bk domain.Booking
	err := store.db.QueryRowContext(ctx,
		`SELECT id, user_id, user_name, resource, start_time, end_time, status, created_at
		 FROM bookings
		 WHERE id = ?`,
		id).Scan(&bk.ID, &bk.UserID, &bk.UserName, &bk.Resource, &bk.StartTime, &bk.EndTime, &bk.Status, &bk.CreatedAt)

	if errors.Is(err, sql.ErrNoRows) { // строки нет вообще - не путать с прочими ошибками ниже
		return domain.Booking{}, booking.ErrNotFound
	}
	if err != nil {
		return domain.Booking{}, fmt.Errorf("sqlite: get by id: %w", err)
	}

	return bk, nil
}

func (store *Store) ListByUser(ctx context.Context, userID int64) ([]domain.Booking, error) {
	// В отличие от ListBusySlots, тут отдаём ВСЕ поля - вызывающий (Service)
	// гарантирует, что этот метод используется только для показа брони её
	// же владельцу, а не всем подряд.
	rows, err := store.db.QueryContext(ctx,
		`SELECT id, user_id, user_name, resource, start_time, end_time, status, created_at
		 FROM bookings
		 WHERE user_id = ? AND status = 'active'
		 ORDER BY start_time`,
		userID)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list by user: %w", err)
	}
	defer rows.Close()

	var bookings []domain.Booking
	for rows.Next() {
		var bk domain.Booking
		if err := rows.Scan(&bk.ID, &bk.UserID, &bk.UserName, &bk.Resource, &bk.StartTime, &bk.EndTime, &bk.Status, &bk.CreatedAt); err != nil {
			return nil, fmt.Errorf("sqlite: scan booking: %w", err)
		}
		bookings = append(bookings, bk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sqlite: list by user: %w", err)
	}

	return bookings, nil
}

func (store *Store) ListBusySlots(ctx context.Context, from, to time.Time) ([]domain.Slot, error) {
	// SELECT только start_time/end_time - без user_id/user_name, см. комментарий в booking.Repository.
	// Пересечение интервалов [start_time, end_time) и [from, to):
	// start_time < to И end_time > from.
	// TODO: колонка resource уже есть в схеме, но здесь не используется -
	// пока все брони резервируют комнату целиком ('all'), запрос корректно
	// считает занятость без учёта resource. Когда появится бронирование
	// отдельных элементов - добавить параметр resource и "AND resource = ?".
	rows, err := store.db.QueryContext(ctx,
		`SELECT start_time, end_time FROM bookings
		 WHERE status = 'active' AND start_time < ? AND end_time > ?
		 ORDER BY start_time`,
		to, from)
	if err != nil {
		return nil, fmt.Errorf("sqlite: list busy slots: %w", err)
	}
	defer rows.Close()

	var slots []domain.Slot
	for rows.Next() {
		var slot domain.Slot
		if err := rows.Scan(&slot.StartTime, &slot.EndTime); err != nil {
			return nil, fmt.Errorf("sqlite: scan slot: %w", err)
		}
		slots = append(slots, slot)
	}
	if err := rows.Err(); err != nil { // ошибка, возникшая во время итерации (не в Scan)
		return nil, fmt.Errorf("sqlite: list busy slots: %w", err)
	}

	return slots, nil
}

func (store *Store) Cancel(ctx context.Context, id int64) error {
	// Проверка владельца - забота вызывающего (Service), сюда доходит только
	// после того, как GetByID уже подтвердил и существование брони, и то, что
	// она принадлежит тому, кто её отменяет.
	if _, err := store.db.ExecContext(ctx, `UPDATE bookings SET status = 'cancelled' WHERE id = ?`, id); err != nil {
		return fmt.Errorf("sqlite: cancel: %w", err)
	}
	return nil
}
