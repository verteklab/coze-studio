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

package ctxutil

import (
	"context"
	"os"
	"strings"

	"github.com/coze-dev/coze-studio/backend/bizpkg/config"
	"github.com/coze-dev/coze-studio/backend/types/consts"
)

// IsAdminFromCtx reports whether the current session belongs to an admin user,
// using the same whitelist logic as middleware.AdminAuthMW: the session email
// must appear in the configured admin email list (base config AdminEmails,
// falling back to the ALLOW_REGISTRATION_EMAIL env var), case-insensitively.
func IsAdminFromCtx(ctx context.Context) bool {
	session := GetUserSessionFromCtx(ctx)
	if session == nil || session.UserEmail == "" {
		return false
	}

	adminEmails := ""
	if baseConf, err := config.Base().GetBaseConfig(ctx); err == nil && baseConf != nil {
		adminEmails = baseConf.AdminEmails
	}
	if adminEmails == "" {
		adminEmails = os.Getenv(consts.AllowRegistrationEmail)
	}
	if adminEmails == "" {
		return false
	}

	for _, email := range strings.Split(adminEmails, ",") {
		if strings.EqualFold(strings.TrimSpace(email), session.UserEmail) {
			return true
		}
	}
	return false
}
