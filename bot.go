package main

import (
	"log"
	"os"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/joho/godotenv"
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Ошибка загрузки .env:", err)
	}

	token := os.Getenv("TELEGRAM_APITOKEN")

	if token == "" {
		log.Fatal("Токен пустой!")
	}

	log.Println("Токен загружен, длина:", len(token))

	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		log.Fatal("Ошибка Telegram API:", err)
	}

	bot.Debug = true

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
