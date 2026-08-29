package app

import (
	"context"
	"errors"
	"strings"

	"github.com/xyzbit/chat2ranker/execution/backend/contract"
	"github.com/xyzbit/chat2ranker/execution/backend/internal/domain"
)

func validSystemModelRole(role string) bool {
	return role == contract.SystemModelControl || role == contract.SystemModelJudge
}

func (s *Service) ListSystemModelBindings(ctx context.Context) ([]contract.SystemModelBinding, error) {
	return s.repo.ListSystemModelBindings(ctx)
}

func (s *Service) GetSystemModelBinding(ctx context.Context, role string) (contract.SystemModelBinding, error) {
	if !validSystemModelRole(role) {
		return contract.SystemModelBinding{}, problem(400, "invalid_system_model_role", "系统模型角色只能是 control 或 judge")
	}
	value, err := s.repo.GetSystemModelBinding(ctx, role)
	if errors.Is(err, domain.ErrNotFound) {
		return value, problem(404, "system_model_unconfigured", "系统模型尚未配置")
	}
	return value, err
}

func (s *Service) SaveSystemModelBinding(ctx context.Context, role string, input contract.SystemModelBindingInput) (contract.SystemModelBinding, error) {
	if !validSystemModelRole(role) {
		return contract.SystemModelBinding{}, problem(400, "invalid_system_model_role", "系统模型角色只能是 control 或 judge")
	}
	connection, err := s.GetModelConnection(ctx, strings.TrimSpace(input.ConnectionID))
	if err != nil {
		return contract.SystemModelBinding{}, err
	}
	if connection.Status != "verified" {
		return contract.SystemModelBinding{}, problem(409, "system_model_connection_unverified", "请先验证模型连接")
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = connection.DefaultModel
	}
	if model == "" {
		return contract.SystemModelBinding{}, problem(400, "system_model_missing_model", "请选择模型")
	}
	value := contract.SystemModelBinding{Role: role, ConnectionID: connection.ID, Model: model, Connection: connection, UpdatedAt: s.now().UTC()}
	return value, s.repo.SaveSystemModelBinding(ctx, value)
}

func (s *Service) ResolveSystemModel(ctx context.Context, role string) (contract.SystemModelRuntime, error) {
	binding, err := s.GetSystemModelBinding(ctx, role)
	if err != nil {
		return contract.SystemModelRuntime{}, err
	}
	connection, credential, err := s.ResolveModelConnection(ctx, binding.ConnectionID)
	if err != nil {
		return contract.SystemModelRuntime{}, err
	}
	binding.Connection = connection
	return contract.SystemModelRuntime{Binding: binding, Credential: credential}, nil
}
