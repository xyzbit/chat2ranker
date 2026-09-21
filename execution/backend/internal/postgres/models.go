package postgres

import (
	"context"
	"encoding/json"

	"github.com/xyzbit/agentkiln/execution/backend/contract"
	"github.com/xyzbit/agentkiln/execution/backend/internal/domain"
)

func (s *Store) ListModelConnections(ctx context.Context) ([]contract.ModelConnection, error) {
	rows, err := s.pool.Query(ctx, `SELECT body,credential_ref FROM model_connections ORDER BY body->>'name',id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []contract.ModelConnection{}
	for rows.Next() {
		value, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *Store) GetModelConnection(ctx context.Context, id string) (contract.ModelConnection, error) {
	return scanConnection(s.pool.QueryRow(ctx, `SELECT body,credential_ref FROM model_connections WHERE id=$1`, id))
}
func (s *Store) SaveModelConnection(ctx context.Context, value contract.ModelConnection) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO model_connections(id,body,credential_ref) VALUES($1,$2,$3) ON CONFLICT(id) DO UPDATE SET body=excluded.body,credential_ref=excluded.credential_ref`, value.ID, body, value.CredentialRef)
	return mapError(err)
}
func (s *Store) DeleteModelConnection(ctx context.Context, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM model_connections WHERE id=$1`, id)
	if err != nil {
		return mapError(err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
func (s *Store) ListSystemModelBindings(ctx context.Context) ([]contract.SystemModelBinding, error) {
	rows, err := s.pool.Query(ctx, `SELECT b.body,c.body,c.credential_ref FROM system_model_bindings b JOIN model_connections c ON c.id=b.connection_id ORDER BY b.role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []contract.SystemModelBinding{}
	for rows.Next() {
		value, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *Store) GetSystemModelBinding(ctx context.Context, role string) (contract.SystemModelBinding, error) {
	return scanBinding(s.pool.QueryRow(ctx, `SELECT b.body,c.body,c.credential_ref FROM system_model_bindings b JOIN model_connections c ON c.id=b.connection_id WHERE b.role=$1`, role))
}
func (s *Store) SaveSystemModelBinding(ctx context.Context, value contract.SystemModelBinding) error {
	value.Connection = contract.ModelConnection{}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO system_model_bindings(role,connection_id,body) VALUES($1,$2,$3) ON CONFLICT(role) DO UPDATE SET connection_id=excluded.connection_id,body=excluded.body`, value.Role, value.ConnectionID, body)
	return mapError(err)
}
func scanConnection(row scanner) (contract.ModelConnection, error) {
	var value contract.ModelConnection
	var ref string
	err := row.Scan(&value, &ref)
	value.CredentialRef, value.HasCredential = ref, ref != ""
	if value.Prices == nil {
		value.Prices = map[string]contract.ModelPrice{}
	}
	return value, mapError(err)
}
func scanBinding(row scanner) (contract.SystemModelBinding, error) {
	var value contract.SystemModelBinding
	var connection contract.ModelConnection
	var ref string
	err := row.Scan(&value, &connection, &ref)
	connection.CredentialRef, connection.HasCredential = ref, ref != ""
	if connection.Prices == nil {
		connection.Prices = map[string]contract.ModelPrice{}
	}
	value.Connection = connection
	return value, mapError(err)
}
