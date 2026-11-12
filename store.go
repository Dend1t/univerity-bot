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

type Role string

const (
	RoleNone    Role = ""
	RoleTeacher Role = "teacher"
	RoleStudent Role = "student"
)

// ------------------------------------------------------- справочник вуза

type Teacher struct {
	ID      string // 00001
	Name    string
	Subject string
}

// Password — то, что вуз выдаёт преподавателю: FCS + пятизначный ID.
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

// ------------------------------------------------------- модели

type Student struct {
	Faculty string `json:"faculty"`
	Course  int    `json:"course"`
	Group   string `json:"group"`
}

func (s Student) String() string {
	return fmt.Sprintf("%s · %d курс · группа %s", s.Faculty, s.Course, s.Group)
}

type User struct {
	ChatID    int64   `json:"chat_id"`
	Role      Role    `json:"role"`
	TeacherID string  `json:"teacher_id,omitempty"`
	Student   Student `json:"student,omitempty"`
	Done      []int   `json:"done,omitempty"` // отмеченные ДЗ
}

type Homework struct {
	ID        int       `json:"id"`
	TeacherID string    `json:"teacher_id"`
	Subject   string    `json:"subject"`
	Text      string    `json:"text"`
	Faculty   string    `json:"faculty,omitempty"` // "" = все
	Course    int       `json:"course,omitempty"`  // 0  = все
	Group     string    `json:"group,omitempty"`   // "" = все
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (h Homework) Audience() string {
	var p []string
	if h.Faculty != "" {
		p = append(p, h.Faculty)
	}
	if h.Course != 0 {
		p = append(p, fmt.Sprintf("%d курс", h.Course))
	}
	if h.Group != "" {
		p = append(p, "гр. "+h.Group)
	}
	if len(p) == 0 {
		return "все группы"
	}
	return strings.Join(p, " · ")
}

func (h Homework) Matches(s Student) bool {
	if h.Faculty != "" && !strings.EqualFold(h.Faculty, s.Faculty) {
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

// ------------------------------------------------------- хранилище

type Store struct {
	mu   sync.RWMutex
	path string

	Users  map[int64]*User   `json:"users"`
	HW     map[int]*Homework `json:"hw"`
	NextID int               `json:"next_id"`
}

func LoadStore(path string) *Store {
	s := &Store{path: path, Users: map[int64]*User{}, HW: map[int]*Homework{}, NextID: 1}
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
	log.Printf("хранилище: %d пользователей, %d заданий", len(s.Users), len(s.HW))
	return s
}

// saveLocked вызывается под захваченной mu: атомарная запись через tmp+rename.
func (s *Store) saveLocked() {
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

func (s *Store) HWOfTeacher(teacherID string) []Homework {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Homework
	for _, h := range s.HW {
		if h.TeacherID == teacherID {
			out = append(out, *h)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
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
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
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

// ------------------------------------------------------- FSM-сессии

const (
	stepNone        = ""
	stepTeacherPass = "teacher_pass"
	stepFaculty     = "stu_faculty"
	stepCourse      = "stu_course"
	stepGroup       = "stu_group"
	stepHWFac       = "hw_faculty"
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
