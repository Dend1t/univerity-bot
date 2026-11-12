package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------- роли

type Role string

const (
	RoleNone    Role = ""
	RoleTeacher Role = "teacher"
	RoleHeadman Role = "headman"
	RoleStudent Role = "student"
)

func (r Role) Title() string {
	switch r {
	case RoleTeacher:
		return "Преподаватель"
	case RoleHeadman:
		return "Староста"
	case RoleStudent:
		return "Студент"
	}
	return "Гость"
}

// ---------------------------------------------------------------- направления

type Program struct{ Code, Title string }

var programs = []Program{
	{"ISIT", "ИСИТ"},
	{"PI", "ПИ"},
	{"KB", "КБ"},
	{"IB", "ИБ"},
	{"MKN", "МКН"},
	{"PRINF", "ПРИНФ"},
}

var programAlias = func() map[string]string {
	m := map[string]string{}
	for _, p := range programs {
		m[p.Code] = p.Code
		m[p.Title] = p.Code
	}
	return m
}()

func normalizeProgram(s string) (string, bool) {
	k := strings.ToUpper(strings.TrimSpace(s))
	k = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(k)
	c, ok := programAlias[k]
	return c, ok
}

func programTitle(code string) string {
	for _, p := range programs {
		if p.Code == code {
			return p.Title
		}
	}
	return code
}

func programsByLen() []Program {
	out := append([]Program(nil), programs...)
	sort.Slice(out, func(i, j int) bool { return len(out[i].Code) > len(out[j].Code) })
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------- группы

func canonGroup(code, digits string) string { return code + "-" + digits }

// normalizeGroup приводит «421», «ИСИТ-421», «isit 421» к канону ISIT-421.
func normalizeGroup(code, raw string) string {
	g := strings.ToUpper(strings.TrimSpace(raw))
	g = strings.NewReplacer(" ", "", "_", "-", "—", "-").Replace(g)
	for _, p := range programs {
		for _, pref := range []string{p.Code + "-", p.Title + "-", p.Code, p.Title} {
			if strings.HasPrefix(g, pref) {
				g = strings.TrimLeft(strings.TrimPrefix(g, pref), "-")
				break
			}
		}
	}
	if isDigits(g) {
		return canonGroup(code, g)
	}
	return g
}

func prettyGroup(g string) string {
	for _, p := range programs {
		if strings.HasPrefix(g, p.Code+"-") {
			return p.Title + "-" + strings.TrimPrefix(g, p.Code+"-")
		}
	}
	return g
}

// ---------------------------------------------------------------- преподаватели

type Teacher struct {
	ID      string
	Name    string
	Subject string
}

func (t Teacher) Password() string { return "FCS" + t.ID }

var teachers = []Teacher{
	{"00001", "Преподователь_тест_1", "Математический анализ"},
	{"00002", "Преподователь_тест_2", "Основы программирования"},
	{"00003", "Преподователь_тест_3", "Дискретная математика"},
	{"00004", "Преподователь_тест_4", "Оптика и механика"},
	{"00005", "Преподователь_тест_5", "История"},
}

var teacherByPassword = func() map[string]Teacher {
	m := make(map[string]Teacher, len(teachers))
	for _, t := range teachers {
		m[t.Password()] = t
	}
	return m
}()

func teacherByID(id string) (Teacher, bool) {
	for _, t := range teachers {
		if t.ID == id {
			return t, true
		}
	}
	return Teacher{}, false
}

func subjects() []string {
	out := make([]string, 0, len(teachers))
	for _, t := range teachers {
		out = append(out, t.Subject)
	}
	return out
}

// ---------------------------------------------------------------- пароль старосты

type HeadmanPass struct {
	Program string
	Course  int
	Group   string
	Deputy  bool
}

// ISIT4211 = ISIT | 4 — номер группы | 2 — курс | 1 — подгруппа | 1 — староста (2 — зам).
func parseHeadmanPassword(raw string) (HeadmanPass, bool) {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.NewReplacer(" ", "", "-", "", "_", "").Replace(s)
	for _, p := range programsByLen() {
		for _, pref := range []string{p.Code, p.Title} {
			if !strings.HasPrefix(s, pref) {
				continue
			}
			rest := s[len(pref):]
			if len(rest) != 4 || !isDigits(rest) {
				return HeadmanPass{}, false
			}
			if rest[3] != '1' && rest[3] != '2' {
				return HeadmanPass{}, false
			}
			course := int(rest[1] - '0')
			if course < 1 || course > 6 {
				return HeadmanPass{}, false
			}
			return HeadmanPass{
				Program: p.Code,
				Course:  course,
				Group:   canonGroup(p.Code, rest[:3]),
				Deputy:  rest[3] == '2',
			}, true
		}
	}
	return HeadmanPass{}, false
}

// ---------------------------------------------------------------- модели

type Student struct {
	Faculty string `json:"faculty"`
	Program string `json:"program"` // код: ISIT
	Course  int    `json:"course"`
	Group   string `json:"group"` // ISIT-421
}

func (s Student) String() string {
	return fmt.Sprintf("%s · %s · %d курс · %s",
		s.Faculty, programTitle(s.Program), s.Course, prettyGroup(s.Group))
}

type User struct {
	ChatID    int64   `json:"chat_id"`
	Role      Role    `json:"role"`
	Deputy    bool    `json:"deputy,omitempty"`
	TeacherID string  `json:"teacher_id,omitempty"`
	Student   Student `json:"student,omitempty"`
	Done      []int   `json:"done,omitempty"`
}

func (u User) CanPublish() bool { return u.Role == RoleTeacher || u.Role == RoleHeadman }

type Homework struct {
	ID           int       `json:"id"`
	TeacherID    string    `json:"teacher_id,omitempty"`
	AuthorChatID int64     `json:"author_chat_id,omitempty"`
	Subject      string    `json:"subject"`
	Text         string    `json:"text"`
	Faculty      string    `json:"faculty,omitempty"`
	Program      string    `json:"program,omitempty"`
	Course       int       `json:"course,omitempty"`
	Group        string    `json:"group,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (h Homework) Audience() string {
	var p []string
	if h.Faculty != "" {
		p = append(p, h.Faculty)
	}
	if h.Program != "" {
		p = append(p, programTitle(h.Program))
	}
	if h.Course != 0 {
		p = append(p, fmt.Sprintf("%d курс", h.Course))
	}
	if h.Group != "" {
		p = append(p, prettyGroup(h.Group))
	}
	if len(p) == 0 {
		return "все студенты"
	}
	switch {
	case h.Group != "":
	case h.Course != 0:
		p = append(p, "все группы")
	case h.Program != "":
		p = append(p, "все курсы")
	default:
		p = append(p, "все направления")
	}
	return strings.Join(p, " · ")
}

func (h Homework) Matches(s Student) bool {
	if h.Faculty != "" && !strings.EqualFold(h.Faculty, s.Faculty) {
		return false
	}
	if h.Program != "" && !strings.EqualFold(h.Program, s.Program) {
		return false
	}
	if h.Course != 0 && h.Course != s.Course {
		return false
	}
	if h.Group != "" && !strings.EqualFold(h.Group, s.Group) {
		return false
	}
	return true
}

// ---------------------------------------------------------------- хранилище

type Store struct {
	mu   sync.RWMutex
	path string

	Users  map[int64]*User   `json:"users"`
	HW     map[int]*Homework `json:"hw"`
	NextID int               `json:"next_id"`
}

func LoadStore(path string) *Store {
	s := &Store{path: path, Users: map[int64]*User{}, HW: map[int]*Homework{}, NextID: 1}
	if path == "" {
		log.Println("хранилище: только память")
		return s
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("хранилище: %v", err)
		}
		return s
	}
	if err := json.Unmarshal(data, s); err != nil {
		log.Printf("хранилище повреждено (%v) — стартуем с чистого", err)
		return s
	}
	if s.Users == nil {
		s.Users = map[int64]*User{}
	}
	if s.HW == nil {
		s.HW = map[int]*Homework{}
	}
	if s.NextID == 0 {
		s.NextID = 1
	}
	s.migrate()
	log.Printf("хранилище: %d пользователей, %d заданий", len(s.Users), len(s.HW))
	return s
}

// migrate приводит старые профили/ДЗ к кодам направлений и канону групп.
func (s *Store) migrate() {
	for _, u := range s.Users {
		if code, ok := normalizeProgram(u.Student.Program); ok {
			u.Student.Program = code
		} else {
			u.Student.Program = ""
		}
		if u.Student.Program != "" {
			u.Student.Group = normalizeGroup(u.Student.Program, u.Student.Group)
		}
	}
	for _, h := range s.HW {
		if code, ok := normalizeProgram(h.Program); ok {
			h.Program = code
		} else {
			h.Program = ""
		}
		if h.Program != "" && h.Group != "" {
			h.Group = normalizeGroup(h.Program, h.Group)
		}
	}
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		log.Printf("сериализация: %v", err)
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("запись: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("rename: %v", err)
	}
}

func (s *Store) User(chatID int64) User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if u, ok := s.Users[chatID]; ok {
		return *u
	}
	return User{ChatID: chatID}
}

func (s *Store) PutUser(u User) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := u
	s.Users[u.ChatID] = &cp
	s.saveLocked()
}

func (s *Store) DropUser(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.Users, chatID)
	s.saveLocked()
}

func (s *Store) AddHW(h Homework) Homework {
	s.mu.Lock()
	defer s.mu.Unlock()
	h.ID, s.NextID = s.NextID, s.NextID+1
	h.CreatedAt, h.UpdatedAt = time.Now(), time.Now()
	s.HW[h.ID] = &h
	s.saveLocked()
	return h
}

func (s *Store) EditText(id int, text string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.HW[id]
	if !ok {
		return false
	}
	h.Text, h.UpdatedAt = text, time.Now()
	s.saveLocked()
	return true
}

func (s *Store) DeleteHW(id int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.HW[id]; !ok {
		return false
	}
	delete(s.HW, id)
	s.saveLocked()
	return true
}

func (s *Store) HWByID(id int) (Homework, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	h, ok := s.HW[id]
	if !ok {
		return Homework{}, false
	}
	return *h, true
}

func sortHW(out []Homework) []Homework {
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

func (s *Store) HWOfAuthor(chatID int64) []Homework {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Homework
	for _, h := range s.HW {
		if h.AuthorChatID == chatID {
			out = append(out, *h)
		}
	}
	return sortHW(out)
}

func (s *Store) HWOfTeacher(teacherID string) []Homework {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Homework
	for _, h := range s.HW {
		if h.TeacherID == teacherID {
			out = append(out, *h)
		}
	}
	return sortHW(out)
}

func (s *Store) HWForStudent(st Student) []Homework {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Homework
	for _, h := range s.HW {
		if h.Matches(st) {
			out = append(out, *h)
		}
	}
	return sortHW(out)
}

func (s *Store) ToggleDone(chatID int64, hwID int) []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.Users[chatID]
	if !ok {
		return nil
	}
	idx := -1
	for i, id := range u.Done {
		if id == hwID {
			idx = i
			break
		}
	}
	if idx >= 0 {
		u.Done = append(u.Done[:idx], u.Done[idx+1:]...)
	} else {
		u.Done = append(u.Done, hwID)
	}
	s.saveLocked()
	out := make([]int, len(u.Done))
	copy(out, u.Done)
	return out
}

// DoneStats — сколько студентов из аудитории задания отметили его выполненным.
func (s *Store) DoneStats(h Homework) (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	done, total := 0, 0
	for _, u := range s.Users {
		if u.Role != RoleStudent && u.Role != RoleHeadman {
			continue
		}
		if !h.Matches(u.Student) {
			continue
		}
		total++
		for _, id := range u.Done {
			if id == h.ID {
				done++
				break
			}
		}
	}
	return done, total
}

// ---------------------------------------------------------------- сессии

const (
	stepNone        = ""
	stepTeacherPass = "teacher_pass"
	stepHeadmanPass = "headman_pass"
	stepFaculty     = "stu_faculty"
	stepProgram     = "stu_program"
	stepCourse      = "stu_course"
	stepGroup       = "stu_group"
	stepHWFac       = "hw_faculty"
	stepHWProgram   = "hw_program"
	stepHWCourse    = "hw_course"
	stepHWGroup     = "hw_group"
	stepHWText      = "hw_text"
	stepHWEdit      = "hw_edit"
)

type Session struct {
	Step  string
	HWID  int
	Draft Homework
	Reg   Student
	Fix   bool
}

type Sessions struct {
	mu sync.Mutex
	m  map[int64]Session
}

func NewSessions() *Sessions { return &Sessions{m: map[int64]Session{}} }

func (s *Sessions) Get(id int64) Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.m[id]
}

func (s *Sessions) Set(id int64, v Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = v
}

func (s *Sessions) Clear(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}
