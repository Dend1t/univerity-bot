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

	const (
		botStart = "🚀 Старт"
		botHelp  = "❓ Помощь"

		cmdStart = "/start"
		cmdHelp  = "/help"

		MsgStart = "Привет! Я UniverityBot! 🎓"
		MsgHelp  = "Доступные команды: \n/start - Запустить бота \n/help - Помощь"
	)

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(botStart),
			tgbotapi.NewKeyboardButton(botHelp),
		),
	)
	keyboard.ResizeKeyboard = true

	for update := range updates {
		if update.Message == nil {
			continue
		}

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")
		msg.ReplyMarkup = keyboard

		var action string
		if update.Message.IsCommand() {
			action = update.Message.Command()
		} else {
			switch update.Message.Text {
			case botStart:
				action = cmdStart
			case botHelp:
				action = cmdHelp
			default:
				continue
			}
		}

		if action == cmdStart {
			msg.Text = MsgStart
		} else if action == cmdHelp {
			msg.Text = MsgHelp
		} else {
			msg.Text = "Неизвестная команда. Используй /help"
		}

		if _, err := bot.Send(msg); err != nil {
			log.Println("Ошибка отправки:", err)
		}
	}
}
