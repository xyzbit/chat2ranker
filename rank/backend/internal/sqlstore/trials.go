package sqlstore

import (
	"context"
	"github.com/xyzbit/agentkiln/rank/backend/internal/domain"
)

func (s *Store) ListTrialConversations(ctx context.Context, tenantID string, filter domain.TrialConversationFilter) ([]domain.TrialConversation, error) {
	rows, err := s.q.QueryContext(ctx, `
	 SELECT c.public_id, COALESCE((SELECT substr(input_text,1,80) FROM responses r WHERE r.conversation_id=c.id ORDER BY r.id LIMIT 1),'')
	 FROM conversations c WHERE c.tenant_id=$1 AND c.trial=1
	 AND ($2='' OR c.agent_version_id IN (SELECT id FROM agent_versions WHERE family_id=$3))
	 AND ($4='' OR c.agent_version_id=$5)
	 AND ($6=0 OR c.deployment_id IS NULL)
	 AND ($7='' OR c.id<(SELECT id FROM conversations WHERE public_id=$8 AND tenant_id=$9))
	 ORDER BY c.id DESC LIMIT 30`, tenantID, filter.AgentFamilyID, filter.AgentFamilyID, filter.AgentVersionID, filter.AgentVersionID, boolInt(filter.DirectOnly), filter.Before, filter.Before, tenantID)
	if err != nil {
		return nil, err
	}
	items := []domain.TrialConversation{}
	for rows.Next() {
		var item domain.TrialConversation
		if err := rows.Scan(&item.ID, &item.Title); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for i := range items {
		items[i].Conversation, err = s.GetConversation(ctx, items[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
