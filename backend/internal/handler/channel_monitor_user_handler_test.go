//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUserMonitorResponsesExposeOnlyPublicSource(t *testing.T) {
	listItem := userMonitorViewToItem(&service.UserMonitorView{
		ID:            1,
		PrimarySource: "traffic",
		ExtraModels: []service.ExtraModelStatus{
			{Model: "gpt-5.5", Source: "probe"},
		},
	})
	if listItem.PrimarySource != "traffic" {
		t.Fatalf("primary source = %q, want traffic", listItem.PrimarySource)
	}
	if len(listItem.ExtraModels) != 1 || listItem.ExtraModels[0].Source != "probe" {
		t.Fatalf("extra model response = %#v", listItem.ExtraModels)
	}

	detail := userMonitorDetailToResponse(&service.UserMonitorDetail{
		ID: 1,
		Models: []service.ModelDetail{
			{Model: "gpt-5.6-sol", Source: "traffic"},
			{Model: "gpt-5.5", Source: "probe"},
		},
	})
	if detail.Models[0].Source != "traffic" || detail.Models[1].Source != "probe" {
		t.Fatalf("detail model sources = %#v", detail.Models)
	}
}
