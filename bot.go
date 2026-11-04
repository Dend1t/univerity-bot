
package main

import (
	"log"
	"net/http"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "10000"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Univerity Bot работает!"))
	})

	go func() {
		log.Println("HTTP сервер запущен на порту", port)
		if err := http.ListenAndServe(":"+port, nil); err != nil {
			log.Fatal("Ошибка HTTP сервера:", err)
		}
	}()

	token := os.Getenv("TELEGRAM_APITOKEN")

	if token == "" {
		log.Fatal("Токен пустой!")
	}

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatal("Ошибка Telegram API:", err)
	}

	bot.Debug = false

	log.Printf("Бот авторизован: %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 30

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		if !update.Message.IsCommand() {
			continue
		}

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")

		switch update.Message.Command() {
		case "start":
			msg.Text = "Привет! Я Univerity Bot!"

		case "help":
			msg.Text = "Доступные команды:\n/start - Запустить бота\n/help - Помощь"

		default:
			msg.Text = "Неизвестная команда. Используй /help"
		}

		if _, err := bot.Send(msg); err != nil {
			log.Println("Ошибка отправки:", err)
		}
	}
}
