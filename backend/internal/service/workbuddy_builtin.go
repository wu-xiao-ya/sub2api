package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/workbuddy"
)

const workBuddyBuiltinID = workbuddy.ID
const WorkBuddyImportMaxBytes = workbuddy.MaxImportBytes

func (m *PluginManager) ImportWorkBuddyAccounts(ctx context.Context, id int64, raw string) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > WorkBuddyImportMaxBytes || !json.Valid([]byte(raw)) {
		return nil, errors.New("invalid WorkBuddy JSON")
	}
	return m.Authorize(ctx, id, "import", "", raw)
}

func isBuiltinWorkBuddy(p *PluginInstallation) bool {
	return p != nil && p.PluginKey == workBuddyBuiltinID && p.Manifest.ID == workBuddyBuiltinID && isProviderPlugin(p.Manifest)
}

func describeBuiltinWorkBuddy(p *PluginInstallation) {
	if !isBuiltinWorkBuddy(p) {
		return
	}
	p.Builtin = true
	p.Name = "WorkBuddy 国内版"
	p.Version = workbuddy.Version
	p.Description = "国内版账号授权、积分查询与可选模型"
	p.Compatibility.Compatible = true
	p.Compatibility.Tested = true
	p.Compatibility.Status = "compatible"
	p.Compatibility.Message = "WorkBuddy is included in this Sub2API build"
}

func localBuiltinWorkBuddy(p *PluginInstallation) (*PluginInstallation, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	local := *p
	describeBuiltinWorkBuddy(&local)
	local.BinaryPath = path
	return &local, nil
}

func readBuiltinWorkBuddyUI(relative string) ([]byte, string, error) {
	name := strings.TrimPrefix(relative, "/")
	switch name {
	case "", "index.html":
		name = "index.html"
	case "app.js", "style.css":
	default:
		return nil, "", os.ErrNotExist
	}
	path := "ui/" + name
	data, err := workbuddy.Assets.ReadFile(path)
	return data, path, err
}
