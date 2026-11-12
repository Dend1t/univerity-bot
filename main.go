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

func privateOnly(next bot.HandlerFunc) bot.HandlerFunc {
	return func(ctx context.Context, b *bot.Bot, u *models.Update) {
		chat := u.EffectiveChat()
		if chat != nil && chat.Type != "private" {
			if u.Message != nil {
				_, _ = b.SendMessage(ctx, &bot.SendMessageParams{
					ChatID: chat.ID,
					Text:   "🔒 Я работаю только в личных сообщениях.",
				})
			}
			return
		}
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
	if err := godotenv.Load(); err != nil {
		log.Println(".env не найден, читаю переменные окружения")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	token := os.Getenv("TELEGRAM_APITOKEN")
	if token == "" {
		log.Fatal("Токен пустой!")
	}

	startHealthServer(ctx, envOr("PORT", "10000"))

	a := newApp(LoadStore(os.Getenv("DATA_FILE")))

	b, err := bot.New(token,
		bot.WithDefaultHandler(a.fallback),
		bot.WithMiddlewares(recoverMW, privateOnly, logMW),
		bot.WithErrorsHandler(func(err error) { log.Printf("[TGBOT] %v", err) }),
		bot.WithCheckInitTimeout(10*time.Second),
		bot.WithWorkers(1),
		bot.WithNotAsyncHandlers(),
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
			{Command: "start", Description: "Регистрация / меню"},
			{Command: "help", Description: "Помощь"},
			{Command: "hw", Description: "Домашние задания"},
			{Command: "new", Description: "Создать ДЗ"},
			{Command: "list", Description: "Мои ДЗ и сдача"},
			{Command: "stats", Description: "Статистика"},
			{Command: "profile", Description: "Профиль"},
			{Command: "cancel", Description: "Отменить шаг"},
			{Command: "logout", Description: "Выйти"},
		},
	}); err != nil {
		log.Printf("setMyCommands: %v", err)
	}

	b.RegisterHandlerMatchFunc(a.matchSession, a.onStep)

	for _, r := range []struct {
		cmd string
		h   bot.HandlerFunc
	}{
		{"start", a.onStart}, {"help", a.onHelp}, {"cancel", a.onCancel},
		{"new", a.onNewHW}, {"list", a.onMyHW}, {"hw", a.onStudentHW},
		{"stats", a.onStats}, {"profile", a.onProfile}, {"logout", a.onLogout},
	} {
		b.RegisterHandlerMatchFunc(matchCommand(r.cmd), r.h)
	}

	for _, r := range []struct {
		text string
		h    bot.HandlerFunc
	}{
		{btnNewHW, a.onNewHW}, {btnMyHW, a.onMyHW}, {btnHW, a.onStudentHW},
		{btnStats, a.onStats}, {btnProfile, a.onProfile}, {btnHelp, a.onHelp}, {btnLogout, a.onLogout},
	} {
		b.RegisterHandler(bot.HandlerTypeMessageText, r.text, bot.MatchTypeExact, r.h)
	}

	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "role:", bot.MatchTypePrefix, a.onRole)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "prog:", bot.MatchTypePrefix, a.onProgram)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "subj:", bot.MatchTypePrefix, a.onSubject)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "aud:", bot.MatchTypePrefix, a.onAudience)
	b.RegisterHandler(bot.HandlerTypeCallbackQueryData, "hw:", bot.MatchTypePrefix, a.onHWAction)

	b.RegisterHandlerMatchFunc(a.matchWidget, a.onWidget)

	log.Println("Бот запущен, long polling…")
	b.Start(ctx)
	log.Println("Бот остановлен")
}
