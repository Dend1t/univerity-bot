package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/joho/godotenv"
)

func recoverMW(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, u *models.Update) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic: %v\n%s", r, debug.Stack())
			}
		}()
		next(ctx, b, u)
	}
}

func logMW(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, u *models.Update) {
		start := time.Now()
		next(ctx, b, u)
		log.Printf("upd=%d chat=%d за %s", u.ID, chatIDOf(u), time.Since(start))
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func startHealthServer(ctx context.Context, port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Univerity Bot работает!"))
	})
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Println("HTTP сервер запущен на порту", port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("HTTP сервер: %v", err)
		}
	}()
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(c)
	}()
}

func main() {
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Fatal("Токен пустой!")
	}

	startHealthServer(ctx, envOr("PORT", "10000"))

	a := newApp(LoadStore(envOr("DATA_FILE", "data.json")))

	b, err := bot.New(token,
		bot.WithDefaultHandler(a.fallback),
		bot.WithMiddlewares(recoverMW, logMW),
		bot.WithErrorsHandler(func(err error) { log.Printf("[TGBOT] %v", err) }),
		bot.WithCheckInitTimeout(10*time.Second),
		bot.WithWorkers(1),
		bot.WithNotAsyncHandlers(), // последовательно: FSM без гонок
	)
	if err != nil {
		log.Fatal("Ошибка Telegram API:", err)
	}

	me, err := b.GetMe(ctx)
	if err != nil {
		log.Fatal("getMe:", err)
	}
	log.Printf("Бот авторизован: %s", me.Username)

	if _, err := b.SetMyCommands(ctx, &bot.SetMyCommandsParams{
		Commands: []models.BotCommand{
			{Command: "start", Description: "Регистрация / главное меню"},
			{Command: "help", Description: "Помощь"},
			{Command: "hw", Description: "Домашние задания (студент)"},
			{Command: "new", Description: "Создать ДЗ (преподаватель)"},
			{Command: "list", Description: "Мои ДЗ (преподаватель)"},
			{Command: "profile", Description: "Профиль"},
			{Command: "cancel", Description: "Отменить текущий шаг"},
			{Command: "logout", Description: "Выйти"},
		},
	}); err != nil {
		log.Printf("setMyCommands: %v", err)
	}

	// 1) Активная сессия перехватывает ввод раньше остальных.
	b.RegisterHandlerMatchFunc(a.matchSession, a.onStep)

	// 2) Команды (с поддержкой /cmd@botname).
	for _, r := range []struct {
		cmd string
		h   bot.HandlerFunc
	}{
		{"start", a.onStart}, {"help", a.onHelp}, {"cancel", a.onCancel},
		{"new", a.onNewHW}, {"list", a.onMyHW}, {"hw", a.onStudentHW},
		{"profile", a.onProfile}, {"logout", a.onLogout},
	} {
		b.RegisterHandlerMatchFunc(matchCommand(r.cmd), r.h)
	}

	// 3) Кнопки клавиатуры.
	for _, r := range []struct {
		text string
		h    bot.HandlerFunc
	}{
		{btnNewHW, a.onNewHW}, {btnMyHW, a.onMyHW}, {btnHW, a.onStudentHW},
		{btnProfile, a.onProfile}, {btnHelp, a.onHelp}, {btnLogout, a.onLogout},
	} {
		b.RegisterHandler(bot.HandlerTypeMessageText, r.text, bot.MatchTypeExact, r.h)
	}

	// 4) Inline-кнопки.
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "role:", bot.MatchTypePrefix, a.onRole)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "aud:", bot.MatchTypePrefix, a.onAudience)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "hw:", bot.MatchTypePrefix, a.onHWAction)

	// 5) Kliки внутри HTML-виджета (Bot API 10.4).
	b.RegisterHandler(bot.HandlerTypeWidgetQueryData, "d:", bot.MatchTypePrefix, a.onWidgetDone)

	log.Println("Бот запущен, long polling…")
	b.Start(ctx)
	log.Println("Бот остановлен")
}
