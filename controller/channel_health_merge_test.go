package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/service"
)

func TestMergeChannelProbeEvents(t *testing.T) {
	items := []channelHealthEventItem{
		{
			ID:        3,
			CreatedAt: 103,
			ChannelID: 47,
			EventType: service.ChannelHealthEventProbeSucceeded,
			Content:   "常规主动探针通过",
			Other:     map[string]interface{}{"probe_mode": "continuous_active"},
		},
		{
			ID:        2,
			CreatedAt: 101,
			ChannelID: 47,
			EventType: service.ChannelHealthEventProbeStarted,
			Content:   "常规主动探针开始",
			Other:     map[string]interface{}{"probe_mode": "continuous_active"},
		},
		{
			ID:        1,
			CreatedAt: 100,
			ChannelID: 47,
			EventType: service.ChannelHealthEventProbeScanned,
			Content:   "主动探针扫描到持续探测渠道",
			Other:     map[string]interface{}{"probe_mode": "continuous_active"},
		},
	}

	merged := mergeChannelProbeEvents(items, 10)
	if len(merged) != 1 {
		t.Fatalf("expected one merged probe record, got %d", len(merged))
	}
	item := merged[0]
	if item.EventType != service.ChannelHealthEventProbeSucceeded {
		t.Fatalf("expected final event type %q, got %q", service.ChannelHealthEventProbeSucceeded, item.EventType)
	}
	if !item.MergedProbe {
		t.Fatal("expected merged probe marker")
	}
	if item.RawEventCount != 3 {
		t.Fatalf("expected three raw events, got %d", item.RawEventCount)
	}
	if item.StartedAt != 101 || item.FinishedAt != 103 || item.Duration != 2 {
		t.Fatalf("unexpected timing: started=%d finished=%d duration=%d", item.StartedAt, item.FinishedAt, item.Duration)
	}
}
