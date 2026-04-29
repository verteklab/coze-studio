/*
 * Copyright 2025 coze-dev Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package modelmgr

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gen/field"

	"github.com/coze-dev/coze-studio/backend/api/model/admin/config"
	"github.com/coze-dev/coze-studio/backend/api/model/app/developer_api"
	"github.com/coze-dev/coze-studio/backend/bizpkg/config/modelmgr/internal/model"
	"github.com/coze-dev/coze-studio/backend/bizpkg/config/modelmgr/internal/query"
)

func (c *ModelConfig) CreateModel(ctx context.Context, modelClass developer_api.ModelClass, modelShowName string, conn *config.Connection, extra *ModelExtra) (int64, error) {
	id, err := c.createModel(ctx, nil, modelClass, modelShowName, conn, extra)
	if err != nil {
		return 0, err
	}

	err = c.SetDoNotUseOldModelConf(ctx)
	if err != nil {
		return 0, fmt.Errorf("set do not use old model failed, err: %w", err)
	}

	return id, nil
}

func (c *ModelConfig) createModel(ctx context.Context, id *int64, modelClass developer_api.ModelClass, modelShowName string, conn *config.Connection, extra *ModelExtra) (int64, error) {
	if conn == nil {
		return 0, fmt.Errorf("connection is nil")
	}

	if conn.BaseConnInfo == nil {
		return 0, fmt.Errorf("base conn info is nil")
	}

	provider, ok := GetModelProvider(modelClass)
	if !ok {
		return 0, fmt.Errorf("model class %s not supported", modelClass)
	}

	conn, err := encryptConn(ctx, conn)
	if err != nil {
		return 0, err
	}

	modelName := conn.BaseConnInfo.Model
	modelMeta, err := modelMetaConf.GetModelMeta(modelClass, modelName)
	if err != nil {
		return 0, fmt.Errorf("get model meta failed, err: %w", err)
	}

	if modelMeta.Connection != nil {
		conn.Ark = modelMeta.Connection.Ark
		conn.Openai = modelMeta.Connection.Openai
		conn.Deepseek = modelMeta.Connection.Deepseek
		conn.Gemini = modelMeta.Connection.Gemini
		conn.Qwen = modelMeta.Connection.Qwen
		conn.Ollama = modelMeta.Connection.Ollama
		conn.Claude = modelMeta.Connection.Claude
		if conn.CustomHTTP == nil {
			conn.CustomHTTP = modelMeta.Connection.CustomHTTP
		}
	}

	extraStr := "{}"
	if extra != nil {
		extraByte, err1 := json.Marshal(extra)
		if err1 != nil {
			return 0, fmt.Errorf("marshal extra failed, err: %w", err)
		}

		extraStr = string(extraByte)
	}

	q := query.ModelInstance.WithContext(ctx)
	m := &model.ModelInstance{
		Type:        int32(config.ModelType_LLM),
		Provider:    provider,
		Connection:  conn,
		Capability:  pickCapability(extraCapability(extra), modelMeta.Capability),
		Parameters:  modelMeta.Parameters,
		DisplayInfo: modelMeta.DisplayInfo,
		Extra:       extraStr,
	}

	if id != nil {
		m.ID = *id
	}

	if len(modelShowName) > 0 {
		m.DisplayInfo.Name = modelShowName
	}

	err = q.Create(m)
	if err != nil {
		return 0, err
	}

	return m.ID, nil
}

func (c *ModelConfig) DeleteModel(ctx context.Context, modelID int64) error {
	q := query.ModelInstance.WithContext(ctx)
	_, err := q.Where(query.ModelInstance.ID.Eq(modelID)).Delete()
	return err
}

func encryptConn(ctx context.Context, conn *config.Connection) (*config.Connection, error) {
	// encrypt conn if you need
	return conn, nil
}

func decryptConn(ctx context.Context, conn *config.Connection) (*config.Connection, error) {
	return conn, nil
}

// pickCapability returns reqCap when non-nil; otherwise metaCap.
// Used by createModel/UpdateModel to honor caller-supplied capability over
// the default from model_meta.json.
func pickCapability(reqCap, metaCap *developer_api.ModelAbility) *developer_api.ModelAbility {
	if reqCap != nil {
		return reqCap
	}
	return metaCap
}

// extraCapability returns the Capability field of e, or nil if e is nil.
// Helper for use in createModel where extra may be nil.
func extraCapability(e *ModelExtra) *developer_api.ModelAbility {
	if e == nil {
		return nil
	}
	return e.Capability
}

// UpdateModel patches an existing ModelInstance row. Currently supports
// updating capability, connection, and display_info. Other fields
// (provider, parameters, extra) are intentionally NOT modified by this path.
//
// The route /api/admin/config/model/update was declared in idl/admin/config.thrift
// but had no implementation; this function is the bizpkg backing.
func (c *ModelConfig) UpdateModel(ctx context.Context, m *config.Model) error {
	if m == nil || m.ID == 0 {
		return fmt.Errorf("UpdateModel: model id is required")
	}

	q := query.ModelInstance.WithContext(ctx)

	// Verify the row exists; surface a clear error if it doesn't.
	existing, err := q.Where(query.ModelInstance.ID.Eq(m.ID)).First()
	if err != nil {
		return fmt.Errorf("UpdateModel: lookup id=%d failed: %w", m.ID, err)
	}

	// Build the field-list of columns to update so GORM writes ONLY what the
	// caller patched — Provider/Parameters/Extra are not touched even if they
	// have zero values in the in-memory copy.
	updated := *existing
	selectFields := []field.Expr{}
	if m.Capability != nil {
		updated.Capability = m.Capability
		selectFields = append(selectFields, query.ModelInstance.Capability)
	}
	if m.Connection != nil {
		updated.Connection = m.Connection
		selectFields = append(selectFields, query.ModelInstance.Connection)
	}
	if m.DisplayInfo != nil {
		updated.DisplayInfo = m.DisplayInfo
		selectFields = append(selectFields, query.ModelInstance.DisplayInfo)
	}

	if len(selectFields) == 0 {
		return nil // empty patch is a no-op
	}

	if _, err = q.Where(query.ModelInstance.ID.Eq(m.ID)).
		Select(selectFields...).
		Updates(&updated); err != nil {
		return fmt.Errorf("UpdateModel: save id=%d failed: %w", m.ID, err)
	}
	return nil
}
