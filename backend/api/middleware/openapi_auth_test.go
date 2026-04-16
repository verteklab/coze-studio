package middleware

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/assert"
)

func TestIsNeedOpenapiAuthForKnowledgeRoutes(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "existing document list openapi route",
			path: "/open_api/knowledge/document/list",
			want: true,
		},
		{
			name: "dataset detail openapi route",
			path: "/open_api/knowledge/detail",
			want: true,
		},
		{
			name: "slice list openapi route",
			path: "/open_api/knowledge/slice/list",
			want: true,
		},
		{
			name: "photo list openapi route",
			path: "/open_api/knowledge/photo/list",
			want: true,
		},
		{
			name: "photo detail openapi route",
			path: "/open_api/knowledge/photo/detail",
			want: true,
		},
		{
			name: "web knowledge route still uses session auth",
			path: "/api/knowledge/detail",
			want: false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var c app.RequestContext
			c.Request.SetRequestURI(tc.path)

			assert.Equal(t, tc.want, isNeedOpenapiAuth(&c))
		})
	}
}
