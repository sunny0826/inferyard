package desktop

import (
	"context"
	"encoding/json"
	"strings"

	"inferyard/studio/internal/core"
)

type Message struct{ Title, Body string }

func Subscribe(ctx context.Context, bus *core.EventBus, show func(Message) error, logf func(string, ...any)) {
	SubscribeWithFilter(ctx, bus, nil, show, logf)
}

func SubscribeWithFilter(ctx context.Context, bus *core.EventBus, suppress func(core.Event) bool, show func(Message) error, logf func(string, ...any)) {
	wake, unsubscribe, cursor := bus.Subscribe()
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				events, gap, current := bus.After(cursor)
				cursor = current
				if gap {
					logf("desktop notification events exceeded replay buffer")
					continue
				}
				for _, event := range events {
					if ctx.Err() != nil {
						return
					}
					if suppress != nil && suppress(event) {
						continue
					}
					if message, ok := messageFor(event); ok {
						if err := show(message); err != nil {
							logf("desktop notification delivery failed: %v", err)
						}
					}
				}
			}
		}
	}()
}

func messageFor(event core.Event) (Message, bool) {
	switch event.Type {
	case "report.created":
		return reportCreatedMessage(event.Data), true
	case "run.state":
		return runStateMessage(event.Data)
	case "download.state":
		return downloadStateMessage(event.Data)
	case "engine.state":
		return engineStateMessage(event.Data)
	default:
		return Message{}, false
	}
}

func reportCreatedMessage(data json.RawMessage) Message {
	var payload struct {
		Report struct {
			ReportId string `json:"report_id"`
		} `json:"report"`
	}
	if json.Unmarshal(data, &payload) == nil && strings.TrimSpace(payload.Report.ReportId) != "" {
		return Message{
			Title: "报告已生成",
			Body:  "测评报告 " + payload.Report.ReportId + " 已就绪，可打开 Studio 查看。",
		}
	}
	return Message{
		Title: "报告已生成",
		Body:  "测评报告已就绪，可打开 Studio 查看。",
	}
}

func runStateMessage(data json.RawMessage) (Message, bool) {
	var payload struct {
		Task struct {
			Kind   string  `json:"kind"`
			State  string  `json:"state"`
			Bundle *string `json:"bundle"`
		} `json:"task"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Message{}, false
	}
	name := "测评任务"
	if payload.Task.Kind == "context_speed" {
		name = "长上下文专项"
	} else if payload.Task.Bundle != nil && strings.TrimSpace(*payload.Task.Bundle) != "" {
		name = "测评题集 " + *payload.Task.Bundle
	}
	switch payload.Task.State {
	case "done":
		return Message{Title: name + "已完成", Body: "结果已保存，可打开 Studio 查看详情或报告。"}, true
	case "failed":
		return Message{Title: name + "失败", Body: "请打开 Studio 查看失败原因与恢复操作。"}, true
	case "cancelled":
		return Message{Title: name + "已取消", Body: "已完成的进度与结果已保留。"}, true
	default:
		return Message{}, false
	}
}

func downloadStateMessage(data json.RawMessage) (Message, bool) {
	var payload struct {
		Task struct {
			Kind        string `json:"kind"`
			State       string `json:"state"`
			DisplayName string `json:"display_name"`
			OutName     string `json:"out_name"`
		} `json:"task"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Message{}, false
	}
	if payload.Task.Kind == "polystrata" {
		label := strings.TrimSpace(payload.Task.DisplayName)
		if label == "" {
			label = "PolyStrata"
		}
		switch payload.Task.State {
		case "done":
			return Message{Title: "PolyStrata 安装完成", Body: label + " 已就绪，可打开 Studio 开始测评。"}, true
		case "failed":
			return Message{Title: "PolyStrata 安装失败", Body: label + " 安装未完成，请打开 Studio 查看原因并重试。"}, true
		case "cancelled":
			return Message{Title: "PolyStrata 安装已取消", Body: label + " 的安装已取消。"}, true
		default:
			return Message{}, false
		}
	}
	kind := "模型"
	if payload.Task.Kind == "engine" {
		kind = "引擎"
	}
	target := kind
	if strings.TrimSpace(payload.Task.OutName) != "" {
		target = kind + " " + payload.Task.OutName
	}
	switch payload.Task.State {
	case "done":
		return Message{Title: kind + "下载完成", Body: target + " 已就绪，可用于测评。"}, true
	case "failed":
		return Message{Title: kind + "下载失败", Body: target + " 下载失败，请打开 Studio 查看原因并重试。"}, true
	case "cancelled":
		return Message{Title: kind + "下载已取消", Body: target + " 的下载已取消。"}, true
	default:
		return Message{}, false
	}
}

func engineStateMessage(data json.RawMessage) (Message, bool) {
	var payload struct {
		Engine struct {
			State string `json:"state"`
		} `json:"engine"`
	}
	if json.Unmarshal(data, &payload) != nil {
		return Message{}, false
	}
	switch payload.Engine.State {
	case "failed":
		return Message{Title: "托管引擎异常退出", Body: "请打开 Studio 检查引擎状态并重新启动。"}, true
	case "orphaned":
		return Message{Title: "检测到遗留模型服务", Body: "请打开 Studio 停止遗留服务或清理记录。"}, true
	default:
		return Message{}, false
	}
}
