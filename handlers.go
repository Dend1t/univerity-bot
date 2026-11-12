package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strconv"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const (
	btnNewHW   = "➕ Новое ДЗ"
	btnMyHW    = "📋 Мои ДЗ"
	btnHW      = "📚 Домашние задания"
	btnProfile = "👤 Профиль"
	btnHelp    = "❓ Помощь"
	btnLogout  = "🚪 Выйти"
)

type app struct {
	store *Store
	sess  *Sessions
}

func newApp(s *Store) *app { return &app{store: s, sess: NewSessions()} }

// ------------------------------------------------------- утилиты

func esc(s string) string { return html.EscapeString(s) }

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func chatIDOf(u *models.Update) int64 {
	if c := u.EffectiveChat(); c != nil {
		return c.ID
	}
	if usr := u.EffectiveUser(); usr != nil {
		return usr.ID
	}
	return 0
}

func commandOf(m *models.Message) string {
	if m == nil {
		return ""
	}
	for _, e := range m.Entities {
		if e.Type != models.MessageEntityTypeBotCommand || e.Offset != 0 {
			continue
		}
		if e.Length <= 1 || e.Length > len(m.Text) {
			return ""
		}
		cmd := m.Text[1:e.Length]
		if i := strings.IndexByte(cmd, '@'); i >= 0 {
			cmd = cmd[:i]
		}
		return strings.ToLower(cmd)
	}
	return ""
}

func matchCommand(name string) bot.MatchFunc {
	return func(u *models.Update) bool { return commandOf(u.Message) == name }
}

func kb(rows ...[]models.KeyboardButton) *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true, IsPersistent: true}
}

func kbFor(u User) models.ReplyMarkup {
	switch u.Role {
	case RoleTeacher:
		return kb(
			[]models.KeyboardButton{{Text: btnNewHW}, {Text: btnMyHW}},
			[]models.KeyboardButton{{Text: btnProfile}, {Text: btnHelp}},
			[]models.KeyboardButton{{Text: btnLogout}},
		)
	case RoleStudent:
		return kb(
			[]models.KeyboardButton{{Text: btnHW}},
			[]models.KeyboardButton{{Text: btnProfile}, {Text: btnHelp}},
			[]models.KeyboardButton{{Text: btnLogout}},
		)
	}
	return &models.ReplyKeyboardRemove{RemoveKeyboard: true}
}

func hwKB(id int) *models.InlineKeyboardMarkup {
	return &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
		{Text: "✏️ Изменить", CallbackData: fmt.Sprintf("hw:edit:%d", id)},
		{Text: "🗑 Удалить", CallbackData: fmt.Sprintf("hw:del:%d", id)},
	}}}
}

func hwCard(h Homework, done bool) string {
	mark := ""
	if done {
		mark = "\n\n✅ <b>Выполнено</b>"
	}
	return fmt.Sprintf("<b>%s</b> · ДЗ #%d\n<i>%s</i>\n\n%s\n\n🕒 %s%s",
		esc(h.Subject), h.ID, esc(h.Audience()), esc(h.Text),
		h.CreatedAt.Format("02.01.2006 15:04"), mark)
}

func whoami(u User) string {
	switch u.Role {
	case RoleTeacher:
		t, _ := teacherByID(u.TeacherID)
		return fmt.Sprintf("👨‍🏫 <b>%s</b>\nПредмет: %s\nКод: %s", esc(t.Name), esc(t.Subject), esc(t.ID))
	case RoleStudent:
		return "🎓 <b>Студент</b>\n" + esc(u.Student.String())
	}
	return "Гость"
}

func (a *app) send(ctx context.Context, b *bot.Bot, chatID int64, text string, markup models.ReplyMarkup) {
	_, err := b.SendMessage(ctx, &bot.SendMessageParams{
		ChatID: chatID, Text: text, ParseMode: "HTML", ReplyMarkup: markup,
	})
	if err == nil {
		return
	}
	var tooMany *bot.TooManyRequestsError
	switch {
	case errors.Is(err, bot.ErrorForbidden):
		log.Printf("чат %d: бот заблокирован", chatID)
		a.sess.Clear(chatID)
	case errors.As(err, &tooMany):
		log.Printf("чат %d: 429, retry_after=%d", chatID, tooMany.RetryAfter)
	default:
		log.Printf("отправка в %d: %v", chatID, err)
	}
}

func answer(ctx context.Context, b *bot.Bot, id, text string) {
	if _, err := b.AnswerCallbackQuery(ctx, &bot.AnswerCallbackQueryParams{
		CallbackQueryID: id, Text: text,
	}); err != nil {
		log.Printf("answerCallback: %v", err)
	}
}

// require — проверка регистрации и роли.
func (a *app) require(ctx context.Context, b *bot.Bot, chatID int64, role Role) (User, bool) {
	u := a.store.User(chatID)
	if u.Role == RoleNone {
		a.send(ctx, b, chatID, "Сначала зарегистрируйся: /start", nil)
		return u, false
	}
	if role != RoleNone && u.Role != role {
		a.send(ctx, b, chatID, "🚫 Эта команда доступна только другой роли.", kbFor(u))
		return u, false
	}
	return u, true
}

// ------------------------------------------------------- регистрация

func (a *app) onStart(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := u.Message.Chat.ID
	a.sess.Clear(chatID)
	usr := a.store.User(chatID)
	if usr.Role != RoleNone {
		a.send(ctx, b, chatID, "С возвращением!\n\n"+whoami(usr), kbFor(usr))
		return
	}
	a.send(ctx, b, chatID,
		"Привет! Я <b>UniverityBot</b> 🎓\n\nКто ты?",
		&models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "👨‍🏫 Преподаватель", CallbackData: "role:teacher"},
			{Text: "🎓 Студент", CallbackData: "role:student"},
		}}})
}

func (a *app) onRole(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	answer(ctx, b, q.ID, "")

	if a.store.User(chatID).Role != RoleNone {
		a.send(ctx, b, chatID, "Ты уже зарегистрирован. Выйти: "+btnLogout, nil)
		return
	}

	switch q.Data {
	case "role:teacher":
		a.sess.Set(chatID, Session{Step: stepTeacherPass})
		a.send(ctx, b, chatID,
			"🔑 Введи пароль, который выдал вуз.\nФормат: <b>FCS00001</b>\n\n<i>Тестовый стенд: FCS00001…FCS00005</i>\n\nОтмена — /cancel", nil)
	case "role:student":
		a.sess.Set(chatID, Session{Step: stepFaculty})
		a.send(ctx, b, chatID, "🏛 Укажи свой <b>факультет</b> (например: ФКН).\n\nОтмена — /cancel", nil)
	}
}

func (a *app) matchSession(u *models.Update) bool {
	return u.Message != nil && a.sess.Get(u.Message.Chat.ID).Step != stepNone
}

// onStep — единый обработчик всех пошаговых сценариев.
func (a *app) onStep(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := u.Message.Chat.ID
	s := a.sess.Get(chatID)
	text := strings.TrimSpace(u.Message.Text)

	switch commandOf(u.Message) {
	case "cancel":
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "❌ Действие отменено.", kbFor(a.store.User(chatID)))
		return
	case "start":
		a.sess.Clear(chatID)
		a.onStart(ctx, b, u)
		return
	}

	switch s.Step {

	case stepTeacherPass:
		t, ok := teacherByPassword[strings.ToUpper(text)]
		if !ok {
			a.send(ctx, b, chatID, "❌ Пароль не найден. Проверь код (пример: <b>FCS00003</b>) или /cancel.", nil)
			return
		}
		usr := User{ChatID: chatID, Role: RoleTeacher, TeacherID: t.ID}
		a.store.PutUser(usr)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Вход выполнен!\n\n"+whoami(usr), kbFor(usr))

	case stepFaculty:
		if text == "" {
			a.send(ctx, b, chatID, "Напиши название факультета текстом.", nil)
			return
		}
		s.Reg.Faculty, s.Step = trunc(text, 64), stepCourse
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Факультет: <b>"+esc(s.Reg.Faculty)+"</b>\n\nТеперь <b>курс</b> — число от 1 до 6.", nil)

	case stepCourse:
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 6 {
			a.send(ctx, b, chatID, "❌ Курс — число от 1 до 6.", nil)
			return
		}
		s.Reg.Course, s.Step = n, stepGroup
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, fmt.Sprintf("Курс: <b>%d</b>\n\nТеперь <b>группа</b> (например: ИВТ-231).", n), nil)

	case stepGroup:
		if text == "" {
			a.send(ctx, b, chatID, "Напиши название группы текстом.", nil)
			return
		}
		s.Reg.Group = trunc(text, 32)
		usr := User{ChatID: chatID, Role: RoleStudent, Student: s.Reg}
		a.store.PutUser(usr)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Регистрация завершена!\n\n"+whoami(usr)+"\n\nЖми «"+btnHW+"».", kbFor(usr))

	case stepHWFac:
		if text == "" {
			a.send(ctx, b, chatID, "Напиши факультет текстом.", nil)
			return
		}
		s.Draft.Faculty, s.Step = trunc(text, 64), stepHWCourse
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Факультет: <b>"+esc(s.Draft.Faculty)+"</b>\n\nКурс (1–6)?", nil)

	case stepHWCourse:
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 6 {
			a.send(ctx, b, chatID, "❌ Курс — число от 1 до 6.", nil)
			return
		}
		s.Draft.Course, s.Step = n, stepHWGroup
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Группа?", nil)

	case stepHWGroup:
		if text == "" {
			a.send(ctx, b, chatID, "Напиши группу текстом.", nil)
			return
		}
		s.Draft.Group, s.Step = trunc(text, 32), stepHWText
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Аудитория: <b>"+esc(s.Draft.Audience())+"</b>\n\nОтправь <b>текст задания</b>.", nil)

	case stepHWText:
		if text == "" {
			a.send(ctx, b, chatID, "❌ Пустое задание. Пришли текст или /cancel.", nil)
			return
		}
		usr := a.store.User(chatID)
		if usr.Role != RoleTeacher {
			a.sess.Clear(chatID)
			return
		}
		t, _ := teacherByID(usr.TeacherID)
		h := s.Draft
		h.TeacherID, h.Subject, h.Text = t.ID, t.Subject, trunc(text, 3000)
		h = a.store.AddHW(h)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Задание опубликовано!\n\n"+hwCard(h, false), kbFor(usr))

	case stepHWEdit:
		if text == "" {
			a.send(ctx, b, chatID, "❌ Пустой текст. Пришли новый вариант или /cancel.", nil)
			return
		}
		if !a.store.EditText(s.HWID, trunc(text, 3000)) {
			a.sess.Clear(chatID)
			a.send(ctx, b, chatID, "Задание уже удалено.", kbFor(a.store.User(chatID)))
			return
		}
		h, _ := a.store.HWByID(s.HWID)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✏️ Обновлено!\n\n"+hwCard(h, false), kbFor(a.store.User(chatID)))
	}
}

// ------------------------------------------------------- преподаватель

func (a *app) onNewHW(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	if _, ok := a.require(ctx, b, chatID, RoleTeacher); !ok {
		return
	}
	a.sess.Clear(chatID)
	a.send(ctx, b, chatID, "Кому назначить задание?", &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "👥 Всем студентам", CallbackData: "aud:all"}},
			{{Text: "🎯 Факультет / курс / группа", CallbackData: "aud:pick"}},
		}})
}

func (a *app) onAudience(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	answer(ctx, b, q.ID, "")
	if _, ok := a.require(ctx, b, chatID, RoleTeacher); !ok {
		return
	}
	switch q.Data {
	case "aud:all":
		a.sess.Set(chatID, Session{Step: stepHWText})
		a.send(ctx, b, chatID, "Аудитория: <b>все группы</b>\n\nОтправь <b>текст задания</b>.\nОтмена — /cancel", nil)
	case "aud:pick":
		a.sess.Set(chatID, Session{Step: stepHWFac})
		a.send(ctx, b, chatID, "Укажи <b>факультет</b>.\nОтмена — /cancel", nil)
	}
}

func (a *app) onMyHW(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	usr, ok := a.require(ctx, b, chatID, RoleTeacher)
	if !ok {
		return
	}
	list := a.store.HWOfTeacher(usr.TeacherID)
	if len(list) == 0 {
		a.send(ctx, b, chatID, "У тебя пока нет заданий. Нажми «"+btnNewHW+"».", kbFor(usr))
		return
	}
	a.send(ctx, b, chatID, fmt.Sprintf("📋 Твои задания: <b>%d</b>", len(list)), kbFor(usr))
	for i, h := range list {
		if i == 10 {
			a.send(ctx, b, chatID, "…показаны последние 10.", nil)
			break
		}
		a.send(ctx, b, chatID, hwCard(h, false), hwKB(h.ID))
	}
}

func (a *app) onHWAction(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	parts := strings.Split(q.Data, ":") // hw:<action>:<id>
	if len(parts) != 3 {
		answer(ctx, b, q.ID, "")
		return
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil {
		answer(ctx, b, q.ID, "Некорректный ID")
		return
	}
	usr, ok := a.require(ctx, b, chatID, RoleTeacher)
	if !ok {
		answer(ctx, b, q.ID, "")
		return
	}
	h, found := a.store.HWByID(id)
	if !found {
		answer(ctx, b, q.ID, "Задание уже удалено")
		return
	}
	if h.TeacherID != usr.TeacherID {
		answer(ctx, b, q.ID, "Это задание другого преподавателя")
		return
	}

	msgID := 0
	if q.Message.Message != nil {
		msgID = q.Message.Message.ID
	}

	switch parts[1] {
	case "edit":
		a.sess.Set(chatID, Session{Step: stepHWEdit, HWID: id})
		answer(ctx, b, q.ID, "Жду новый текст")
		a.send(ctx, b, chatID, fmt.Sprintf("✏️ Пришли новый текст для ДЗ #%d.\nОтмена — /cancel", id), nil)

	case "del":
		answer(ctx, b, q.ID, "")
		confirm := &models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{{
			{Text: "✅ Да, удалить", CallbackData: fmt.Sprintf("hw:yes:%d", id)},
			{Text: "↩️ Отмена", CallbackData: fmt.Sprintf("hw:no:%d", id)},
		}}}
		if msgID != 0 {
			_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
				ChatID: chatID, MessageID: msgID, ReplyMarkup: confirm,
			})
		} else {
			a.send(ctx, b, chatID, fmt.Sprintf("Удалить ДЗ #%d?", id), confirm)
		}

	case "yes":
		a.store.DeleteHW(id)
		a.sess.Clear(chatID)
		answer(ctx, b, q.ID, "Удалено")
		text := fmt.Sprintf("🗑 Задание <b>#%d</b> (%s) удалено.", id, esc(h.Subject))
		if msgID != 0 {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{
				ChatID: chatID, MessageID: msgID, Text: text, ParseMode: "HTML",
			})
		} else {
			a.send(ctx, b, chatID, text, kbFor(usr))
		}

	case "no":
		answer(ctx, b, q.ID, "Отменено")
		if msgID != 0 {
			_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{
				ChatID: chatID, MessageID: msgID, ReplyMarkup: hwKB(id),
			})
		}
	}
}

// ------------------------------------------------------- студент

func (a *app) onStudentHW(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	usr, ok := a.require(ctx, b, chatID, RoleStudent)
	if !ok {
		return
	}
	list := a.store.HWForStudent(usr.Student)
	if len(list) == 0 {
		a.send(ctx, b, chatID, "🎉 Для твоей группы заданий пока нет.", kbFor(usr))
		return
	}

	// Bot API 10.4: интерактивный HTML-виджет (только личка с ботом).
	if u.Message != nil && u.Message.Chat.Type == "private" {
		items := make([]widgetItem, 0, 8)
		for i, h := range list {
			if i == 8 {
				break
			}
			items = append(items, widgetItem{
				ID: h.ID, Subject: h.Subject,
				Meta: fmt.Sprintf("#%d · %s · %s", h.ID, h.Audience(), h.CreatedAt.Format("02.01")),
				Text: trunc(h.Text, 160),
			})
		}
		if doc, err := renderWidget(items); err == nil {
			_, err := b.SendWidget(ctx, &bot.SendWidgetParams{
				ChatID:       chatID,
				HTML:         doc,
				Height:       480,
				AllowQueries: true,
				ChatData:     encodeState(usr.Done),
				Caption:      "📚 Твои задания — отмечай выполненные прямо здесь",
			})
			if err == nil {
				return
			}
			log.Printf("виджет недоступен (%v) — текстовый режим", err)
		}
	}

	// Fallback: обычный текст.
	done := map[int]bool{}
	for _, id := range usr.Done {
		done[id] = true
	}
	a.send(ctx, b, chatID, fmt.Sprintf("📚 Заданий для тебя: <b>%d</b>", len(list)), kbFor(usr))
	for i, h := range list {
		if i == 15 {
			a.send(ctx, b, chatID, "…показаны последние 15.", nil)
			break
		}
		a.send(ctx, b, chatID, hwCard(h, done[h.ID]), nil)
	}
}

// onWidgetDone — клик внутри виджета: TelegramWidget.request("d:<id>").
func (a *app) onWidgetDone(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.WidgetQuery
	chatID := q.From.ID
	if q.Chat != nil {
		chatID = q.Chat.ID
	}

	state := `{"d":[]}`
	if id, err := strconv.Atoi(strings.TrimPrefix(q.Data, "d:")); err == nil {
		if _, ok := a.store.HWByID(id); ok {
			state = encodeState(a.store.ToggleDone(chatID, id))
		}
	}

	// Ответ обязан уйти за 15 секунд, иначе виджет получит TIMEOUT.
	if _, err := b.AnswerWidgetQuery(ctx, &bot.AnswerWidgetQueryParams{
		WidgetQueryID: q.ID, Data: state,
	}); err != nil {
		log.Printf("answerWidgetQuery: %v", err)
	}

	// Фиксируем состояние в сообщении — переживёт перезапуск клиента.
	if q.MessageID != 0 {
		if _, err := b.EditMessageWidgetData(ctx, &bot.EditMessageWidgetDataParams{
			ChatID: chatID, MessageID: q.MessageID, ChatData: &state,
		}); err != nil {
			log.Printf("editMessageWidgetData: %v", err)
		}
	}
}

// ------------------------------------------------------- общее

func (a *app) onProfile(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	usr, ok := a.require(ctx, b, chatID, RoleNone)
	if !ok {
		return
	}
	extra := ""
	if usr.Role == RoleStudent {
		extra = fmt.Sprintf("\nОтмечено выполненных: <b>%d</b>", len(usr.Done))
	} else {
		extra = fmt.Sprintf("\nОпубликовано заданий: <b>%d</b>", len(a.store.HWOfTeacher(usr.TeacherID)))
	}
	a.send(ctx, b, chatID, whoami(usr)+extra, kbFor(usr))
}

func (a *app) onLogout(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	a.store.DropUser(chatID)
	a.sess.Clear(chatID)
	a.send(ctx, b, chatID, "🚪 Ты вышел. Профиль удалён.", kbFor(User{}))
	a.onStart(ctx, b, &models.Update{Message: &models.Message{Chat: models.Chat{ID: chatID}}})
}

func (a *app) onCancel(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	a.sess.Clear(chatID)
	a.send(ctx, b, chatID, "Сейчас нечего отменять.", kbFor(a.store.User(chatID)))
}

func (a *app) onHelp(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	usr := a.store.User(chatID)
	text := "❓ <b>Помощь</b>\n\n/start — начать / сменить роль\n/help — эта справка\n/cancel — отменить текущий шаг\n"
	switch usr.Role {
	case RoleTeacher:
		text += "\n<b>Преподаватель:</b>\n/new — создать ДЗ\n/list — мои ДЗ (изменить/удалить)\n/profile — профиль\n/logout — выйти"
	case RoleStudent:
		text += "\n<b>Студент:</b>\n/hw — домашние задания\n/profile — профиль\n/logout — выйти"
	default:
		text += "\nСначала зарегистрируйся: /start"
	}
	a.send(ctx, b, chatID, text, kbFor(usr))
}

func (a *app) fallback(ctx context.Context, b *bot.Bot, u *models.Update) {
	if u.Message == nil {
		return
	}
	chatID := u.Message.Chat.ID
	if commandOf(u.Message) != "" {
		a.send(ctx, b, chatID, "Неизвестная команда. Используй /help", nil)
		return
	}
	if a.store.User(chatID).Role == RoleNone {
		a.send(ctx, b, chatID, "Нажми /start, чтобы зарегистрироваться.", nil)
	}
}
