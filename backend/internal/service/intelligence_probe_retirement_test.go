package service

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceProbeRetirementDoesNotStartWorker(t *testing.T) {
	svc := ProvideIntelligenceProbeService(nil, nil, nil, nil, nil, nil)
	defer svc.Stop()
	require.False(t, svc.started)
	require.Empty(t, svc.gatewayURL)
	require.Nil(t, svc.db)
}

func TestIntelligenceProbeRetirementHasNoRoutes(t *testing.T) {
	for _, path := range []string{"../server/routes/admin.go", "../server/routes/user.go"} {
		source, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		ast.Inspect(source, func(node ast.Node) bool {
			if literal, ok := node.(*ast.BasicLit); ok && literal.Kind == token.STRING {
				require.NotContains(t, literal.Value, "/intelligence-probe", path)
			}
			return true
		})
	}
}
