package authz

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/casbin/casbin/v2/model"
	"github.com/casbin/casbin/v2/persist"
)

var (
	ErrAdapterReadOnly         = errors.New("authz: policy adapter is load-only")
	ErrPolicyChangedDuringLoad = errors.New("authz: policy changed repeatedly during load")
)

type StoredPolicy struct {
	PType  string
	Values []string
}

// PolicySource 提供权威策略及其单调递增版本，供加载器检测读取期间的并发变更。
// 策略变更与版本递增必须在同一事务内提交。
type PolicySource interface {
	// LoadPolicyRows 返回全部角色授权和继承规则；空结果表示没有策略。
	LoadPolicyRows(context.Context) ([]StoredPolicy, error)
	// PolicyVersion 返回当前已提交的全局策略版本；加载器通过读取前后版本相等确认快照稳定。
	PolicyVersion(context.Context) (int64, error)
}

// StorageAdapter adapts a project-owned policy store to Casbin's read API.
// Writes stay in the owning service so its transaction and audit rules cannot
// be bypassed through Casbin AutoSave.
type StorageAdapter struct {
	source        PolicySource
	loadedVersion atomic.Int64
}

func NewStorageAdapter(source PolicySource) *StorageAdapter {
	return &StorageAdapter{source: source}
}

func (a *StorageAdapter) LoadPolicy(m model.Model) error {
	return a.LoadPolicyContext(context.Background(), m)
}

// LoadPolicyContext 在读取前后核对版本，取得稳定快照后才填充模型并记录已加载版本。
// 连续五次读取均发生版本变化时返回 ErrPolicyChangedDuringLoad。
func (a *StorageAdapter) LoadPolicyContext(ctx context.Context, m model.Model) error {
	var rows []StoredPolicy
	var version int64
	stable := false
	for range 5 {
		before, err := a.source.PolicyVersion(ctx)
		if err != nil {
			return err
		}
		rows, err = a.source.LoadPolicyRows(ctx)
		if err != nil {
			return err
		}
		after, err := a.source.PolicyVersion(ctx)
		if err != nil {
			return err
		}
		if before == after {
			version = after
			stable = true
			break
		}
	}
	if !stable {
		return ErrPolicyChangedDuringLoad
	}
	for _, row := range rows {
		line := append([]string{row.PType}, row.Values...)
		if err := persist.LoadPolicyArray(line, m); err != nil {
			return fmt.Errorf("authz: load policy %v: %w", line, err)
		}
	}
	a.loadedVersion.Store(version)
	return nil
}

func (a *StorageAdapter) LoadedPolicyVersion() int64 { return a.loadedVersion.Load() }

func (a *StorageAdapter) SavePolicy(model.Model) error {
	return fmt.Errorf("authz: SavePolicy: %w", ErrAdapterReadOnly)
}

func (a *StorageAdapter) AddPolicy(string, string, []string) error {
	return fmt.Errorf("authz: AddPolicy: %w", ErrAdapterReadOnly)
}

func (a *StorageAdapter) RemovePolicy(string, string, []string) error {
	return fmt.Errorf("authz: RemovePolicy: %w", ErrAdapterReadOnly)
}

func (a *StorageAdapter) RemoveFilteredPolicy(string, string, int, ...string) error {
	return fmt.Errorf("authz: RemoveFilteredPolicy: %w", ErrAdapterReadOnly)
}

var _ persist.Adapter = (*StorageAdapter)(nil)
