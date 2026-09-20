// Package loadcase 管理具名装载状态档：内存索引 + JSON 文件持久化。
// 所有方法可并发调用；每个装载状态独立存取，互不渗透。
package loadcase

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unicode"

	"stability-service/internal/stability"
)

var (
	// ErrNotFound 表示指定名字的装载状态不存在。
	ErrNotFound = errors.New("装载状态不存在")
	// ErrExists 表示同名装载状态已存在。
	ErrExists = errors.New("同名装载状态已存在")
)

// Case 是一份具名装载状态档。
type Case struct {
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Input       stability.Input    `json:"input"`
	Particulars map[string]float64 `json:"particulars,omitempty"` // 主要尺度等备注信息（如船长、船宽、吃水）
	CreatedAt   time.Time          `json:"createdAt"`
	UpdatedAt   time.Time          `json:"updatedAt"`
}

// Store 是装载状态档的并发安全存储，变更即时落盘。
type Store struct {
	mu    sync.RWMutex
	path  string
	cases map[string]Case
}

// Open 打开（或按需创建）位于 path 的存储文件。
func Open(path string) (*Store, error) {
	s := &Store{path: path, cases: make(map[string]Case)}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取装载状态存储失败: %w", err)
	}
	if len(data) == 0 {
		return s, nil
	}
	var cases []Case
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, fmt.Errorf("解析装载状态存储 %s 失败: %w", path, err)
	}
	for _, c := range cases {
		s.cases[c.Name] = c
	}
	return s, nil
}

// Create 新建一份装载状态档；同名已存在时返回 ErrExists。
func (s *Store) Create(c Case) error {
	if err := validateCase(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cases[c.Name]; ok {
		return fmt.Errorf("%w: %q", ErrExists, c.Name)
	}
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	s.cases[c.Name] = c
	return s.persistLocked()
}

// Update 覆盖既有装载状态档；不存在时返回 ErrNotFound。CreatedAt 保留原值。
func (s *Store) Update(c Case) error {
	if err := validateCase(c); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.cases[c.Name]
	if !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, c.Name)
	}
	c.CreatedAt = old.CreatedAt
	c.UpdatedAt = time.Now().UTC()
	s.cases[c.Name] = c
	return s.persistLocked()
}

// Get 按名字取回装载状态档。
func (s *Store) Get(name string) (Case, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.cases[name]
	return c, ok
}

// List 返回全部装载状态档，按名字排序。
func (s *Store) List() []Case {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.listLocked()
}

// Delete 删除指定名字的装载状态档；不存在时返回 ErrNotFound。
func (s *Store) Delete(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.cases[name]; !ok {
		return fmt.Errorf("%w: %q", ErrNotFound, name)
	}
	delete(s.cases, name)
	return s.persistLocked()
}

func (s *Store) listLocked() []Case {
	out := make([]Case, 0, len(s.cases))
	for _, c := range s.cases {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// persistLocked 把当前内容原子写入磁盘（先写临时文件再改名）。调用方须持锁。
func (s *Store) persistLocked() error {
	if dir := filepath.Dir(s.path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建数据目录失败: %w", err)
		}
	}
	data, err := json.MarshalIndent(s.listLocked(), "", "  ")
	if err != nil {
		return fmt.Errorf("序列化装载状态失败: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("写入装载状态存储失败: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("落盘装载状态存储失败: %w", err)
	}
	return nil
}

func validateCase(c Case) error {
	if c.Name == "" {
		return &stability.ValidationError{Problems: []string{"装载状态名称 name 不能为空"}}
	}
	if len(c.Name) > 128 {
		return &stability.ValidationError{Problems: []string{"装载状态名称 name 过长（上限 128 字符）"}}
	}
	for _, r := range c.Name {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return &stability.ValidationError{Problems: []string{
				fmt.Sprintf("装载状态名称 %q 含非法字符（不允许路径分隔符与控制字符）", c.Name)}}
		}
	}
	return stability.Validate(c.Input)
}
