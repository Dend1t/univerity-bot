package main

import (
	"log"
	"net/http"
	"os"
	"strings"

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
		botStart   = "🚀 Старт"
		botHelp    = "❓ Помощь"
		botWriteHW = "✏️ Записать"

		cmdStart   = "start"
		cmdHelp    = "help"
		cmdWriteHW = "writehw"

		MsgStart = "Привет! Я UniverityBot! 🎓"
		MsgHelp  = "Доступные команды:\n/start - Запустить бота\n/help - Помощь\n/writehw - Записать задание"
	)

	keyboard := tgbotapi.NewReplyKeyboard(
		tgbotapi.NewKeyboardButtonRow(
			tgbotapi.NewKeyboardButton(botStart),
			tgbotapi.NewKeyboardButton(botHelp),
			tgbotapi.NewKeyboardButton(botWriteHW),
		),
	)
	keyboard.ResizeKeyboard = true

	awaitingWrite := make(map[int64]bool)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chatID := update.Message.Chat.ID
		text := strings.TrimSpace(update.Message.Text)

		msg := tgbotapi.NewMessage(chatID, "")
		msg.ReplyMarkup = keyboard

		if awaitingWrite[chatID] {
			if update.Message.IsCommand() && update.Message.Command() == "cancel" {
				awaitingWrite[chatID] = false
				msg.Text = "❌ Запись отменена."
			} else {
				awaitingWrite[chatID] = false

				if text == "" {
					msg.Text = "❌ Отправь текстовое сообщение."
				} else {
					msg.Text = "✅ Задание получено!\n\n" + text
				}
			}

			if _, err := bot.Send(msg); err != nil {
				log.Println("Ошибка отправки:", err)
			}
			continue
		}

		var action string

		if update.Message.IsCommand() {
			action = update.Message.Command()
		} else {
			switch text {
			case botStart:
				action = cmdStart
			case botHelp:
				action = cmdHelp
			case botWriteHW:
				action = cmdWriteHW
			default:
				continue
			}
		}

		switch action {
		case cmdStart:
			msg.Text = MsgStart

		case cmdHelp:
			msg.Text = MsgHelp

		case cmdWriteHW:
			awaitingWrite[chatID] = true
			msg.Text = "✏️ Отправь текст задания.\n\nДля отмены напиши /cancel."

		default:
			msg.Text = "Неизвестная команда. Используй /help"
		}

		if _, err := bot.Send(msg); err != nil {
			log.Println("Ошибка отправки:", err)
		}
	}
}
