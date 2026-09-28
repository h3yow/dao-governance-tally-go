package governance

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
)

// Store 是本地持久化接口。Save 必须是"全量替换"语义：
// 先写临时文件再原子 rename，保证崩溃/断电时不会留下半截状态。
type Store interface {
	Load() (*State, error)
	Save(*State) error
}

// FileStore 把整个状态以单个 JSON 文件原子落盘，用于模拟链上状态。
type FileStore struct {
	path string
}

// NewFileStore 创建基于文件的存储。
func NewFileStore(path string) *FileStore {
	return &FileStore{path: path}
}

// Path 返回底层文件路径。
func (s *FileStore) Path() string { return s.path }

// Load 读取并反序列化状态；文件不存在时返回 ErrNotFound，
// 由上层（Governor）决定是否初始化空状态。
func (s *FileStore) Load() (*State, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if err := st.normalize(); err != nil {
		return nil, err
	}
	return &st, nil
}

// Save 先写同目录临时文件并 fsync，再 rename 覆盖目标文件。
// rename 在同一文件系统上是原子的，因此任何时刻要么读到旧状态、要么读到新状态。
func (s *FileStore) Save(st *State) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
