package governance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Store 负责 State 的本地持久化（原子写 JSON 文件，模拟链上状态）。
// 每次状态变更后整份状态落盘；写文件采用 temp+rename，进程崩溃时
// 要么读到旧文件、要么读到新文件，不会出现半截 JSON。
type Store struct {
	path  string
	state *State
}

// NewStore 打开（或初始化）path 指向的状态文件。
func NewStore(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("empty store path")
	}
	st := newState()
	data, err := os.ReadFile(path)
	if err == nil {
		if jerr := json.Unmarshal(data, st); jerr != nil {
			return nil, fmt.Errorf("load state: %w", jerr)
		}
		if len(st.Blocks) == 0 {
			return nil, errors.New("corrupt state: no blocks")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read state file: %w", err)
	}
	s := &Store{path: path, state: st}
	if err := s.Save(); err != nil {
		return nil, err
	}
	return s, nil
}

// State 返回当前状态指针。调用方必须在 Governor 的锁保护下使用。
func (s *Store) State() *State { return s.state }

// Replace 用给定状态替换内存状态（主要用于测试/恢复）。
func (s *Store) Replace(st *State) error {
	if st == nil {
		return errors.New("nil state")
	}
	s.state = st
	return s.Save()
}

// Save 将当前状态原子落盘。
func (s *Store) Save() error {
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return writeFileAtomic(s.path, data, 0o600)
}

func (s *Store) Close() error { return s.Save() }

// writeFileAtomic 先写同目录临时文件再 rename，保证落盘文件的完整性。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir state dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp state: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("fsync temp state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename state: %w", err)
	}
	return nil
}
