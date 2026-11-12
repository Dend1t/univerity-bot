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
	btnStats   = "📊 Статистика"
	btnProfile = "👤 Профиль"
	btnHelp    = "❓ Помощь"
	btnLogout  = "🚪 Выйти"
)

type app struct {
	store *Store
	sess  *Sessions
}

func newApp(s *Store) *app { return &app{store: s, sess: NewSessions()} }

// ================================================================ утилиты

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

// ================================================================ клавиатуры

func kb(rows ...[]models.KeyboardButton) *models.ReplyKeyboardMarkup {
	return &models.ReplyKeyboardMarkup{Keyboard: rows, ResizeKeyboard: true, IsPersistent: true}
}

func kbFor(u User) models.ReplyMarkup {
	switch u.Role {
	case RoleTeacher:
		return kb(
			[]models.KeyboardButton{{Text: btnNewHW}, {Text: btnMyHW}},
			[]models.KeyboardButton{{Text: btnStats}, {Text: btnProfile}},
			[]models.KeyboardButton{{Text: btnHelp}, {Text: btnLogout}},
		)
	case RoleHeadman:
		return kb(
			[]models.KeyboardButton{{Text: btnHW}, {Text: btnNewHW}},
			[]models.KeyboardButton{{Text: btnMyHW}, {Text: btnStats}},
			[]models.KeyboardButton{{Text: btnProfile}, {Text: btnLogout}},
		)
	case RoleStudent:
		return kb(
			[]models.KeyboardButton{{Text: btnHW}},
			[]models.KeyboardButton{{Text: btnStats}, {Text: btnProfile}},
			[]models.KeyboardButton{{Text: btnHelp}, {Text: btnLogout}},
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

func programKB(stopText string) *models.InlineKeyboardMarkup {
	var rows [][]models.InlineKeyboardButton
	var row []models.InlineKeyboardButton
	for i, p := range programs {
		row = append(row, models.InlineKeyboardButton{Text: p.Title, CallbackData: "prog:" + p.Code})
		if len(row) == 3 || i == len(programs)-1 {
			rows = append(rows, row)
			row = nil
		}
	}
	if stopText != "" {
		rows = append(rows, []models.InlineKeyboardButton{{Text: stopText, CallbackData: "aud:stop"}})
	}
	return &models.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// ================================================================ форматирование

func whoami(u User) string {
	switch u.Role {
	case RoleTeacher:
		t, _ := teacherByID(u.TeacherID)
		return fmt.Sprintf("👨‍🏫 <b>%s</b>\nПредмет: %s\nКод: %s", esc(t.Name), esc(t.Subject), esc(t.ID))
	case RoleHeadman:
		role := "Староста"
		if u.Deputy {
			role = "Зам. старосты"
		}
		return fmt.Sprintf("🧑‍✈️ <b>%s</b>\n%s", role, esc(u.Student.String()))
	case RoleStudent:
		return "🎓 <b>Студент</b>\n" + esc(u.Student.String())
	}
	return "Гость"
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

// ================================================================ отправка

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

func (a *app) widget(ctx context.Context, b *bot.Bot, chatID int64, doc, caption, state string) bool {
	_, err := b.SendWidget(ctx, &bot.SendWidgetParams{
		ChatID: chatID, HTML: doc, Height: 520,
		AllowQueries: true, ChatData: state, Caption: caption,
	})
	if err != nil {
		log.Printf("виджет недоступен (%v) — текстовый режим", err)
		return false
	}
	return true
}

// ================================================================ права

func (a *app) require(ctx context.Context, b *bot.Bot, chatID int64, role Role) (User, bool) {
	u := a.store.User(chatID)
	if u.Role == RoleNone {
		a.send(ctx, b, chatID, "Сначала зарегистрируйся: /start", nil)
		return u, false
	}
	if role != RoleNone && u.Role != role {
		a.send(ctx, b, chatID, "🚫 Эта команда доступна другой роли.", kbFor(u))
		return u, false
	}
	return u, true
}

func (a *app) canManage(u User, id int) bool {
	h, ok := a.store.HWByID(id)
	if !ok {
		return false
	}
	if h.AuthorChatID != 0 {
		return h.AuthorChatID == u.ChatID
	}
	return u.Role == RoleTeacher && h.TeacherID == u.TeacherID
}

func (a *app) ownHW(usr User) []Homework {
	if usr.Role == RoleTeacher {
		return a.store.HWOfTeacher(usr.TeacherID)
	}
	return a.store.HWOfAuthor(usr.ChatID)
}

// ================================================================ старт и роли

func (a *app) onStart(ctx context.Context, b *bot.Bot, u *models.Update) {
	a.start(ctx, b, chatIDOf(u))
}

func (a *app) start(ctx context.Context, b *bot.Bot, chatID int64) {
	a.sess.Clear(chatID)
	usr := a.store.User(chatID)
	if usr.Role != RoleNone {
		a.send(ctx, b, chatID, "С возвращением!\n\n"+whoami(usr), kbFor(usr))
		return
	}
	a.send(ctx, b, chatID, "Привет! Я <b>UniverityBot</b> 🎓\n\nКто ты?",
		&models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "👨‍🏫 Преподаватель", CallbackData: "role:teacher"}},
			{{Text: "🧑‍✈️ Староста / зам", CallbackData: "role:headman"}},
			{{Text: "🎓 Студент", CallbackData: "role:student"}},
		}})
}

func (a *app) onRole(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	answer(ctx, b, q.ID, "")
	if a.store.User(chatID).Role != RoleNone {
		a.send(ctx, b, chatID, "Ты уже зарегистрирован. Сменить роль: "+btnLogout, nil)
		return
	}
	switch q.Data {
	case "role:teacher":
		a.sess.Set(chatID, Session{Step: stepTeacherPass})
		a.send(ctx, b, chatID, "🔑 Пароль вуза, формат <b>FCS00001</b>.\n<i>Стенд: FCS00001…FCS00005</i>\n\nОтмена — /cancel", nil)
	case "role:headman":
		a.sess.Set(chatID, Session{Step: stepHeadmanPass})
		a.send(ctx, b, chatID,
			"🔑 Пароль группы, например <b>ISIT4211</b>:\n"+
				"• ISIT — направление (ISIT, PI, KB, IB, MKN, PRINF)\n"+
				"• 4 — номер группы\n• 2 — курс\n• 1 — подгруппа\n"+
				"• последняя цифра: <b>1</b> — староста, <b>2</b> — зам\n\nОтмена — /cancel", nil)
	case "role:student":
		a.sess.Set(chatID, Session{Step: stepFaculty})
		a.send(ctx, b, chatID, "🏛 Укажи <b>факультет</b> (например: ФКН).\n\nОтмена — /cancel", nil)
	}
}

// ================================================================ выбор направления

func (a *app) onProgram(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	answer(ctx, b, q.ID, "")
	code := strings.TrimPrefix(q.Data, "prog:")
	if _, ok := normalizeProgram(code); !ok {
		return
	}
	s := a.sess.Get(chatID)
	switch s.Step {
	case stepProgram:
		a.setRegProgram(ctx, b, chatID, s, code)
	case stepHWProgram:
		a.setDraftProgram(ctx, b, chatID, s, code)
	}
}

func (a *app) setRegProgram(ctx context.Context, b *bot.Bot, chatID int64, s Session, code string) {
	s.Reg.Program = code
	if s.Fix {
		usr := a.store.User(chatID)
		usr.Student.Program = code
		usr.Student.Group = normalizeGroup(code, usr.Student.Group)
		a.store.PutUser(usr)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Профиль обновлён!\n\n"+whoami(usr), kbFor(usr))
		return
	}
	s.Step = stepCourse
	a.sess.Set(chatID, s)
	a.send(ctx, b, chatID, "Направление: <b>"+esc(programTitle(code))+"</b>\n\nТеперь <b>курс</b> — число 1–6.", nil)
}

func (a *app) setDraftProgram(ctx context.Context, b *bot.Bot, chatID int64, s Session, code string) {
	s.Draft.Program, s.Step = code, stepHWCourse
	a.sess.Set(chatID, s)
	a.askAudience(ctx, b, chatID, s.Draft, "Укажи <b>курс</b> (1–6).", "👥 Всем на направлении")
}

func (a *app) askAudience(ctx context.Context, b *bot.Bot, chatID int64, draft Homework, question, stopText string) {
	a.send(ctx, b, chatID,
		"Аудитория пока: <b>"+esc(draft.Audience())+"</b>\n\n"+question,
		&models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: stopText, CallbackData: "aud:stop"}},
		}})
}

func isAudStep(step string) bool {
	switch step {
	case stepHWFac, stepHWProgram, stepHWCourse, stepHWGroup:
		return true
	}
	return false
}

// ================================================================ FSM

func (a *app) matchSession(u *models.Update) bool {
	return u.Message != nil && a.sess.Get(u.Message.Chat.ID).Step != stepNone
}

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
		a.start(ctx, b, chatID)
		return
	}

	switch s.Step {

	case stepTeacherPass:
		t, ok := teacherByPassword[strings.ToUpper(text)]
		if !ok {
			a.send(ctx, b, chatID, "❌ Пароль не найден. Пример: <b>FCS00003</b>. Или /cancel.", nil)
			return
		}
		usr := User{ChatID: chatID, Role: RoleTeacher, TeacherID: t.ID}
		a.store.PutUser(usr)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Вход выполнен!\n\n"+whoami(usr), kbFor(usr))

	case stepHeadmanPass:
		p, ok := parseHeadmanPassword(text)
		if !ok {
			a.send(ctx, b, chatID, "❌ Неверный пароль группы. Пример: <b>ISIT4211</b> (староста) или <b>ISIT4212</b> (зам). Или /cancel.", nil)
			return
		}
		usr := User{
			ChatID: chatID, Role: RoleHeadman, Deputy: p.Deputy,
			Student: Student{Program: p.Program, Course: p.Course, Group: p.Group},
		}
		a.store.PutUser(usr)
		a.sess.Set(chatID, Session{Step: stepFaculty, Reg: usr.Student, Fix: true})
		a.send(ctx, b, chatID, "✅ Доступ открыт: <b>"+esc(prettyGroup(p.Group))+"</b>, "+
			esc(programTitle(p.Program))+", "+strconv.Itoa(p.Course)+" курс.\n\nУкажи <b>факультет</b> для профиля.", nil)

	case stepFaculty:
		if text == "" {
			a.send(ctx, b, chatID, "Напиши факультет текстом.", nil)
			return
		}
		if s.Fix {
			usr := a.store.User(chatID)
			usr.Student.Faculty = trunc(text, 64)
			a.store.PutUser(usr)
			a.sess.Clear(chatID)
			a.send(ctx, b, chatID, "✅ Профиль сохранён!\n\n"+whoami(usr), kbFor(usr))
			return
		}
		s.Reg.Faculty, s.Step = trunc(text, 64), stepProgram
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Факультет: <b>"+esc(s.Reg.Faculty)+"</b>\n\nВыбери <b>направление</b>:", programKB(""))

	case stepProgram:
		code, ok := normalizeProgram(text)
		if !ok {
			a.send(ctx, b, chatID, "Выбери направление кнопкой или напиши: ИСИТ, ПИ, КБ, ИБ, МКН, ПРИНФ.", programKB(""))
			return
		}
		a.setRegProgram(ctx, b, chatID, s, code)

	case stepCourse:
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 6 {
			a.send(ctx, b, chatID, "❌ Курс — число от 1 до 6.", nil)
			return
		}
		s.Reg.Course, s.Step = n, stepGroup
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Курс: <b>"+strconv.Itoa(n)+"</b>\n\nТеперь <b>группа</b>: три цифры (например <b>421</b>) или полное название.", nil)

	case stepGroup:
		g := normalizeGroup(s.Reg.Program, text)
		if g == "" {
			a.send(ctx, b, chatID, "Напиши группу, например <b>421</b>.", nil)
			return
		}
		s.Reg.Group = g
		usr := User{ChatID: chatID, Role: RoleStudent, Student: s.Reg}
		a.store.PutUser(usr)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Регистрация завершена!\n\n"+whoami(usr)+"\n\nЖми «"+btnHW+"».", kbFor(usr))

	case stepHWFac:
		if text == "" {
			a.send(ctx, b, chatID, "Напиши факультет текстом.", nil)
			return
		}
		s.Draft.Faculty, s.Step = trunc(text, 64), stepHWProgram
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Аудитория: <b>"+esc(s.Draft.Audience())+"</b>\n\nВыбери <b>направление</b>:", programKB("👥 Всем на факультете"))

	case stepHWProgram:
		code, ok := normalizeProgram(text)
		if !ok {
			a.send(ctx, b, chatID, "Выбери направление кнопкой.", programKB("👥 Всем на направлении"))
			return
		}
		a.setDraftProgram(ctx, b, chatID, s, code)

	case stepHWCourse:
		n, err := strconv.Atoi(text)
		if err != nil || n < 1 || n > 6 {
			a.send(ctx, b, chatID, "❌ Курс — число от 1 до 6.", nil)
			return
		}
		s.Draft.Course, s.Step = n, stepHWGroup
		a.sess.Set(chatID, s)
		a.askAudience(ctx, b, chatID, s.Draft, "Укажи <b>группу</b> (например 421).", "👥 Всем группам курса")

	case stepHWGroup:
		g := normalizeGroup(s.Draft.Program, text)
		if g == "" {
			a.send(ctx, b, chatID, "Напиши группу, например <b>421</b>.", nil)
			return
		}
		s.Draft.Group, s.Step = g, stepHWText
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Аудитория: <b>"+esc(s.Draft.Audience())+"</b>\n\nОтправь <b>текст задания</b>.\nОтмена — /cancel", nil)

	case stepHWText:
		if text == "" {
			a.send(ctx, b, chatID, "❌ Пустое задание. Пришли текст или /cancel.", nil)
			return
		}
		usr := a.store.User(chatID)
		if !usr.CanPublish() {
			a.sess.Clear(chatID)
			return
		}
		h := s.Draft
		h.AuthorChatID, h.Text = chatID, trunc(text, 3000)
		if usr.Role == RoleTeacher {
			t, _ := teacherByID(usr.TeacherID)
			h.TeacherID, h.Subject = t.ID, t.Subject
		}
		if h.Subject == "" {
			h.Subject = "Объявление группы"
		}
		h = a.store.AddHW(h)
		a.sess.Clear(chatID)
		a.send(ctx, b, chatID, "✅ Опубликовано!\n\n"+hwCard(h, false), kbFor(usr))

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

// ================================================================ создание ДЗ

func (a *app) onNewHW(ctx context.Context, b *bot.Bot, u *models.Update) {
	a.startNewHW(ctx, b, chatIDOf(u))
}

func (a *app) startNewHW(ctx context.Context, b *bot.Bot, chatID int64) {
	usr, ok := a.require(ctx, b, chatID, RoleNone)
	if !ok {
		return
	}
	a.sess.Clear(chatID)

	if usr.Role == RoleHeadman {
		a.startHeadmanHW(ctx, b, chatID)
		return
	}
	if usr.Role != RoleTeacher {
		a.send(ctx, b, chatID, "🚫 Публиковать ДЗ могут преподаватель и староста.", kbFor(usr))
		return
	}
	a.send(ctx, b, chatID, "Кому назначить задание?", &models.InlineKeyboardMarkup{
		InlineKeyboard: [][]models.InlineKeyboardButton{
			{{Text: "👥 Всем студентам", CallbackData: "aud:all"}},
			{{Text: "🎯 Выбрать аудиторию", CallbackData: "aud:pick"}},
		}})
}

func (a *app) startHeadmanHW(ctx context.Context, b *bot.Bot, chatID int64) {
	usr := a.store.User(chatID)
	if usr.Role != RoleHeadman {
		return
	}
	subs := append(subjects(), "Объявление группы")
	var rows [][]models.InlineKeyboardButton
	for i, s := range subs {
		rows = append(rows, []models.InlineKeyboardButton{{Text: s, CallbackData: "subj:" + strconv.Itoa(i)}})
	}
	a.sess.Set(chatID, Session{Draft: Homework{
		Program: usr.Student.Program,
		Course:  usr.Student.Course,
		Group:   usr.Student.Group,
	}})
	a.send(ctx, b, chatID, "📌 Задание для группы <b>"+esc(prettyGroup(usr.Student.Group))+
		"</b>\n\nВыбери предмет:", &models.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (a *app) onSubject(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	answer(ctx, b, q.ID, "")
	usr := a.store.User(chatID)
	if usr.Role != RoleHeadman {
		return
	}
	idx, err := strconv.Atoi(strings.TrimPrefix(q.Data, "subj:"))
	subs := append(subjects(), "Объявление группы")
	if err != nil || idx < 0 || idx >= len(subs) {
		return
	}
	s := a.sess.Get(chatID)
	s.Draft = Homework{
		Subject: subs[idx],
		Program: usr.Student.Program,
		Course:  usr.Student.Course,
		Group:   usr.Student.Group,
	}
	s.Step = stepHWText
	a.sess.Set(chatID, s)
	a.send(ctx, b, chatID, "Предмет: <b>"+esc(subs[idx])+"</b>\nАудитория: <b>"+
		esc(s.Draft.Audience())+"</b>\n\nОтправь <b>текст задания</b>.\nОтмена — /cancel", nil)
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
		a.send(ctx, b, chatID, "Аудитория: <b>все студенты</b>\n\nОтправь <b>текст задания</b>.\nОтмена — /cancel", nil)
	case "aud:pick":
		a.sess.Set(chatID, Session{Step: stepHWFac})
		a.askAudience(ctx, b, chatID, Homework{}, "Укажи <b>факультет</b>.", "👥 Всем студентам")
	case "aud:stop":
		s := a.sess.Get(chatID)
		if !isAudStep(s.Step) {
			answer(ctx, b, q.ID, "Выбор аудитории завершён")
			return
		}
		s.Step = stepHWText
		a.sess.Set(chatID, s)
		a.send(ctx, b, chatID, "Аудитория: <b>"+esc(s.Draft.Audience())+"</b>\n\nОтправь <b>текст задания</b>.\nОтмена — /cancel", nil)
	}
}

// ================================================================ мои ДЗ (дашборд)

func (a *app) onMyHW(ctx context.Context, b *bot.Bot, u *models.Update) {
	a.showDash(ctx, b, chatIDOf(u))
}

func (a *app) showDash(ctx context.Context, b *bot.Bot, chatID int64) {
	usr, ok := a.require(ctx, b, chatID, RoleNone)
	if !ok {
		return
	}
	if !usr.CanPublish() {
		a.send(ctx, b, chatID, "🚫 Раздел доступен преподавателю и старосте.", kbFor(usr))
		return
	}
	list := a.ownHW(usr)
	if len(list) == 0 {
		a.send(ctx, b, chatID, "Пока нет своих заданий. Нажми «"+btnNewHW+"».", kbFor(usr))
		return
	}

	items := make([]dashItem, 0, 12)
	for i, h := range list {
		if i == 12 {
			break
		}
		d, t := a.store.DoneStats(h)
		items = append(items, dashItem{
			ID: h.ID, Subject: h.Subject,
			Meta: fmt.Sprintf("#%d · %s", h.ID, trunc(h.Audience(), 46)),
			Text: trunc(h.Text, 140), Done: d, Total: t, Pct: pct(d, t),
		})
	}
	if doc, err := renderDash(items); err == nil {
		if a.widget(ctx, b, chatID, doc, "📋 Твои задания и статистика сдачи", encodeState(wState{})) {
			return
		}
	}
	for _, h := range list {
		d, t := a.store.DoneStats(h)
		a.send(ctx, b, chatID, hwCard(h, false)+fmt.Sprintf("\n\n📊 Сдали: <b>%d</b> из %d", d, t), hwKB(h.ID))
	}
}

func (a *app) onHWAction(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.CallbackQuery
	chatID := chatIDOf(u)
	parts := strings.Split(q.Data, ":")
	if len(parts) != 3 {
		answer(ctx, b, q.ID, "")
		return
	}
	id, err := strconv.Atoi(parts[2])
	if err != nil {
		answer(ctx, b, q.ID, "Некорректный ID")
		return
	}
	usr := a.store.User(chatID)
	if !usr.CanPublish() {
		answer(ctx, b, q.ID, "Нет прав")
		return
	}
	h, found := a.store.HWByID(id)
	if !found {
		answer(ctx, b, q.ID, "Задание уже удалено")
		return
	}
	if !a.canManage(usr, id) {
		answer(ctx, b, q.ID, "Это чужая запись")
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
			_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{ChatID: chatID, MessageID: msgID, ReplyMarkup: confirm})
		} else {
			a.send(ctx, b, chatID, fmt.Sprintf("Удалить ДЗ #%d?", id), confirm)
		}
	case "yes":
		a.store.DeleteHW(id)
		a.sess.Clear(chatID)
		answer(ctx, b, q.ID, "Удалено")
		text := fmt.Sprintf("🗑 Задание <b>#%d</b> (%s) удалено.", id, esc(h.Subject))
		if msgID != 0 {
			_, _ = b.EditMessageText(ctx, &bot.EditMessageTextParams{ChatID: chatID, MessageID: msgID, Text: text, ParseMode: "HTML"})
		} else {
			a.send(ctx, b, chatID, text, kbFor(usr))
		}
	case "no":
		answer(ctx, b, q.ID, "Отменено")
		if msgID != 0 {
			_, _ = b.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{ChatID: chatID, MessageID: msgID, ReplyMarkup: hwKB(id)})
		}
	}
}

// ================================================================ борд студента/старосты

func (a *app) onStudentHW(ctx context.Context, b *bot.Bot, u *models.Update) {
	a.showBoard(ctx, b, chatIDOf(u))
}

func (a *app) showBoard(ctx context.Context, b *bot.Bot, chatID int64) {
	usr, ok := a.require(ctx, b, chatID, RoleNone)
	if !ok {
		return
	}
	if usr.Role == RoleTeacher {
		a.showDash(ctx, b, chatID)
		return
	}
	if usr.Student.Program == "" {
		a.sess.Set(chatID, Session{Step: stepProgram, Reg: usr.Student, Fix: true})
		a.send(ctx, b, chatID, "🆕 Уточни <b>направление</b>:", programKB(""))
		return
	}

	list := a.store.HWForStudent(usr.Student)
	if len(list) == 0 {
		a.send(ctx, b, chatID, "🎉 Для твоей группы заданий пока нет.", kbFor(usr))
		return
	}

	done := map[int]bool{}
	for _, id := range usr.Done {
		done[id] = true
	}
	items := make([]boardItem, 0, 8)
	for i, h := range list {
		if i == 8 {
			break
		}
		items = append(items, boardItem{
			ID: h.ID, Subject: h.Subject,
			Meta: fmt.Sprintf("#%d · %s · %s", h.ID, trunc(h.Audience(), 40), h.CreatedAt.Format("02.01")),
			Text: trunc(h.Text, 150),
			Mine: a.canManage(usr, h.ID),
		})
	}
	if doc, err := renderBoard(items, usr.Role == RoleHeadman); err == nil {
		if a.widget(ctx, b, chatID, doc, "📚 Задания группы "+prettyGroup(usr.Student.Group), encodeState(wState{Done: usr.Done})) {
			return
		}
	}
	a.send(ctx, b, chatID, fmt.Sprintf("📚 Заданий: <b>%d</b>", len(list)), kbFor(usr))
	for i, h := range list {
		if i == 15 {
			break
		}
		var markup models.ReplyMarkup
		if a.canManage(usr, h.ID) {
			markup = hwKB(h.ID)
		}
		a.send(ctx, b, chatID, hwCard(h, done[h.ID]), markup)
	}
}

// ================================================================ профиль и статистика

func (a *app) onProfile(ctx context.Context, b *bot.Bot, u *models.Update) {
	a.showProfile(ctx, b, chatIDOf(u))
}

func (a *app) showProfile(ctx context.Context, b *bot.Bot, chatID int64) {
	usr, ok := a.require(ctx, b, chatID, RoleNone)
	if !ok {
		return
	}
	v := profileView{Title: "👤 Профиль", Name: usr.Role.Title(), Line1: usr.Student.String()}
	switch usr.Role {
	case RoleTeacher:
		t, _ := teacherByID(usr.TeacherID)
		v.Name, v.Line1 = t.Name, t.Subject
		v.Line2 = fmt.Sprintf("Опубликовано заданий: %d", len(a.store.HWOfTeacher(t.ID)))
	case RoleHeadman:
		if usr.Deputy {
			v.Name = "Зам. старосты"
		}
		v.Line2 = fmt.Sprintf("Своих записей: %d", len(a.store.HWOfAuthor(chatID)))
	default:
		v.Line2 = fmt.Sprintf("Отмечено выполненных: %d", len(usr.Done))
	}
	if doc, err := renderProfile(v); err == nil {
		if a.widget(ctx, b, chatID, doc, "👤 Профиль", encodeState(wState{})) {
			return
		}
	}
	a.send(ctx, b, chatID, whoami(usr)+"\n"+esc(v.Line2), kbFor(usr))
}

func (a *app) onStats(ctx context.Context, b *bot.Bot, u *models.Update) {
	a.showStats(ctx, b, chatIDOf(u))
}

func (a *app) statsRows(usr User) (string, []statRow) {
	if usr.CanPublish() {
		list := a.ownHW(usr)
		rows := make([]statRow, 0, 10)
		for i, h := range list {
			if i == 10 {
				break
			}
			d, t := a.store.DoneStats(h)
			rows = append(rows, statRow{Name: fmt.Sprintf("#%d %s", h.ID, trunc(h.Subject, 28)), Done: d, Total: t, Pct: pct(d, t)})
		}
		return "Сдача по моим заданиям", rows
	}

	list := a.store.HWForStudent(usr.Student)
	done := map[int]bool{}
	for _, id := range usr.Done {
		done[id] = true
	}
	agg := map[string][2]int{}
	var order []string
	for _, h := range list {
		v := agg[h.Subject]
		v[1]++
		if done[h.ID] {
			v[0]++
		}
		if _, seen := agg[h.Subject]; !seen {
			order = append(order, h.Subject)
		}
		agg[h.Subject] = v
	}
	rows := make([]statRow, 0, len(order))
	for _, s := range order {
		v := agg[s]
		rows = append(rows, statRow{Name: s, Done: v[0], Total: v[1], Pct: pct(v[0], v[1])})
	}
	return "Мой прогресс по предметам", rows
}

func (a *app) showStats(ctx context.Context, b *bot.Bot, chatID int64) {
	usr, ok := a.require(ctx, b, chatID, RoleNone)
	if !ok {
		return
	}
	title, rows := a.statsRows(usr)
	if len(rows) == 0 {
		a.send(ctx, b, chatID, "📊 Пока нечего показывать.", kbFor(usr))
		return
	}
	if doc, err := renderStats(title, rows); err == nil {
		if a.widget(ctx, b, chatID, doc, "📊 "+title, encodeState(wState{})) {
			return
		}
	}
	var sb strings.Builder
	sb.WriteString("📊 <b>" + esc(title) + "</b>\n")
	for _, r := range rows {
		sb.WriteString(fmt.Sprintf("\n%s — <b>%d</b> из %d", esc(r.Name), r.Done, r.Total))
	}
	a.send(ctx, b, chatID, sb.String(), kbFor(usr))
}

// ================================================================ виджеты: роутер

func (a *app) dashStats(usr User) map[int][2]int {
	out := map[int][2]int{}
	list := a.ownHW(usr)
	for i, h := range list {
		if i == 12 {
			break
		}
		d, t := a.store.DoneStats(h)
		out[h.ID] = [2]int{d, t}
	}
	return out
}

func (a *app) matchWidget(u *models.Update) bool { return u.WidgetQuery != nil }

func (a *app) onWidget(ctx context.Context, b *bot.Bot, u *models.Update) {
	q := u.WidgetQuery
	chatID := q.From.ID
	if q.Chat != nil {
		chatID = q.Chat.ID
	}
	usr := a.store.User(chatID)
	data := q.Data
	st := wState{}

	switch {
	case strings.HasPrefix(data, "d:"):
		id, _ := strconv.Atoi(data[2:])
		if _, ok := a.store.HWByID(id); ok {
			st.Done = a.store.ToggleDone(chatID, id)
		} else {
			st.Done = usr.Done
			st.Note = "нет такого ДЗ"
		}

	case strings.HasPrefix(data, "x:"):
		id, _ := strconv.Atoi(data[2:])
		if a.canManage(usr, id) {
			a.store.DeleteHW(id)
			st.Note = "del:" + strconv.Itoa(id)
		} else {
			st.Note = "нет прав"
		}
		st.Done = a.store.User(chatID).Done

	case data == "a:":
		if usr.Role == RoleHeadman {
			a.startHeadmanHW(ctx, b, chatID)
			st.Note = "ok"
		} else {
			st.Note = "нет прав"
		}

	case strings.HasPrefix(data, "n:"):
		switch data[2:] {
		case "hw":
			a.showBoard(ctx, b, chatID)
		case "stats":
			a.showStats(ctx, b, chatID)
		}
		st.Note = "ok"

	case data == "r:":
		st.Stats = a.dashStats(usr)

	case data == "r:stats":
		a.showStats(ctx, b, chatID)
		st.Note = "ok"
	}

	payload := encodeState(st)
	if _, err := b.AnswerWidgetQuery(ctx, &bot.AnswerWidgetQueryParams{
		WidgetQueryID: q.ID, Data: payload,
	}); err != nil {
		log.Printf("answerWidgetQuery: %v", err)
	}
	if q.MessageID != 0 {
		if _, err := b.EditMessageWidgetData(ctx, &bot.EditMessageWidgetDataParams{
			ChatID: chatID, MessageID: q.MessageID, ChatData: &payload,
		}); err != nil {
			log.Printf("editMessageWidgetData: %v", err)
		}
	}
}

// ================================================================ меню

func (a *app) onLogout(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	a.store.DropUser(chatID)
	a.sess.Clear(chatID)
	a.send(ctx, b, chatID, "🚪 Ты вышел. Профиль удалён.", kbFor(User{}))
	a.start(ctx, b, chatID)
}

func (a *app) onCancel(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	a.sess.Clear(chatID)
	a.send(ctx, b, chatID, "Сейчас нечего отменять.", kbFor(a.store.User(chatID)))
}

func (a *app) onHelp(ctx context.Context, b *bot.Bot, u *models.Update) {
	chatID := chatIDOf(u)
	usr := a.store.User(chatID)
	text := "❓ <b>Помощь</b>\n\n/start — меню / смена роли\n/help — справка\n/cancel — отменить шаг\n"
	switch usr.Role {
	case RoleTeacher:
		text += "\n<b>Преподаватель:</b>\n/new — ДЗ (всем / факультет / направление / курс / группа)\n/list — мои ДЗ\n/stats — статистика сдачи"
	case RoleHeadman:
		text += "\n<b>Староста:</b>\n/new — ДЗ <b>только своей группе</b>\n/hw — задания группы\n/list — мои записи\n/stats — статистика группы"
	case RoleStudent:
		text += "\n<b>Студент:</b>\n/hw — задания\n/stats — мой прогресс\n/profile — профиль"
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
